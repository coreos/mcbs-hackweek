package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vincent-petithory/dataurl"

	ign3types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/golang/glog"
	"github.com/google/rpmpack"
)

const (
	rootUsername string = "root"
	coreUsername string = "core"
)

type RPMPacker struct {
	packedRPM *rpmpack.RPM
	packTime  uint32
	sshKeys   []string
}

func NewRPMFromIgnition(o opts, config ign3types.Config) (*rpmpack.RPM, error) {
	r, err := newRPMPacker(o)
	if err != nil {
		return nil, fmt.Errorf("could not create RPM: %w", err)
	}

	for _, u := range config.Passwd.Users {
		// Like the MCO, we only support the default core user
		if u.Name == coreUsername {
			glog.Infof("Found the core user, adding authorized_keys")
			for _, key := range u.SSHAuthorizedKeys {
				r.AddSSHKey(key)
			}
		}
	}

	// TODO: I've never tested directories, don't trust it!
	for _, d := range config.Storage.Directories {
		r.AddDirectory(d)
	}

	// Process the files (this works)
	for _, f := range config.Storage.Files {
		if o.isExcluded(f.Path) {
			glog.Infof("SKIPPING (prefix): %s\n", f.Path)
			continue
		} else {
			glog.Infof("FILE: %s\n", f.Path)
			if err := r.AddFile(f); err != nil {
				return nil, fmt.Errorf("could not add file to RPM: %w", err)
			}
		}
	}

	//TODO: I've never tested links, don't trust it !
	for _, l := range config.Storage.Links {
		r.AddLink(l)
	}

	//Loop through the units, put them in the right spot (this also works)
	for _, u := range config.Systemd.Units {
		r.AddUnit(u)
	}

	return r.Pack(), nil
}

func newRPMPacker(o opts) (*RPMPacker, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	metadata := rpmpack.RPMMetaData{
		Name:        o.packageName(),
		Version:     "1",
		Release:     "1",
		Summary:     "A package packed from " + o.config,
		Description: "This is a machine-packed RPM that has been packed by 'ignition2rpm'",
		BuildTime:   time.Now(),
		Packager:    "MCO ignition2rpm",
		Vendor:      "RedHat OpenShift",
		//Licence:     "",
		BuildHost: hostname,
	}

	pRPM, err := rpmpack.NewRPM(metadata)
	if err != nil {
		return nil, err
	}

	//Special sauce from: https://github.com/coreos/rpm-ostree/pull/3125
	// add a fake provides that signals to rpm-ostree to let this package override files
	if o.packageCanOverride {
		pRPM.RPMMetaData.Provides = append(pRPM.RPMMetaData.Provides, &rpmpack.Relation{Name: "rpmostree(override)"})
	}

	return &RPMPacker{
		packedRPM: pRPM,
		sshKeys:   []string{},
		packTime:  uint32(metadata.BuildTime.Unix()),
	}, nil
}

func (r *RPMPacker) AddSSHKey(key ign3types.SSHAuthorizedKey) {
	r.sshKeys = append(r.sshKeys, string(key))
}

func (r *RPMPacker) AddFile(f ign3types.File) error {
	var contents *dataurl.DataURL

	if f.Contents.Source != nil {
		var err error
		contents, err = dataurl.DecodeString(*f.Contents.Source)
		if err != nil {
			return err
		}
	}

	r.addRPMFile(rpmpack.RPMFile{
		Name: RelocateForRpmOstree(f.Path),
		//Body:  []byte(*f.Contents.Source),
		Body:  []byte(contents.Data),
		Mode:  NilMode(f.Mode, 0755),
		Owner: NilString(f.User.Name, rootUsername),
		Group: NilString(f.Group.Name, rootUsername),
	})

	return nil
}

func (r *RPMPacker) AddDirectory(d ign3types.Directory) {
	glog.Infof("DIR: %s (%d %s) (%d %s) %v\n", d.Path, d.User.ID, d.User.Name, d.Group.ID, d.Group.Name, d.Node)

	rpmfile := rpmpack.RPMFile{
		Name: RelocateForRpmOstree(d.Path),
		//Body:  []byte(*d.Contents.Source),
		// The Nil<whatever> functions insulate us from nil pointers and return a default value
		Mode:  NilMode(d.Mode, 0755),
		Owner: NilString(d.User.Name, "root"),
		Group: NilString(d.Group.Name, "root"),
	}

	//tell the rpm library it's a directory. Yes, this could be better
	rpmfile.Mode |= 040000

	r.addRPMFile(rpmfile)
}

func (r *RPMPacker) AddLink(l ign3types.Link) {
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
	}

	// magic "this is a link" mode
	rpmfile.Mode |= 00120000

	r.addRPMFile(rpmfile)
}

func (r *RPMPacker) AddUnit(u ign3types.Unit) {
	unitFile := filepath.Join("/", SystemdUnitsPath(), u.Name)

	glog.Infof("UNIT: %s %s %t\n", u.Name, unitFile, NilBool(u.Enabled, true))
	r.addRPMFile(rpmpack.RPMFile{
		Name: unitFile,
		Body: []byte(NilString(u.Contents, "")),
		Mode: 0644,
	})

	//Some of these units may have dropins
	for _, dropin := range u.Dropins {
		dropinFile := filepath.Join("/", SystemdDropinsPath(u.Name), dropin.Name)
		glog.Infof("\tDROPIN: %s %s\n", dropin.Name, dropinFile)
		r.addRPMFile(rpmpack.RPMFile{
			Name: dropinFile,
			Body: []byte(NilString(dropin.Contents, "")),
			Mode: 0644,
		})
	}
}

func (r *RPMPacker) Pack() *rpmpack.RPM {
	if len(r.sshKeys) > 0 {
		r.addRPMFile(r.sshKeysToRPMFile())
	}

	return r.packedRPM
}

func (r *RPMPacker) addRPMFile(rpmFile rpmpack.RPMFile) {
	rpmFile.MTime = r.packTime
	rpmFile.Type = rpmpack.GenericFile

	r.packedRPM.AddFile(rpmFile)
}

func (r *RPMPacker) sshKeysToRPMFile() rpmpack.RPMFile {
	out := bytes.NewBuffer([]byte{})

	for _, key := range r.sshKeys {
		fmt.Fprintln(out, key)
	}

	return rpmpack.RPMFile{
		// MCO currently support sshkeys
		// yes I'm cheating because I know /var/home becomes /home
		Name:  "/var/home/core/.ssh/authorized_keys",
		Body:  out.Bytes(),
		Mode:  0644,
		Owner: coreUsername,
		Group: coreUsername,
	}
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
