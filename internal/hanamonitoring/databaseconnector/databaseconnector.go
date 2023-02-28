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
	"database/sql"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	// Register hdb driver.
	_ "github.com/SAP/go-hdb/driver"
)

// Connect creates a SQL connection to a HANA database utilizing the go-hdb driver.
func Connect(username, password, host, port string) (*sql.DB, error) {
	dataSource := "hdb://" + username + ":" + password + "@" + host + ":" + port
	db, err := sql.Open("hdb", dataSource)
	if err != nil {
		log.Logger.Errorw("Could not open connection to database.", "username", username, "host", host, "port", port, "err", err)
		return nil, err
	}
	return db, nil
}
