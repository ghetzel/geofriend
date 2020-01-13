package main

import (
	"os"

	"github.com/ghetzel/cli"
	"github.com/ghetzel/geofriend"
	"github.com/ghetzel/go-stockutil/log"
)

func main() {
	app := cli.NewApp()
	app.Name = `geofriend`
	app.Usage = `A geofencing and location utility.`
	app.Version = `0.0.1`

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			Value:  `info`,
			EnvVar: `LOGLEVEL`,
		},
		cli.StringFlag{
			Name:   `address, a`,
			Usage:  `The address of the Tile38 server`,
			Value:  `localhost:9851`,
			EnvVar: `GEOFRIEND_TILE38_ADDRESS`,
		},
	}

	app.Before = func(c *cli.Context) error {
		log.SetLevelString(c.String(`log-level`))
		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:  `load-data`,
			Usage: `Autoload geodata to the destination server`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `data-dir, d`,
					Usage: `The location of the directory containing GeoJSON (.json, .json.gz) data to load`,
					Value: geofriend.DefaultGeoAutoloadDir,
				},
			},
			Action: func(c *cli.Context) {
				log.FatalIf(
					geofriend.LoadTile38(c.GlobalString(`address`), c.String(`data-dir`)),
				)
			},
		},
	}

	app.Run(os.Args)
}
