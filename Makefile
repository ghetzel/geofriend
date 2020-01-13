.PHONY: deps fmt build
.EXPORT_ALL_VARIABLES:

GEODATA_SRCDIR   ?= /cortex/central/archives/websites/census.gov
GEODATA_PREPDIR  ?= dist
GEODATA_DESTDIR  ?= ~/.config/catalina/navigation/geodata
GEODATA_CACHEDIR ?= ~/.cache/catalina/navigation/geodata
GEODATA_STATES   ?= NY NJ CO CA CT MA PA
GEODATA_COUSUB   ?= 36 34 08 06 09 25 42
OGR_NEIGHBORHOOD  = ogr2ogr -progress -t_srs crs:84 -f GeoJSON -nln neighborhood -addfields -update -append $(GEODATA_DESTDIR)/neighborhood.geojson
TIGER_YEAR       ?= 2017
GO111MODULE      ?= on

all: deps fmt build

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './vendor/*')

build:
	go build -o bin/geofriend cmd/geofriend/*.go

dist:
	mkdir dist

# Geospatial Data Download
# --------------------------------------------------------------------------------------------------
download-geodata-neighborhood:
	$(shell test -d build/neighborhood || mkdir -p build/neighborhood)
	$(foreach state, $(GEODATA_STATES), $(shell echo 'Download Zillow $(state): ' >&2 && curl -sSfL 'https://www.zillowstatic.com/static-neighborhood-boundaries/LATEST/static-neighborhood-boundaries/shp/ZillowNeighborhoods-$(state).zip' > build/neighborhood/ZillowNeighborhoods-$(state).zip))
	$(shell echo 'Download NJ Place Names: ' >&2 && curl -sSfL 'https://www.state.nj.us/dep/gis/digidownload/zips/statewide/placenam04.zip' > build/neighborhood/placenam04.zip)
	ls -1 build/neighborhood/*.zip | xargs -I{} unzip -d build/neighborhood/ -o {}

#download-geodata-parking:
#	mkdir -p build/parking/nyc-dot
#	curl -sSfL 'http://a841-dotweb01.nyc.gov/datafeeds/ParkingReg/Parking_Regulation_Shapefile.zip' > build/parking/nyc-dot/signs.zip
#	cd build/parking/nyc-dot && unzip -j -o *.zip

download-geodata-timezone:
	-mkdir -p build/timezone
	curl -sSfL https://github.com/evansiroky/timezone-boundary-builder/releases/download/2018g/timezones-with-oceans.geojson.zip > build/timezone/timezone.zip

download-geodata: download-geodata-neighborhood download-geodata-timezone

# GeoJSON generation and processing
# --------------------------------------------------------------------------------------------------

regen-geodata-neighborhood:
	$(shell rm $(GEODATA_PREPDIR)/neighborhood.geojson)
	$(foreach file, $(wildcard build/neighborhood/*.shp), $(shell echo -n '$(file): ' >&2 && $(OGR_NEIGHBORHOOD) $(file) >&2))

regen-geodata-tiger-state:
#	States
	ogr2ogr -progress -select NAME,STUSPS,STATEFP -f GeoJSON $(GEODATA_PREPDIR)/state.geojson $(GEODATA_SRCDIR)/TIGER$(TIGER_YEAR)/STATE/*.shp

regen-geodata-tiger-county:
#	Counties
	ogr2ogr -progress -select NAME,COUNTYFP,STATEFP -simplify 0.0001 -f GeoJSON $(GEODATA_PREPDIR)/county.geojson $(GEODATA_SRCDIR)/TIGER$(TIGER_YEAR)/COUNTY/*.shp

regen-geodata-tiger-city:
#	City Bounds ("County Subdivisions")
	-rm $(GEODATA_PREPDIR)/city.geojson
	echo -n $(GEODATA_COUSUB) | xargs -P1 -I{} -d ' ' \
		ogr2ogr -nln city -append -progress -select NAME,COUNTYFP,STATEFP -simplify 0.0001 -f GeoJSON $(GEODATA_PREPDIR)/city.geojson $(GEODATA_SRCDIR)/TIGER$(TIGER_YEAR)/COUSUB/tl_$(TIGER_YEAR)_{}_cousub.shp

regen-geodata-tiger-postal:
#	ZIP Codes
	ogr2ogr -progress -select ZCTA5CE10 -simplify 0.001 -f GeoJSON $(GEODATA_PREPDIR)/postal.geojson $(GEODATA_SRCDIR)/TIGER$(TIGER_YEAR)/ZCTA5/*.shp

regen-geodata-tiger-railroad:
#	Railroads
	ogr2ogr -progress -select FULLNAME -simplify 0.001 -f GeoJSON $(GEODATA_PREPDIR)/railroad.geojson $(GEODATA_SRCDIR)/TIGER$(TIGER_YEAR)/RAILS/*.shp

regen-geodata-tiger: dist regen-geodata-tiger-state regen-geodata-tiger-county regen-geodata-tiger-city regen-geodata-tiger-postal regen-geodata-tiger-railroad

#regen-geodata-parking:
#	ogr2ogr -progress -select SIGNDESC1 -f GeoJSON $(GEODATA_PREPDIR)/parking.geojson build/parking/nyc-dot/*.shp

regen-geodata-timezone:
	cd build/timezone && unzip -j -o timezone.zip
	cp build/timezone/combined-with-oceans.json /tmp/timezone.geojson
	ogr2ogr -progress -simplify 0.001 -f GeoJSON $(GEODATA_PREPDIR)/timezone.geojson /tmp/timezone.geojson

regen-geodata: regen-geodata-neighborhood regen-geodata-tiger regen-geodata-timezone

compress:
	ls -1 $(GEODATA_PREPDIR)/*.geojson | xargs -I{} gzip -f -9 {}

finalize:
	cp fieldmap.toml $(GEODATA_PREPDIR)/fieldmap.toml
	rsync -rv --delete $(GEODATA_PREPDIR)/ $(GEODATA_DESTDIR)/
	-rm -rf $(GEODATA_CACHEDIR)

rebuild-timezone: download-geodata-timezone regen-geodata-timezone
rebuild-tiger: regen-geodata-tiger
rebuild-neighborhood: download-geodata-neighborhood regen-geodata-neighborhood

# Full Datasource Rebuild
rebuild: rebuild-timezone rebuild-tiger rebuild-neighborhood compress finalize
