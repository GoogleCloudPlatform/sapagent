/*
Copyright 2023 Google LLC

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

// Package discovery is the module containing one time execution for HANA discovery.
package discovery

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	hdpb "github.com/GoogleCloudPlatform/sapagent/protos/gcbdrhanadiscovery"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	discoveryScriptPath     = "/act/custom_apps/discoverySAP.sh"
	discoverySAPHANAXMLPath = "/act/custom_apps/SAPHANA.xml"
)

// Applications struct for GCBDR CoreApp discovery script. It contains the list of applications
// struct.
type Applications struct {
	XMLName     xml.Name      `xml:"applications"`
	Application []Application `xml:"application"`
}

// Application struct for GCBDR CoreApp discovery script.
type Application struct {
	Name              string            `xml:"name,attr"`
	Friendlytype      string            `xml:"friendlytype,attr"`
	Instance          string            `xml:"instance,attr"`
	DBSID             string            `xml:"DBSID,attr"`
	PORT              string            `xml:"PORT,attr"`
	DBPORT            string            `xml:"DBPORT,attr"`
	Version           string            `xml:"version,attr"`
	Datavolowner      string            `xml:"datavolowner,attr"`
	Hananodes         string            `xml:"hananodes,attr"`
	Masternode        string            `xml:"masternode,attr"`
	Standbynode       string            `xml:"standbynode,attr"`
	Extendedworker    string            `xml:"extendedworker,attr"`
	Keyname           string            `xml:"keyname,attr"`
	Dbnames           string            `xml:"dbnames,attr"`
	UUID              string            `xml:"uuid,attr"`
	Hardwarekey       string            `xml:"hardwarekey,attr"`
	Sitename          string            `xml:"sitename,attr"`
	Configtype        string            `xml:"configtype,attr"`
	Clustertype       string            `xml:"clustertype,attr"`
	ReplicationNodes  string            `xml:"replication_nodes,attr"`
	Files             Files             `xml:"files"`
	Logbackuppath     Logbackuppath     `xml:"logbackuppath"`
	Globalinipath     Globalinipath     `xml:"globalinipath"`
	Catalogbackuppath Catalogbackuppath `xml:"catalogbackuppath"`
	Logmode           string            `xml:"logmode,attr"`
	Scripts           Scripts           `xml:"scripts"`
	Volumes           Volumes           `xml:"volumes"`
}

// Files struct for GCBDR CoreApp discovery script.
type Files struct {
	File []File `xml:"file"`
}

// File struct for GCBDR CoreApp discovery script.
type File struct {
	Path    string `xml:"path,attr"`
	Datavol string `xml:"datavol,attr"`
}

// Logbackuppath struct for GCBDR CoreApp discovery script.
type Logbackuppath struct {
	File File `xml:"file"`
}

// Globalinipath struct for GCBDR CoreApp discovery script.
type Globalinipath struct {
	File File `xml:"file"`
}

// Catalogbackuppath struct for GCBDR CoreApp discovery script.
type Catalogbackuppath struct {
	File File `xml:"file"`
}

// Scripts struct for GCBDR CoreApp discovery script.
type Scripts struct {
	Script []Script `xml:"script"`
}

// Script struct for GCBDR CoreApp discovery script.
type Script struct {
	Phase string `xml:"phase,attr"`
	Path  string `xml:"path,attr"`
}

// Volumes struct for GCBDR CoreApp discovery script.
type Volumes struct {
	Volume []Volume `xml:"volume"`
}

// Volume struct for GCBDR CoreApp discovery script.
type Volume struct {
	Name       string  `xml:"name,attr"`
	Mountpoint string  `xml:"mountpoint,attr"`
	Vgname     string  `xml:"vgname,attr"`
	Lvname     string  `xml:"lvname,attr"`
	Pddisks    Pddisks `xml:"pddisks"`
}

// Pddisks struct for GCBDR CoreApp discovery script.
type Pddisks struct {
	Pd []Pd `xml:"pd"`
}

// Pd struct for GCBDR CoreApp discovery script.
type Pd struct {
	Disk       string `xml:"disk,attr"`
	Devicename string `xml:"devicename,attr"`
}

// Discovery struct has arguments for discovery subcommand.
type Discovery struct {
	FSH               filesystem.FileSystem
	help              bool
	logLevel, logPath string
}

// Name implements the subcommand interface for Discovery.
func (*Discovery) Name() string { return "gcbdr-discovery" }

// Synopsis implements the subcommand interface for Discovery.
func (*Discovery) Synopsis() string {
	return "deep discovery on the HANA DB to discover HANA DB attributes, PD disks supporting Data and log volumes and PD properties."
}

// Usage implements the subcommand interface for Discovery.
func (*Discovery) Usage() string {
	return "Usage: gcbdr-discovery [-h] [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>]\n"
}

// SetFlags implements the subcommand interface for Discovery.
func (d *Discovery) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&d.help, "h", false, "Display help")
	fs.StringVar(&d.logLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.StringVar(&d.logPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/gcbdr-discovery.log")
}

// Execute implements the subcommand interface for Discovery.
func (d *Discovery) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     d.Name(),
		Help:     d.help,
		LogLevel: d.logLevel,
		LogPath:  d.logPath,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}

	_, exitStatus = d.discoveryHandler(ctx, f, commandlineexecutor.ExecuteCommand, d.FSH)
	return exitStatus
}

func (d *Discovery) discoveryHandler(ctx context.Context, fs *flag.FlagSet, exec commandlineexecutor.Execute, fsh filesystem.FileSystem) (*Applications, subcommands.ExitStatus) {
	log.CtxLogger(ctx).Info("Starting HANA DB discovery using GCBDR CoreAPP script")
	args := commandlineexecutor.Params{
		Executable:  "/bin/bash",
		ArgsToSplit: "/act/custom_apps/discoverySAP.sh",
	}
	res := exec(ctx, args)
	if res.ExitCode != 0 {
		log.CtxLogger(ctx).Errorf("Failed to execute GCBDR CoreAPP script %v", res.StdErr)
		return nil, subcommands.ExitFailure
	}
	xmlContent, err := fsh.ReadFile(discoverySAPHANAXMLPath)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not read the file for HANA discovery", "file", discoverySAPHANAXMLPath, "error", err)
		return nil, subcommands.ExitFailure
	}
	apps := &Applications{}
	err = xml.Unmarshal(xmlContent, apps)
	if err != nil {
		log.CtxLogger(ctx).Errorf("Failed to unmarshal GCBDR CoreAPP script: %v", err)
		return nil, subcommands.ExitFailure
	}
	log.CtxLogger(ctx).Info("HANA Applications discovered %v", apps.Application)
	return apps, subcommands.ExitSuccess
}

// GetHANADiscoveryApplications returns the list of HANA discovery applications.
func (d *Discovery) GetHANADiscoveryApplications(ctx context.Context, fs *flag.FlagSet, exec commandlineexecutor.Execute, fsh filesystem.FileSystem) (*hdpb.ApplicationsList, error) {
	apps, exitStatus := d.discoveryHandler(ctx, fs, exec, fsh)
	if exitStatus != subcommands.ExitSuccess {
		log.CtxLogger(ctx).Errorf("Failed to get HANA discovery applications: %v", exitStatus)
		return nil, errors.New("Failed to get HANA discovery applications")
	}
	result := constructApplicationsProto(apps)
	return result, nil
}

func constructApplicationsProto(apps *Applications) *hdpb.ApplicationsList {
	if len(apps.Application) == 0 {
		return nil
	}
	result := &hdpb.ApplicationsList{}
	for _, app := range apps.Application {
		protoApp := hdpb.Application{
			Name:              app.Name,
			Dbsid:             app.DBSID,
			Type:              app.Friendlytype,
			HanaVersion:       app.Version,
			ConfigType:        app.Configtype,
			HardwareKey:       app.Hardwarekey,
			Port:              app.PORT,
			HanaNodes:         app.Hananodes,
			MasterNode:        app.Masternode,
			ReplicationNodes:  app.ReplicationNodes,
			Instance:          app.Instance,
			CatalogBackupPath: app.Catalogbackuppath.File.Path,
			GlobalInitPath:    app.Globalinipath.File.Path,
			DataVolumeOwner:   app.Datavolowner,
			DbNames:           app.Dbnames,
		}
		setPDVolumes(&protoApp, app.Volumes)
		result.Apps = append(result.Apps, &protoApp)
	}
	return result
}

func setPDVolumes(protoApp *hdpb.Application, volumes Volumes) {
	if len(volumes.Volume) == 0 {
		return
	}
	vols := []*hdpb.VolumePD{}
	for _, vol := range volumes.Volume {
		protoVol := &hdpb.VolumePD{
			MountPoint:  vol.Mountpoint,
			VolumeType:  vol.Name,
			VolumeGroup: vol.Vgname,
		}
		switch vol.Name {
		case "datavol":
			protoVol.VolumeName = fmt.Sprintf("%s/DB%s", vol.Mountpoint, protoApp.GetDbsid())
			protoVol.LogicalName = "data"
		case "logvol":
			protoVol.VolumeName = fmt.Sprintf("%s/DB%s", vol.Mountpoint, protoApp.GetDbsid())
			protoVol.LogicalName = "log"
		case "logbackupvol":
			protoVol.VolumeName = fmt.Sprintf("%s/log", vol.Mountpoint)
			protoVol.LogicalName = "logbackup"
		}
		vols = append(vols, protoVol)
	}
	protoApp.VolumeDetails = vols
}
