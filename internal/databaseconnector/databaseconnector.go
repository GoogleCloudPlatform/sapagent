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
	"strings"

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
		HDBUserKey     string
		SID            string
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

// NewGoDBHandle instantiates a go-hdb driver database handle.
// TODO: The current logic in Connect() will go here, and Connect() will call either of the
// two constructors based on the connecting parameters it gets.
func NewGoDBHandle(ctx context.Context, p Params) (*DBHandle, error) {
	// TODO: Implement Constructor for go-hdb connector
	return nil, nil
}

// NewCMDDBHandle instantiates a command-line database handle
func NewCMDDBHandle(p Params) (*DBHandle, error) {
	if p.SID == "" {
		return nil, fmt.Errorf("sid not provided")
	}
	if p.HDBUserKey == "" {
		return nil, fmt.Errorf("hdb userstore Key not provided")
	}

	cmdDBconnection := CMDDBConnection{
		SIDAdmUser: strings.ToLower(p.SID),
		HDBUserKey: p.HDBUserKey,
	}

	return &DBHandle{
		useCMD:      true,
		cmdDBHandle: &cmdDBconnection,
	}, nil
}

// Query queries the database via the goHDB driver or command-line accordingly.
func (db *DBHandle) Query(ctx context.Context, query string, exec commandlineexecutor.Execute) (*QueryResults, error) {
	if db.useCMD {
		sidadmArgs := []string{"-i", "-u", fmt.Sprintf("%sadm", db.cmdDBHandle.SIDAdmUser)}                           // Arguments to run command in sidadm user
		hdbsqlArgs := []string{"hdbsql", "-U", db.cmdDBHandle.HDBUserKey, "-a", "-x", "-quiet", "-Z", "CHOPBLANKS=0"} // Arguments to run hdbsql query in parse-able format

		// Builds a command equivalent to $sudo -i -u <sidadm> hdbsql -U <key> -a -x -quiet <query>
		args := append(sidadmArgs, hdbsqlArgs...)
		args = append(args, query)

		result := exec(ctx, commandlineexecutor.Params{
			Executable: "sudo",
			Args:       args,
		})
		if result.Error != nil || result.ExitCode != 0 {
			log.CtxLogger(ctx).Errorw("Running hdbsql query failed", " stdout:", result.StdOut, " stderr", result.StdErr, " error", result.Error)
			return nil, fmt.Errorf(result.StdErr)
		}

		return &QueryResults{
			useCMD:      true,
			cmdDBResult: result.StdOut,
		}, nil
	}
	// Query via go HDB Driver
	result, err := db.goHDBHandle.QueryContext(ctx, query)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Query execution failed, err", err)
		return nil, err
	}

	return &QueryResults{
		useCMD:      false,
		goHDBResult: result,
	}, nil

}

// ReadRow parses the next row of results into destination
func (qr *QueryResults) ReadRow(dest ...any) error {
	// TODO: Implement row parsing logic
	return nil
}
