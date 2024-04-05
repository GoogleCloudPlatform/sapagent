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
	"net/url"

	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
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

	// CMDDBConnection stores connection information for querying via hdbsql command line
	CMDDBConnection struct {
		SIDAdmUser string // system user to run the queries from
		HDBUserKey string // HDB Userstore Key providing auth and instance details
	}

	// DBHandle provides an object to connect to and query databases, abstracting the underlying connector
	DBHandle struct {
		useCMD      bool
		goHDBHandle *sql.DB
		cmdDBHandle *CMDDBConnection
	}

	// QueryResults is a struct to process the results of a database query
	QueryResults struct {
		useCMD      bool
		goHDBResult *sql.Rows
		cmdDBResult string // Stores entire raw query result
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
		log.CtxLogger(ctx).Debug("Read from secret manager successful")
	}

	// Escape the special characters in the password string, HANA studio does this implicitly.
	p.Password = url.QueryEscape(p.Password)
	dataSource := "hdb://" + p.Username + ":" + p.Password + "@" + p.Host + ":" + p.Port
	if p.EnableSSL {
		dataSource = dataSource + "?TLSServerName=" + p.HostNameInCert + "&TLSRootCAFile=" + p.RootCAFile
	}

	db, err := sql.Open("hdb", dataSource)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not open connection to database.", "username", p.Username, "host", p.Host, "port", p.Port, "err", err)
		return nil, err
	}
	log.CtxLogger(ctx).Debug("Database connection successful")
	return db, nil
}

// Database Handle Functions for Querying and handling results

// QueryContext Queries the database via the goHDB driver or cmdline accordingly
func (db *DBHandle) QueryContext(ctx context.Context, query string, exec commandlineexecutor.Execute) (*QueryResults, error) {
	if db.useCMD {
		// TODO: Implement cmdline querying logic
		return nil, nil
	}
	// Query via go HDB Driver
	result, err := db.goHDBHandle.QueryContext(ctx, query)
	return &QueryResults{
		useCMD:      false,
		goHDBResult: result,
	}, err
}

// ReadRow parses the next row of results into destination
func (qr *QueryResults) ReadRow(dest ...any) error {
	// TODO: Implement row parsing logic
	return nil
}
