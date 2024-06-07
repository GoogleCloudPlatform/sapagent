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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const (
	// Linux path for the directory containing file for maintenancemode config.
	linuxDirPath = "/etc/google-cloud-sap-agent/"

	// The file stores the maintenancemode config.
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

	// ModeReader is a concrete type responsible for reading the contents of maintenance.json file.
	ModeReader struct{}

	// ModeWriter is a concrete type responsible for writing the value
	// into the maintenance.json file.
	ModeWriter struct{}

	// maintenanceModeJson is a concrete type representing the content of
	// maintenance.json file.
	maintenanceModeJSON struct {
		// SIDs contain the SAP SIDs under maintenance.
		SIDs []string `json:"sids"`
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
	Config          *cnfpb.Configuration
	Client          cloudmonitoring.TimeSeriesCreator
	Reader          FileReader
	Sids            map[string]bool
	SkippedMetrics  map[string]bool
	PMBackoffPolicy backoff.BackOffContext
}

// ReadMaintenanceMode reads the current value for the SIDs under maintenance persisted in
// maintenance.json file, If the file is empty or it does not exist no sid is considered under
// maintenance.
// An unsuccessful call will return nil, err
func ReadMaintenanceMode(fr FileReader) ([]string, error) {
	content, err := fr.Read(filepath.Join(linuxDirPath, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil || len(content) == 0 {
		log.Logger.Errorw("Could not read the file", "file", filepath.Join(linuxDirPath, fileName), "error", err)
		return nil, err
	}
	mntModeContent := &maintenanceModeJSON{}
	if err := json.Unmarshal(content, mntModeContent); err != nil {
		log.Logger.Errorw("Could not parse maintenance.json file, error", log.Error(err))
		return nil, err
	}
	return mntModeContent.SIDs, nil
}

// UpdateMaintenanceMode updates the maintenance.json file by appending / removing the sid passed
// in the arguments based on the mntmode value passed.
func UpdateMaintenanceMode(mntmode bool, sid string, fr FileReader, fw FileWriter) ([]string, error) {
	sidsUnderMaintenance, err := ReadMaintenanceMode(fr)
	if err != nil {
		log.Logger.Errorw("Could not read maintenance.json file", log.Error(err))
		return nil, err
	}
	ind := indexOf(sidsUnderMaintenance, sid)
	// SID not found in the slice
	if ind == -1 {
		if !mntmode {
			return sidsUnderMaintenance, fmt.Errorf("SID: %s is not in maintenance mode already", sid)
		}
		sidsUnderMaintenance = append(sidsUnderMaintenance, sid)
	} else {
		if mntmode {
			log.Logger.Debugw("SID is already in maintenance mode.", "sid", sid)
			return sidsUnderMaintenance, fmt.Errorf("SID: %s is already in maintenance mode", sid)
		}
		sidsUnderMaintenance = removeSID(sidsUnderMaintenance, ind)
	}
	mntModeContent := &maintenanceModeJSON{SIDs: sidsUnderMaintenance}
	marshalContent, _ := json.Marshal(mntModeContent)
	if err := fw.MakeDirs(linuxDirPath, 0777); err != nil {
		log.Logger.Errorw("Error making directory", "directory", linuxDirPath, "error", err)
		return nil, err
	}
	if err := fw.Write(filepath.Join(linuxDirPath, fileName), marshalContent, 0777); err != nil {
		log.Logger.Errorw("Could not write maintenance.json file", log.Error(err))
		return nil, err
	}
	return sidsUnderMaintenance, nil
}

func removeSID(SIDs []string, ind int) []string {
	last := len(SIDs) - 1
	swapper := reflect.Swapper(SIDs)
	swapper(ind, last)
	SIDs = SIDs[:last]
	return SIDs
}

// Collect is a MaintenanceMode implementation of the Collector interface from
// processmetrics. It returns the value of current maintenancemode configured per sid as a metric
// list.
func (p *InstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var metrics []*mrpb.TimeSeries
	if _, ok := p.SkippedMetrics[mntmodePath]; ok {
		return metrics, nil
	}
	log.CtxLogger(ctx).Debug("Starting maintenancemode metric collection.")
	sidsUnderMaintenance, err := ReadMaintenanceMode(p.Reader)
	if err != nil {
		return nil, err
	}
	for sid := range p.Sids {
		mntmode := contains(sidsUnderMaintenance, sid)
		labels := make(map[string]string)
		labels["sid"] = sid
		log.CtxLogger(ctx).Debugw("MaintenanceMode metric for SID", "sid", sid, "maintenancemode", mntmode)
		params := timeseries.Params{
			CloudProp:    p.Config.CloudProperties,
			MetricType:   metricURL + mntmodePath,
			MetricLabels: labels,
			Timestamp:    tspb.Now(),
			BoolValue:    mntmode,
			BareMetal:    p.Config.BareMetal,
		}
		metrics = append(metrics, timeseries.BuildBool(params))
	}
	return metrics, nil
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *InstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		var err error
		res, err = p.Collect(ctx)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Error in Collection", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Debug("Retry limit exceeded", "error", err)
	}
	return res, err
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

func indexOf(list []string, item string) int {
	for i, v := range list {
		if v == item {
			return i
		}
	}
	return -1
}
