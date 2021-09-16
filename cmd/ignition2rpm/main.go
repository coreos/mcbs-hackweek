package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vincent-petithory/dataurl"

	ign3types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/golang/glog"
	"github.com/google/rpmpack"

	resourceread "github.com/openshift/machine-config-operator/lib/resourceread"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	ctrlcommon "github.com/openshift/machine-config-operator/pkg/controller/common"
)

var Version string
var excludePrefix string

func main() {

	var config string
	var outputRPM string
	var version bool
	var packageCanOverride bool

	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "2")

	flag.StringVar(&excludePrefix, "exclude-prefix", "", "Exclude files with this prefix")
	flag.StringVar(&config, "config", "", "Config file ign/machineconfig to read")
	flag.StringVar(&outputRPM, "output", "", "Specify name of RPM file to write")
	flag.BoolVar(&packageCanOverride, "can-override", false, "Include fake 'provides' for rpm-ostree (https://github.com/coreos/rpm-ostree/pull/3125)")
	flag.BoolVar(&version, "version", false, "show the version ("+Version+")")

	flag.Parse()
	if version {
		fmt.Printf("%s\n", Version)
		os.Exit(0)
	}

	//just the filename so we can use it as the package name
	fileName := strings.TrimSuffix(config, filepath.Ext(config))
	fileName = filepath.Base(fileName)

	var err error

	// use the library functions to figure out what this is
	configi, contentFrom, err := senseAndLoadOnceFrom(config)
	if err != nil {
		glog.Fatalf("Unable to decipher onceFrom config type: %s", err)
	}

	// I might use this later, but not yet
	_ = contentFrom

	//just so we can set the build host
	hostname, _ := os.Hostname()

	//specify some boilerplate metadata
	packedRPM, err := rpmpack.NewRPM(rpmpack.RPMMetaData{
		Name:        fileName,
		Version:     "1",
		Release:     "1",
		Summary:     "A package packed from " + config,
		Description: "This is a machine-packed RPM that has been packed by 'ignition2rpm'",
		BuildTime:   time.Now(),
		Packager:    "MCO ignition2rpm",
		Vendor:      "RedHat OpenShift",
		//Licence:     "",
		BuildHost: hostname,
	},
	)

	if err != nil {
		glog.Fatalf("Failed to create new RPM file: %s", err)
	}

	//Special sauce from: https://github.com/coreos/rpm-ostree/pull/3125
	// add a fake provides that signals to rpm-ostree to let this package override files
	if packageCanOverride {
		packedRPM.RPMMetaData.Provides = append(packedRPM.RPMMetaData.Provides, &rpmpack.Relation{Name: "rpmostree(override)"})
	}

	/*Adding prefixes alone is not enough to make it relocatable (nope, doesn't work)
	magic numbers come from rpmtag.h
	packedRPM.AddCustomTag(1098, rpmpack.EntryStringSlice([]string{"/etc/", "/var/", "usr"}))
	*/

	// have to process the envelope if it's machineconfig, otherwise we don't
	switch c := configi.(type) {
	case ign3types.Config:

		//process the ignition into an RPM
		err = Ign2Rpm(packedRPM, &c)
		if err != nil {
			glog.Fatalf("Unable to convert ignition to RPM payload: %v", err)
		}
	case mcfgv1.MachineConfig:

		//shuck the ignition out of the machineconfig if need be
		//TODO: fish the name out of the machineconfig for the metadata?
		newIgnConfig, err := ctrlcommon.ParseAndConvertConfig(c.Spec.Config.Raw)
		if err != nil {
			glog.Fatalf("Unable to convert machine config to ignition: %v", err)
		}
		err = Ign2Rpm(packedRPM, &newIgnConfig)
		if err != nil {
			glog.Fatalf("Unable to convert ignition to RPM payload: %v", err)
		}
	}

	//make the rpmfile we're going to write to disk
	f, err := os.Create(outputRPM)
	if err != nil {
		panic(err)
	}

	//stuff our RPM file guts into it
	if err := packedRPM.Write(f); err != nil {
		glog.Fatalf("Unable to write RPM %s to disk: %v", outputRPM, err)
	}

	glog.Infof("Wrote %s", outputRPM)

}

type onceFromOrigin int

const (
	onceFromUnknownConfig onceFromOrigin = iota
	onceFromLocalConfig
	onceFromRemoteConfig
)

// blatantly stolen from mco daemon's once-from
func senseAndLoadOnceFrom(onceFrom string) (interface{}, onceFromOrigin, error) {
	var (
		content     []byte
		contentFrom onceFromOrigin
	)
	// Read the content from a remote endpoint if requested
	/* #nosec */
	if strings.HasPrefix(onceFrom, "http://") || strings.HasPrefix(onceFrom, "https://") {
		contentFrom = onceFromRemoteConfig
		resp, err := http.Get(onceFrom)
		if err != nil {
			return nil, contentFrom, err
		}
		defer resp.Body.Close()
		// Read the body content from the request
		content, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, contentFrom, err
		}

	} else {
		// Otherwise read it from a local file
		contentFrom = onceFromLocalConfig
		absoluteOnceFrom, err := filepath.Abs(filepath.Clean(onceFrom))
		if err != nil {
			return nil, contentFrom, err
		}
		content, err = ioutil.ReadFile(absoluteOnceFrom)
		if err != nil {
			return nil, contentFrom, err
		}
	}

	// Try each supported parser
	ignConfig, err := ctrlcommon.ParseAndConvertConfig(content)
	if err == nil && ignConfig.Ignition.Version != "" {
		glog.V(2).Info("onceFrom file is of type Ignition")
		return ignConfig, contentFrom, nil
	}

	// Try to parse as a machine config
	mc, err := resourceread.ReadMachineConfigV1(content)
	if err == nil && mc != nil {
		glog.V(2).Info("onceFrom file is of type MachineConfig")
		return *mc, contentFrom, nil
	}

	return nil, onceFromUnknownConfig, fmt.Errorf("unable to decipher onceFrom config type: %v", err)
}

// makes sure we always get a uint value back, even if nil
func NilMode(obj *int, val uint) uint {
	if obj == nil {
		return val
	}

	octalstring := strconv.FormatInt(int64(*obj), 8)
	octal, _ := strconv.ParseInt(octalstring, 8, 64)
	return uint(octal)

}

// makes sure we always get a string value back, even if nil
func NilString(obj *string, val string) string {
	if obj == nil {
		return val
	}
	return *obj
}

// makes sure we always get a bool value back, even if nil
func NilBool(obj *bool, val bool) bool {
	if obj == nil {
		return val
	}
	return *obj
}

// Rewrites file paths for rpm-ostree so they get linked back into the right place.
// This will break the package though for things like RHEL, because it won't be able to find its toys
func RelocateForRpmOstree(fileName string) string {
	prefixes := map[string]string{"/usr/local/": "/var/usrlocal/"}

	// for each prefix in the list
	for prefix, target := range prefixes {

		// if our file starts with that prefix
		if strings.HasPrefix(fileName, prefix) {

			//replace the prefix with the targer prefix
			replaced := strings.Replace(fileName, prefix, target, 1)
			glog.Infof("REPLACING: %s %s", fileName, replaced)
			return replaced

		}
	}

	return fileName

}

// Converts ignition to an RPM and returns the RPM
func Ign2Rpm(r *rpmpack.RPM, config *ign3types.Config) error {

	packTime := time.Now().Unix()

	// MCO currently support sshkeys
	// yes I'm cheating because I know /var/home becomes /home
	coreUserSSHDir := "/var/home/core/.ssh"
	for _, u := range config.Passwd.Users {
		concatKeys := ""
		if u.Name == "core" {
			glog.Infof("Found the core user, adding authorized_keys")
			for _, key := range u.SSHAuthorizedKeys {
				concatKeys = concatKeys + string(key) + "\n"
			}

		}
		rpmfile := rpmpack.RPMFile{

			Name:  filepath.Join(coreUserSSHDir, "authorized_keys"),
			Body:  []byte(concatKeys),
			Mode:  0644,
			Owner: u.Name,
			Group: u.Name,
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		r.AddFile(rpmfile)

	}

	// TODO: I've never tested directories, don't trust it!
	for _, d := range config.Storage.Directories {
		glog.Infof("DIR: %s (%d %s) (%d %s) %v\n", d.Path, d.User.ID, d.User.Name, d.Group.ID, d.Group.Name, d.Node)

		// rpm-ostree limits where we can put files
		// but it links some of them back into the right spots if we put them where it wants them
		d.Path = RelocateForRpmOstree(d.Path)

		rpmfile := rpmpack.RPMFile{
			Name: d.Path,
			//Body:  []byte(*d.Contents.Source),
			// The Nil<whatever> functions insulate us from nil pointers and return a default value
			Mode:  NilMode(d.Mode, 0755),
			Owner: NilString(d.User.Name, "root"),
			Group: NilString(d.Group.Name, "root"),
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		//tell the rpm library it's a directory. Yes, this could be better
		rpmfile.Mode |= 040000
		r.AddFile(rpmfile)

	}

	// Process the files (this works)
	for _, f := range config.Storage.Files {

		if excludePrefix != "" && strings.HasPrefix(f.Path, excludePrefix) {
			glog.Infof("SKIPPING (prefix): %s\n", f.Path)
			continue
		} else {
			glog.Infof("FILE: %s\n", f.Path)
		}

		var contents *dataurl.DataURL

		if f.Contents.Source != nil {
			var err error
			contents, err = dataurl.DecodeString(*f.Contents.Source)
			if err != nil {
				return err
			}
		}

		//rpm-ostree limits where we can put files
		//but it links some of them back into the right spots
		f.Path = RelocateForRpmOstree(f.Path)
		rpmfile := rpmpack.RPMFile{
			Name: f.Path,
			//Body:  []byte(*f.Contents.Source),
			Body:  []byte(contents.Data),
			Mode:  NilMode(f.Mode, 0755),
			Owner: NilString(f.User.Name, "root"),
			Group: NilString(f.Group.Name, "root"),
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		r.AddFile(rpmfile)
	}

	//TODO: I've never tested links, don't trust it !
	for _, l := range config.Storage.Links {
		glog.Infof("LINK: %s %s\n", l.Path, l.Node.Path)

		// rpm-ostree limits where we can put files
		// but it links some of them back into the right spots if we put them where it wants them
		l.Path = RelocateForRpmOstree(l.Path)
		rpmfile := rpmpack.RPMFile{
			Name:  l.Node.Path,
			Body:  []byte(l.Path),
			Mode:  0755,
			Owner: NilString(l.User.Name, "root"),
			Group: NilString(l.Group.Name, "root"),
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		// magic "this is a link" mode
		rpmfile.Mode |= 00120000
		r.AddFile(rpmfile)

	}

	//Loop through the units, put them in the right spot (this also works)
	for _, u := range config.Systemd.Units {
		unitFile := filepath.Join("/", SystemdUnitsPath(), u.Name)

		glog.Infof("UNIT: %s %s %t\n", u.Name, unitFile, NilBool(u.Enabled, true))
		rpmfile := rpmpack.RPMFile{
			Name:  unitFile,
			Body:  []byte(NilString(u.Contents, "")),
			Mode:  0644,
			Owner: "root",
			Group: "root",
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		r.AddFile(rpmfile)

		//Some of these units may have dropins
		for _, dropin := range u.Dropins {
			dropinFile := filepath.Join("/", SystemdDropinsPath(u.Name), dropin.Name)
			glog.Infof("\tDROPIN: %s %s\n", dropin.Name, dropinFile)
			rpmfile := rpmpack.RPMFile{
				Name:  dropinFile,
				Body:  []byte(NilString(dropin.Contents, "")),
				Mode:  0644,
				Owner: "root",
				Group: "root",
				MTime: uint32(packTime),
				Type:  rpmpack.GenericFile,
			}
			r.AddFile(rpmfile)
		}

	}

	return nil

}

// blatantly stolen from ignition internal libraries
func SystemdUnitsPath() string {
	return filepath.Join("etc", "systemd", "system")
}

func SystemdRuntimeUnitsPath() string {
	return filepath.Join("run", "systemd", "system")
}

func SystemdRuntimeUnitWantsPath(unitName string) string {
	return filepath.Join("run", "systemd", "system", unitName+".wants")
}

func SystemdDropinsPath(unitName string) string {
	return filepath.Join("etc", "systemd", "system", unitName+".d")
}

func SystemdRuntimeDropinsPath(unitName string) string {
	return filepath.Join("run", "systemd", "system", unitName+".d")
}
