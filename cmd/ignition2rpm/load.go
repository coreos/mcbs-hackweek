package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	ign3types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"

	resourceread "github.com/openshift/machine-config-operator/lib/resourceread"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	ctrlcommon "github.com/openshift/machine-config-operator/pkg/controller/common"

	butaneConfig "github.com/coreos/butane/config"
	butaneOpts "github.com/coreos/butane/config/common"
	"k8s.io/apimachinery/pkg/util/sets"
)

type onceFromOrigin int

const (
	onceFromUnknownConfig onceFromOrigin = iota
	onceFromLocalConfig
	onceFromRemoteConfig
)

func processEnvelope(content interface{}, o *opts) (*ign3types.Config, error) {
	// have to process the envelope if it's machineconfig, otherwise we don't
	switch c := content.(type) {
	case ign3types.Config:
		return &c, nil
	case mcfgv1.MachineConfig:
		//shuck the ignition out of the machineconfig if need be
		//TODO: fish the name out of the machineconfig for the metadata?
		newIgnConfig, err := ctrlcommon.ParseAndConvertConfig(c.Spec.Config.Raw)
		if err != nil {
			return nil, fmt.Errorf("unable to convert machine config to ignition: %w", err)
		}

		// If output RPM is not set and we have a machine config, use the machine
		// config name
		if o.outputRPM == "" {
			o.outputRPM = c.GetName()
		}

		return &newIgnConfig, nil
	}

	return nil, fmt.Errorf("unknown input type")
}

func loadConfig(onceFrom string) ([]byte, onceFromOrigin, error) {
	var (
		contentSrc onceFromOrigin
		err        error
		reader     io.ReadCloser
	)

	defer func() {
		if reader != nil {
			reader.Close()
		}
	}()

	if strings.HasPrefix(onceFrom, "http://") || strings.HasPrefix(onceFrom, "https://") {
		resp, err := http.Get(onceFrom)
		if err != nil {
			return []byte{}, onceFromRemoteConfig, err
		}

		reader = resp.Body
		contentSrc = onceFromRemoteConfig
	} else {
		// Otherwise read it from a local file
		absoluteOnceFrom, err := filepath.Abs(filepath.Clean(onceFrom))
		if err != nil {
			return []byte{}, onceFromLocalConfig, err
		}

		local, err := os.Open(absoluteOnceFrom)
		if err != nil {
			return []byte{}, onceFromLocalConfig, err
		}

		reader = local
		contentSrc = onceFromLocalConfig
	}

	content, err := ioutil.ReadAll(reader)
	return content, contentSrc, err
}

// blatantly stolen from mco daemon's once-from
func senseAndLoadOnceFrom(onceFrom string) (interface{}, onceFromOrigin, error) {
	content, contentFrom, err := loadConfig(onceFrom)
	if err != nil {
		return nil, contentFrom, fmt.Errorf("could not load content: %w", err)
	}

	// Try each supported parser
	// Ignition
	ignConfig, err := ctrlcommon.ParseAndConvertConfig(content)
	if err == nil && ignConfig.Ignition.Version != "" {
		glog.V(2).Info("onceFrom file is of type Ignition")
		return ignConfig, contentFrom, nil
	}

	// Machine Config
	// Note: This will only read the first machine config in a given input file.
	// The rest will be ignored.
	mc, err := resourceread.ReadMachineConfigV1(content)
	if err == nil && mc != nil {
		glog.V(2).Info("onceFrom file is of type MachineConfig")
		return *mc, contentFrom, nil
	}

	// Butane
	ign, err := parseButane(content)
	if err == nil {
		glog.V(2).Info("onceFrom file is of type Butane")
		return *ign, contentFrom, nil
	}

	return nil, onceFromUnknownConfig, fmt.Errorf("unable to decipher onceFrom config type: %w", err)
}

func parseButane(butaneBytes []byte) (*ign3types.Config, error) {
	// The following Butane variants and versions are unsupported:
	// - FCOS 1.4.0 - parses into Ignition 3.3.
	// - FCOS 1.5.0 - parses into Ignition 3.4 (experimental).
	// - Openshift 4.10.0 - parses into Ingition 3.4 (experimental).
	//
	// The issue with supporting Ignition 3.3 and 3.4 is that the
	// ctrlcommon.ParseAndConvertConfig() function assumes a max Ignition version
	// of 3.2, so it throws an error.
	//
	// Translating to 3.3 / 3.4 is outside the scope, as is adding the support to
	// ctrlcommon.ParseAndConvertConfig(). So for now, we'll just exclude those
	// versions and variants.
	//
	// This pattern was inspired by:
	// https://github.com/coreos/butane/blob/dcc128af5a36d81121e6af2ffa3305be74cb46dc/config/config.go
	type butane struct {
		Version string `yaml:"version"`
		Variant string `yaml:"variant"`
	}

	b := butane{}

	if err := yaml.Unmarshal(butaneBytes, &b); err != nil {
		return nil, err
	}

	supportedButaneVersions := map[string]map[string]sets.Empty{
		"fcos": map[string]sets.Empty{
			"1.0.0": sets.Empty{},
			"1.1.0": sets.Empty{},
			"1.2.0": sets.Empty{},
			"1.3.0": sets.Empty{},
		},
		"openshift": map[string]sets.Empty{
			"4.8.0": sets.Empty{},
			"4.9.0": sets.Empty{},
		},
		"rhcos": map[string]sets.Empty{
			"0.1.0": sets.Empty{},
		},
	}

	versionsForVariant, ok := supportedButaneVersions[b.Variant]
	if !ok {
		return nil, fmt.Errorf("unsupported butane variant: %s; supported variants %s", b.Variant, sortedMapKeys(supportedButaneVersions))
	}

	if _, ok := versionsForVariant[b.Version]; !ok {
		return nil, fmt.Errorf("unsupported butane version: %s for variant %s; supported versions %s", b.Version, b.Variant, sortedMapKeys(versionsForVariant))
	}

	ignBytes, _, err := butaneConfig.TranslateBytes(butaneBytes, butaneOpts.TranslateBytesOptions{
		// We always want Ignition configs, not MachineConfigs
		// See: https://github.com/coreos/butane/blob/dcc128af5a36d81121e6af2ffa3305be74cb46dc/config/openshift/v4_8/translate.go#L139-L145
		Raw: true,
	})

	if err != nil {
		return nil, err
	}

	ignConfig, err := ctrlcommon.ParseAndConvertConfig(ignBytes)
	return &ignConfig, err
}

func sortedMapKeys(theMap interface{}) string {
	return "(" + strings.Join(sets.StringKeySet(theMap).List(), ", ") + ")"
}
