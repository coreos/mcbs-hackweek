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

	resourceread "github.com/openshift/machine-config-operator/lib/resourceread"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	ctrlcommon "github.com/openshift/machine-config-operator/pkg/controller/common"
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

	defer reader.Close()

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

	return nil, onceFromUnknownConfig, fmt.Errorf("unable to decipher onceFrom config type: %w", err)
}
