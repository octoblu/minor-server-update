package main

import (
	"fmt"
	"log"
	"os"

	"github.com/codegangsta/cli"
	"github.com/coreos/go-semver/semver"
	"github.com/fatih/color"
	"github.com/octoblu/minor-server-update/vctlsync"
	De "github.com/tj/go-debug"
)

var debug = De.Debug("minor-server-update:main")

func main() {
	app := cli.NewApp()
	app.Name = "minor-server-update"
	app.Version = version()
	app.Action = run
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "etcd-uri",
			EnvVar: "MINOR_SERVER_UPDATE_ETCD_URI",
			Usage:  "Etcd URI that vulcan uses. Will only be used for read-only activity",
		},
		cli.StringFlag{
			Name:   "vulcand-uri",
			EnvVar: "MINOR_SERVER_UPDATE_VULCAND_URI",
			Usage:  "Vulcand URI where vulcan's API is available. Will only be used all write activity",
		},
	}
	app.Run(os.Args)
}

func run(context *cli.Context) {
	etcdURI, vulcandURI := getOpts(context)

	vctlSync := vctlsync.New(etcdURI, vulcandURI)
	err := vctlSync.Run()
	if err != nil {
		log.Fatalln("error on vctlSync.Run", err.Error())
	}
	os.Exit(0)
}

func getOpts(context *cli.Context) (string, string) {
	etcdURI := context.String("etcd-uri")
	vulcandURI := context.String("vulcand-uri")

	if etcdURI == "" || vulcandURI == "" {
		cli.ShowAppHelp(context)

		if etcdURI == "" {
			color.Red("  Missing required flag --etcd-uri or MINOR_SERVER_UPDATE_ETCD_URI")
		}
		if vulcandURI == "" {
			color.Red("  Missing required flag --vulcand-uri or MINOR_SERVER_UPDATE_VULCAND_URI")
		}
		os.Exit(1)
	}

	return etcdURI, vulcandURI
}

func version() string {
	version, err := semver.NewVersion(VERSION)
	if err != nil {
		errorMessage := fmt.Sprintf("Error with version number: %v", VERSION)
		log.Panicln(errorMessage, err.Error())
	}
	return version.String()
}
