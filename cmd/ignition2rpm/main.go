package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
)

var Version string

type opts struct {
	excludePrefix      string
	config             string
	outputRPM          string
	packageCanOverride bool
}

func (o opts) isExcluded(path string) bool {
	return o.excludePrefix != "" && strings.HasPrefix(path, o.excludePrefix)
}

func (o opts) packageName() string {
	//just the filename so we can use it as the package name
	fileName := strings.TrimSuffix(o.config, filepath.Ext(o.config))

	return filepath.Base(fileName)
}

func main() {
	o := opts{}

	var version bool

	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "2")

	flag.StringVar(&o.excludePrefix, "exclude-prefix", "", "Exclude files with this prefix")
	flag.StringVar(&o.config, "config", "", "Config file ign/machineconfig to read")
	flag.StringVar(&o.outputRPM, "output", "", "Specify name of RPM file to write (optional for MachineConfigs)")
	flag.BoolVar(&o.packageCanOverride, "can-override", false, "Include fake 'provides' for rpm-ostree (https://github.com/coreos/rpm-ostree/pull/3125)")
	flag.BoolVar(&version, "version", false, "show the version ("+Version+")")

	flag.Parse()
	if version {
		fmt.Printf("%s\n", Version)
		os.Exit(0)
	}

	var err error

	// use the library functions to figure out what this is
	configi, contentFrom, err := senseAndLoadOnceFrom(o.config)
	if err != nil {
		glog.Fatal(err)
	}

	// I might use this later, but not yet
	_ = contentFrom

	/*Adding prefixes alone is not enough to make it relocatable (nope, doesn't work)
	magic numbers come from rpmtag.h
	packedRPM.AddCustomTag(1098, rpmpack.EntryStringSlice([]string{"/etc/", "/var/", "usr"}))
	*/

	ignConfig, err := processEnvelope(configi, &o)
	if err != nil {
		glog.Fatalf("Unable to get ignition config from %s, check inputs: %v", o.config, err)
	}

	rpm, err := NewRPMFromIgnition(o, *ignConfig)
	if err != nil {
		glog.Fatalf("Unable to convert ignition to RPM payload: %v", err)
	}

	//make the rpmfile we're going to write to disk
	f, err := os.Create(o.outputRPM)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	//stuff our RPM file guts into it
	if err := rpm.Write(f); err != nil {
		glog.Fatalf("Unable to write RPM %s to disk: %v", o.outputRPM, err)
	}

	glog.Infof("Wrote %s", o.outputRPM)
}
