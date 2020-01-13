package geofriend

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/netutil"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/gomodule/redigo/redis"
	"github.com/paulmach/go.geojson"
)

var FieldMapFilename = `fieldmap.toml`
var DefaultGeoDataDir = `~/.cache/catalina/navigation/geodata`
var DefaultGeoAutoloadDir = `~/.config/catalina/navigation/geodata`
var GeoserverRedisConnectTimeout = redis.DialConnectTimeout(5 * time.Second)

type GeoFieldMappings struct {
	Types map[string]*GeoFieldMapping `toml:"types"`
}

type GeoFieldMapping struct {
	DisplayKeys   []string   `toml:"display"`
	OnlyFields    []string   `toml:"only"`
	NormalizeKeys string     `toml:"normalize"`
	Rename        [][]string `toml:"rename"`
}

func LoadTile38(address string, dir string) error {
	if dir != `` && address != `` {
		var fieldmap GeoFieldMappings

		// load field mappings if we have any
		if dir, err := pathutil.ExpandUser(dir); err == nil {
			// parse file containing "dataset:field" mappings (one per line)
			fieldmapFile := filepath.Join(dir, FieldMapFilename)

			if _, err := toml.DecodeFile(fieldmapFile, &fieldmap); err == nil {
				if len(fieldmap.Types) > 0 {
					log.Infof("[tile38] Loaded %d geodata field mappings", len(fieldmap.Types))
				} else {
					log.Warningf("[tile38] No geodata field mappings were loaded")
				}
			} else if !os.IsNotExist(err) {
				return err
			}
		}

		if err := netutil.WaitForOpen(`tcp`, address, 10*time.Second); err != nil {
			return fmt.Errorf("could not connect to %s", address)
		} else {
			log.Infof("[tile38] geoserver connection to %s verified, proceeding to autoload data...", address)
		}

		if rclient, err := redis.Dial(`tcp`, address, GeoserverRedisConnectTimeout); err == nil {
			if dir, err := pathutil.ExpandUser(dir); err == nil {
				// load datasets
				if files, err := filepath.Glob(filepath.Join(dir, `*.*`)); err == nil {
					var wg sync.WaitGroup

					for _, filename := range files {
						wg.Add(1)
						t38AutoloadDataFile(&wg, &fieldmap, rclient, filename, nil)
					}

					wg.Wait()
					return nil
				} else {
					return err
				}
			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return fmt.Errorf("Must specify destination address and geodata source directory")
	}
}

func t38AutoloadDataFile(wg *sync.WaitGroup, fieldmap *GeoFieldMappings, rclient redis.Conn, filename string, data io.ReadCloser) {
	if wg != nil {
		defer wg.Done()
	}

	if data == nil {
		if file, err := os.Open(filename); err == nil {
			data = file
		} else {
			log.Warningf("[tile38] failed to open %v: %v", filename, err)
			return
		}
	}

	if data != nil {
		ext := filepath.Ext(filename)
		dataset := strings.TrimSuffix(filepath.Base(filename), ext)

		defer data.Close()

		switch ext {
		case `.gz`:
			if file, err := os.Open(filename); err == nil {
				if gzipreader, err := gzip.NewReader(file); err == nil {
					t38AutoloadDataFile(nil, fieldmap, rclient, dataset, gzipreader)
				} else {
					log.Warningf("[tile38] failed to decompress %v: %v", filename, err)
				}
			} else {
				log.Warningf("[tile38] failed to open %v: %v", filename, err)
			}
		case `.geojson`:
			var count int
			var collection geojson.FeatureCollection

			if err := json.NewDecoder(data).Decode(&collection); err == nil {
				// mangle the incoming features...
				for i, feature := range collection.Features {
					var fieldargs []interface{}
					var mapping *GeoFieldMapping

					if fk, ok := fieldmap.Types[dataset]; ok {
						mapping = fk
					}

					if mapping == nil {
						log.Warningf("Skipping geodata file %v: no mapping found", filename)
						return
					}

					// pre-process feature properties into something Tile38 is cool with
					// -----------------------------------------------------------------------------
					properties := make(map[string]interface{})

					for k, v := range feature.Properties {
						trim := strings.TrimLeft(fmt.Sprintf("%v", v), `0`)

						switch mapping.NormalizeKeys {
						case `hyphenate`:
							k = stringutil.Hyphenate(k)
						case `camelize`:
							k = stringutil.Camelize(k)
						case `underscore`, ``:
							k = stringutil.Underscore(k)
						case `upper`:
							k = strings.ToUpper(k)
						case `lower`:
							k = strings.ToLower(k)
						}

						// perform field renames
						for _, pair := range mapping.Rename {
							if len(pair) == 2 && k == pair[0] && pair[1] != `` {
								k = pair[1]
							}
						}

						// remove fields we don't want (if requested)
						if len(mapping.OnlyFields) > 0 && !sliceutil.ContainsString(mapping.OnlyFields, k) {
							continue
						}

						if typeutil.IsNumeric(trim) {
							if len(trim) == 5 {
								// handle a special case for US ZIP codes (so that codes in the
								// Northeastern US starting with zero (e.g.: 07753) don't become 7753.
								v = trim
							} else {
								// autotype the rest
								v = stringutil.Autotype(trim)
							}

							properties[k] = v
							fieldargs = append(fieldargs, k, v)
						} else {
							properties[k] = stringutil.Autotype(v)
						}
					}

					// cull nulls
					feature.Properties, _ = maputil.Compact(properties)

					// add the feature data
					if data, err := feature.MarshalJSON(); err == nil {
						args := []interface{}{dataset, i}
						args = append(args, `OBJECT`, data)

						if _, err := rclient.Do(`SET`, args...); err == nil {
							args = []interface{}{dataset, i}
							args = append(args, fieldargs...)
							if _, err := rclient.Do(`FSET`, args...); err == nil {
								count += 1
							} else {
								log.Warningf("[tile38] failed to load feature %v: %v", i, err)
							}
						} else {
							log.Warningf("[tile38] failed to load feature %v: %v", i, err)
						}
					} else {
						log.Warningf("[tile38] failed to load feature %v: %v", i, err)
					}
				}
			} else {
				log.Warningf("[tile38] failed to parse %v: %v", filename, err)
			}

			log.Infof("[tile38] %s: loaded %d features", dataset, count)
		}

		data.Close()
	} else {
		log.Warningf("[tile38] empty data source for %v", filename)
	}
}
