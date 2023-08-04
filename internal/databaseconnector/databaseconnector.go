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

// Package databaseconnector connects to HANA databases with go-hdb driver.
package databaseconnector

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	// Register hdb driver.
	_ "github.com/SAP/go-hdb/driver"
)

type (
	// Params is a struct which holds the information required for connecting to a database.
	Params struct {
		Username       string
		Password       string
		PasswordSecret string
		Host           string
		Port           string
		EnableSSL      bool
		HostNameInCert string
		RootCAFile     string
		GCEService     gceInterface
		Project        string
	}

	// gceInterface is the testable equivalent for gce.GCE for secret manager access.
	gceInterface interface {
		GetSecret(ctx context.Context, projectID, secretName string) (string, error)
	}
)

// Connect creates a SQL connection to a HANA database utilizing the go-hdb driver.
func Connect(ctx context.Context, p Params) (handle *sql.DB, err error) {
	if p.Password == "" && p.PasswordSecret == "" {
		return nil, fmt.Errorf("Could not attempt to connect to database %s, both password and secret name are empty", p.Host)
	}
	if p.Password == "" && p.PasswordSecret != "" {
		if p.Password, err = p.GCEService.GetSecret(ctx, p.Project, p.PasswordSecret); err != nil {
			return nil, err
		}
		log.Logger.Debug("Read from secret manager successful")
	}

	dataSource := "hdb://" + p.Username + ":" + p.Password + "@" + p.Host + ":" + p.Port
	if p.EnableSSL {
		dataSource = dataSource + "?TLSServerName=" + p.HostNameInCert + "&TLSRootCAFile=" + p.RootCAFile
	}

	db, err := sql.Open("hdb", dataSource)
	if err != nil {
		log.Logger.Errorw("Could not open connection to database.", "username", p.Username, "host", p.Host, "port", p.Port, "err", err)
		return nil, err
	}
	log.Logger.Debug("Database connection successful")
	return db, nil
}
