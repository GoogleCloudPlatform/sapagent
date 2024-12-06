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
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SAP/go-hdb/driver"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

var (
	// authErrorCodes is a list of error codes that are related to authentication according to the
	// SAP documentation found here: https://help.sap.com/docs/hana-cloud-database/sap-hana-cloud-sap-hana-database-sql-reference-guide/sql-error-codes
	authErrorCodes = []int{10, 332, 414, 415, 416}
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
		PingSpec       *PingSpec // Allows for testing a connection to a database.
	}

	pingImpl func(ctx context.Context, db *DBHandle) error

	// PingSpec is a struct which holds values for establishing an open connection to a database.
	PingSpec struct {
		MaxRetries int           // The total number of ping attempts will be MaxRetries + 1.
		Timeout    time.Duration // In practice, a timeout of at least 2 minutes is required for a successful connection to a database that is coming online.
		pingImpl   pingImpl
	}

	// gceInterface is the testable equivalent for gce.GCE for secret manager access.
	gceInterface interface {
		GetSecret(ctx context.Context, projectID, secretName string) (string, error)
	}

	// CMDDBConnection stores connection information for querying via hdbsql command line.
	CMDDBConnection struct {
		SIDAdmUser     string // system user to run the queries from
		HDBUserKey     string // HDB Userstore Key providing auth and instance details
		EnableSSL      bool
		HostNameInCert string
		RootCAFile     string
	}

	// DBHandle provides an object to connect to and query databases, abstracting the underlying connector.
	DBHandle struct {
		useCMD      bool
		goHDBHandle *sql.DB
		cmdDBHandle *CMDDBConnection
	}

	// QueryResults is a struct to process the results of a database query.
	QueryResults struct {
		useCMD           bool
		goHDBResult      *sql.Rows
		cmdDBResult      []string // Stores the entire query result split by rows.
		cmdDBResultIndex int      // Stores current index of the command-line queried result.
	}
)

// CreateDBHandle creates a DB handle to the database that queries using either the go-hdb driver or hdbsql command-line.
func CreateDBHandle(ctx context.Context, p Params) (db *DBHandle, err error) {
	// we want to use go-hdb for username:password authorizations and hdbsql command-line for hdbuserstore key authorization.
	if p.HDBUserKey != "" {
		log.CtxLogger(ctx).Debug("Using hdbsql command-line")
		db, err = NewCMDDBHandle(p)
		if err != nil {
			return nil, err
		}
	} else {
		log.CtxLogger(ctx).Debug("Using go-hdb driver")
		db, err = NewGoDBHandle(ctx, p)
		if err != nil {
			return nil, err
		}
	}
	if p.PingSpec == nil {
		log.CtxLogger(ctx).Debug("Database connection successful")
		return db, nil
	}

	maxNumberOfPings := p.PingSpec.MaxRetries + 1
	for i := 0; i < maxNumberOfPings; i++ {
		pingFunc := db.Ping
		if p.PingSpec.pingImpl != nil {
			pingFunc = func(ctx context.Context) error {
				return p.PingSpec.pingImpl(ctx, db)
			}
		}
		if err = pingFunc(ctx); err == nil { // If NO error.
			log.CtxLogger(ctx).Debugw("Database connection established successfully", "attempts", i+1)
			return db, nil
		}
		time.Sleep(p.PingSpec.Timeout)
	}
	return nil, err
}

// Database Handle Functions for Querying and handling results

// NewGoDBHandle instantiates a go-hdb driver database handle.
func NewGoDBHandle(ctx context.Context, p Params) (handle *DBHandle, err error) {
	if p.Password == "" && p.PasswordSecret == "" {
		return nil, fmt.Errorf("could not attempt to connect to database %s, both password and secret name are empty", p.Host)
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
		return nil, err
	}
	return &DBHandle{
		useCMD:      false,
		goHDBHandle: db,
	}, nil
}

// NewCMDDBHandle instantiates a command-line database handle.
func NewCMDDBHandle(p Params) (handle *DBHandle, err error) {
	if p.SID == "" {
		return nil, fmt.Errorf("sid not provided")
	}
	if p.HDBUserKey == "" {
		return nil, fmt.Errorf("hdb userstore Key not provided")
	}

	cmdDBconnection := CMDDBConnection{
		SIDAdmUser:     fmt.Sprintf("%sadm", strings.ToLower(p.SID)),
		HDBUserKey:     p.HDBUserKey,
		EnableSSL:      p.EnableSSL,
		HostNameInCert: p.HostNameInCert,
		RootCAFile:     p.RootCAFile,
	}

	return &DBHandle{
		useCMD:      true,
		cmdDBHandle: &cmdDBconnection,
	}, nil
}

// Query queries the database via the goHDB driver or command-line accordingly.
func (db *DBHandle) Query(ctx context.Context, query string, exec commandlineexecutor.Execute) (*QueryResults, error) {
	if !db.useCMD {
		// Query via go HDB Driver.
		resultRows, err := db.goHDBHandle.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &QueryResults{
			useCMD:      false,
			goHDBResult: resultRows,
		}, nil
	}
	// Query via hdbsql command-line.
	sidadmArgs := []string{"-i", "-u", db.cmdDBHandle.SIDAdmUser}                                                 // Arguments to run command in sidadm user
	hdbsqlArgs := []string{"hdbsql", "-U", db.cmdDBHandle.HDBUserKey, "-a", "-x", "-quiet", "-Z", "CHOPBLANKS=0"} // Arguments to run hdbsql query in parse-able format

	if db.cmdDBHandle.EnableSSL {
		hdbsqlArgs = append(hdbsqlArgs, "-e", "-sslhostnameincert", db.cmdDBHandle.HostNameInCert, "-ssltruststore", db.cmdDBHandle.RootCAFile)
	}
	// Builds a command equivalent to [$sudo -i -u <sidadm> hdbsql -U <key> -a -x -quiet <query>].
	args := append(sidadmArgs, hdbsqlArgs...)
	args = append(args, query)

	result := exec(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       args,
	})
	if result.Error != nil || result.ExitCode != 0 {
		usagemetrics.Error(usagemetrics.HDBUserstoreKeyFailure)
		return nil, fmt.Errorf("%s", result.StdErr)
	}

	result.StdOut = strings.TrimSuffix(result.StdOut, "\n")
	resultRows := []string{}
	if result.StdOut != "" {
		// Non empty result
		resultRows = strings.Split(result.StdOut, "\n")
	}

	return &QueryResults{
		useCMD:           true,
		cmdDBResult:      resultRows,
		cmdDBResultIndex: -1,
	}, nil
}

// Ping pings the database via the goHDB driver or command-line accordingly.
func (db *DBHandle) Ping(ctx context.Context) error {
	if !db.useCMD {
		// Ping via go HDB Driver.
		return db.goHDBHandle.PingContext(ctx)
	}
	// Ping via hdbsql command-line - run a dummy query.
	dummyQuery := "SELECT * FROM DUMMY"
	_, err := db.Query(ctx, dummyQuery, commandlineexecutor.ExecuteCommand)
	return err
}

// Next iterates to the next row of result. Returns false if no more rows remain or the next row could not be read.
func (qr *QueryResults) Next() bool {
	if !qr.useCMD {
		// Results fetched via go-hdb driver.
		return qr.goHDBResult.Next()
	}
	// Results fetched via command-line.
	if qr.cmdDBResultIndex == (len(qr.cmdDBResult) - 1) {
		// No more rows to read.
		return false
	}
	qr.cmdDBResultIndex++
	return true
}

// ReadRow parses the current row of results into destination.
func (qr *QueryResults) ReadRow(dest ...any) error {
	if !qr.useCMD {
		// Results fetched via go-hdb driver.
		return qr.goHDBResult.Scan(dest...)
	}
	// Results fetched via command-line.
	if qr.cmdDBResultIndex <= -1 {
		return fmt.Errorf("called ReadRow() before calling Next()")
	}
	if qr.cmdDBResultIndex >= len(qr.cmdDBResult) {
		return fmt.Errorf("No more results to read")
	}
	return parseIntoValues(qr.cmdDBResult[qr.cmdDBResultIndex], dest...)
}

// Tokenize rows of hdbsql command-line results.
// This regexp matches values separated by commas, while not counting commas inside quotes.
// For non-primitive data types (i.e. string, date, etc.) it matches values inside the quotes.
// Eg: an hdbsql result: `1,"Doe, John",70.5,True`
// will be matched as: 	["1", "Doe, John", "70.5", "True"]
// TODO: Add support to parse values containing double quotes.
var cmdResultPattern = regexp.MustCompile(`("([^"]*)"|(\w[^",]*)|\?)`)

func parseIntoValues(resultRow string, dest ...any) error {
	nullChar := "?" // Represents NULL characters in the query output.

	matches := cmdResultPattern.FindAllStringSubmatch(resultRow, -1)
	if len(matches) != len(dest) {
		return fmt.Errorf("result has %d columns, but %d destination arguments provided", len(matches), len(dest))
	}

	// Parse each matched token as per the provided destination pointer type.
	// For NULL values the value is set to the default/zero values.
	for i, match := range matches {
		if len(match) < 3 {
			usagemetrics.Error(usagemetrics.HDBUserstoreKeyFailure)
			return fmt.Errorf("could not parse result")
		}
		switch d := dest[i].(type) {
		case (*string):
			// Non primitive or alphanumeric data types are enclosed in quotes.
			// For NULL values, match[2] is an empty string "".
			if match[0] == nullChar || strings.HasPrefix(match[0], `"`) {
				*d = match[2] // Gets result enclosed in quotes (e.g. strings, date, etc) as string.
				continue
			}
			*d = match[0] // Gets result not enclosed in quotes (e.g. int, float, etc) as string.
		case (*int64):
			if match[0] == nullChar {
				*d = 0
				continue
			}
			val, err := strconv.ParseInt(match[0], 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse %s as int64: %v", match[0], err)
			}
			*d = val
		case (*float64):
			if match[0] == nullChar {
				*d = 0.0
				continue
			}
			val, err := strconv.ParseFloat(match[0], 64)
			if err != nil {
				return fmt.Errorf("failed to parse %s as float64: %v", match[0], err)
			}
			*d = val
		case (*bool):
			if match[0] == nullChar {
				*d = false
				continue
			}
			val, err := strconv.ParseBool(match[0])
			if err != nil {
				return fmt.Errorf("failed to parse %s as bool: %v", match[0], err)
			}
			*d = val
		case (*any):
			// Non primitive or alphanumeric data types are enclosed in quotes.
			// For NULL values, match[2] is an empty string "".
			if match[0] == nullChar || strings.HasPrefix(match[0], `"`) {
				*d = match[2] // Gets result enclosed in quotes (e.g. strings, date, etc) as string.
				continue
			}
			*d = match[0] // Gets result not enclosed in quotes (e.g. int, float, etc) as string.
		default:
			usagemetrics.Error(usagemetrics.HDBUserstoreKeyFailure)
			return fmt.Errorf("unsupported destination argument type: %T", d)
		}
	}
	return nil
}

// IsAuthError returns true if the error is related to authentication, and false otherwise.
func IsAuthError(err error) bool {
	if hdbError := asHdbError(err); hdbError != nil {
		return isHdbErrorAuthRelated(*hdbError)
	}
	return false
}

// asHdbError returns the HDB error from the given error if it is an HDB error, and nil otherwise.
func asHdbError(err error) *driver.DBError {
	var hdbError driver.DBError
	isHdbError := err != nil && errors.As(err, &hdbError)
	if !isHdbError {
		return nil
	}
	return &hdbError
}

// isHdbErrorAuthRelated returns true if the HDB error is related to authentication, and false
// otherwise.
func isHdbErrorAuthRelated(err driver.DBError) bool {
	code := err.Code()
	for _, authCode := range authErrorCodes {
		if authCode == code {
			return true
		}
	}
	return false
}
