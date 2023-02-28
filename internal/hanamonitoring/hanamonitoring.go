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

// Package hanamonitoring queries HANA databases and sends the results as metrics to Cloud Monitoring.
package hanamonitoring

import (
	"context"
	"database/sql"

	"github.com/GoogleCloudPlatform/sapagent/internal/hanamonitoring/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

type gceInterface interface {
	GetSecret(ctx context.Context, projectID, secretName string) (string, error)
}

// Parameters hold the parameters necessary to invoke Start().
type Parameters struct {
	Config     *cpb.Configuration
	GCEService gceInterface
}

// ConnectToDatabases attempts to create a *sql.DB connection for each HANAInstance.
func ConnectToDatabases(ctx context.Context, params Parameters) []*sql.DB {
	if params.Config == nil || params.Config.GetHanaMonitoringConfiguration() == nil {
		return nil
	}

	var databases []*sql.DB
	for _, i := range params.Config.GetHanaMonitoringConfiguration().GetHanaInstances() {
		password := i.GetPassword()
		if password == "" && i.GetSecretName() == "" {
			log.Logger.Errorf("Both password and secret name are empty, cannot connect to database %s", i.GetName())
			continue
		}

		if password == "" && i.GetSecretName() != "" {
			if secret, err := params.GCEService.GetSecret(ctx, params.Config.GetCloudProperties().GetProjectId(), i.GetSecretName()); err == nil {
				password = secret
			}
		}

		if database, err := databaseconnector.Connect(i.GetUser(), password, i.GetHost(), i.GetPort()); err == nil {
			databases = append(databases, database)
		}
	}
	return databases
}
