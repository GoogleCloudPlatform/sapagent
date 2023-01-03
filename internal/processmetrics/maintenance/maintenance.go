/*
Copyright 2022 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package maintenance package is responsible for maintenance mode handling
// for the GC SAP Agent. It contains functions to configure maintenance mode,
// display the current value for maintenance mode.
// Package maintenance implements processmetrics.Collector interface to collect below
// custom metric:-
//   - /sap/mntmode - A custom metric representing the current value of maintenancemode.
package maintenance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"

	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
)

const (
	// Linux path for the directory containing file for maintenancemode config.
	linuxDirPath = "/usr/sap/google-cloud-sap-agent/conf"

	// Name of the file persisting the maintenancemode config.
	fileName    = "maintenance.json"
	metricURL   = "workload.googleapis.com"
	mntmodePath = "/sap/mntmode"
)

type (
	// FileReader interface provides abstraction on the file reading methods.
	FileReader interface {
		// Read method is responsible for reading the contents of the file name
		// passed. It returns the bytes of the file content in a successful call
		// with a nil error. In case of unsuccessful call it returns nil, error.
		Read(fileName string) ([]byte, error)
	}

	// FileWriter interface provides abstraction on the file writing methods.
	FileWriter interface {
		// Write method is responsible for writing the data passed into the
		// filename passed in the given permission mode. It returns an error in
		// case of an unsuccessful call.
		Write(fileName string, data []byte, perm os.FileMode) error

		// MakeDirs method is responsible for creating the directory named path.
		// It returns an error if unable to do so.
		MakeDirs(path string, perm os.FileMode) error
	}

	// ModeReader is a concrete type responsible for reading the value
	// of maintenance mode from the maintenancemode.json file.
	ModeReader struct{}

	// ModeWriter is a concrete type responsible for writing the value
	// into the maintenancemode.json file.
	ModeWriter struct{}

	// maintenanceModeJson is a concrete type representing the content of
	// maintenancemode.json file.
	maintenanceModeJSON struct {
		MaintenanceMode bool `json:"maintenance_mode"`
	}
)

// Read is the implementation of FileReader interface.
func (mmr ModeReader) Read(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// Write is the implementation of FileWriter interface.
func (mmw ModeWriter) Write(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

// MakeDirs  is the implementation of FileWriter interface.
func (mmw ModeWriter) MakeDirs(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// InstanceProperties have the necessary context for maintenance mode metric collection
type InstanceProperties struct {
	Config *cnfpb.Configuration
	Client cloudmonitoring.TimeSeriesCreator
	Reader FileReader
}

// ReadMaintenanceMode reads the current value for maintenancemode persisted in
// maintenance.json file, it returns the default value for maintenancemode i.e. false along
// with err: nil if the file does not exist.
//
// An unsuccessful call will return false, err
func ReadMaintenanceMode(fr FileReader) (bool, error) {
	content, err := fr.Read(filepath.Join(linuxDirPath, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil || len(content) == 0 {
		log.Logger.Error(fmt.Sprintf("Could not read the file: %s", filepath.Join(linuxDirPath, fileName)), log.Error(err))
		return false, err
	}
	mntModeContent := &maintenanceModeJSON{}
	if err := json.Unmarshal(content, mntModeContent); err != nil {
		log.Logger.Error("Could not parse maintenance.json file, error", log.Error(err))
		return false, err
	}
	return mntModeContent.MaintenanceMode, nil
}

// UpdateMaintenanceMode updates the value for maintenancemode in
// the maintenancemode.json file , it returns the error in case it
// is unable to update the file.
func UpdateMaintenanceMode(mntmode bool, fw FileWriter) error {
	mntModeContent := &maintenanceModeJSON{MaintenanceMode: mntmode}
	marshalContent, _ := json.Marshal(mntModeContent)
	if err := fw.MakeDirs(linuxDirPath, 0777); err != nil {
		log.Logger.Error(fmt.Sprintf("Error making directory %s", linuxDirPath), log.Error(err))
		return err
	}
	if err := fw.Write(filepath.Join(linuxDirPath, fileName), marshalContent, 0777); err != nil {
		log.Logger.Error("Could not write maintenance.json file", log.Error(err))
		return err
	}
	return nil
}

// Collect is a MaintenanceMode implementation of the Collector interface from
// processmetrics. It returns the value of current maintenancemode configured
// as a metric list.
func (p *InstanceProperties) Collect() []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	log.Logger.Debug("Starting maintenancemode metric collection.")
	mntmode, err := ReadMaintenanceMode(p.Reader)
	if err != nil {
		return nil
	}
	log.Logger.Debugf("MaintenanceMode metric is set to %t.", mntmode)
	params := timeseries.Params{
		CloudProp:  p.Config.CloudProperties,
		MetricType: metricURL + mntmodePath,
		Timestamp:  tspb.Now(),
		BoolValue:  mntmode,
		BareMetal:  p.Config.BareMetal,
	}
	ts := timeseries.BuildBool(params)
	return append(metrics, &sapdiscovery.Metrics{TimeSeries: ts})
}
