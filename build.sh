#!/bin/bash

#
# Build script that will get the module dependencies and build a linux binary.
# The google_cloud_sap_agent binary will be built into the buildoutput/ dir.
#

set -exu

echo "Starting the build process for the SAP Agent..."

echo "**************  Getting go 1.21"
wget -q https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
mkdir -p /tmp/sapagent
tar -C /tmp/sapagent -xzf go1.21.0.linux-amd64.tar.gz

export GOROOT=/tmp/sapagent/go
mkdir -p $GOROOT/.cache
mkdir -p $GOROOT/pkg/mod
export GOMODCACHE=$GOROOT/pkg/mod
export GOCACHE=$GOROOT/.cache
PATH=/tmp/sapagent/go/bin:$PATH
go clean -modcache

echo "**************  Getting module dependencies"
go mod vendor

echo "**************  Running all tests"
go test ./...

echo "**************  Building Linux binary"
mkdir -p buildoutput
env GOOS=linux GOARCH=amd64 go build -mod=readonly -v -o buildoutput/google_cloud_sap_agent cmd/local/main.go

echo "**************  Cleaning up"
rm -f go1.21.0.linux-amd64.tar.gz*
go clean -modcache
rm -fr /tmp/sapagent

echo "**************  Finished building the SAP Agent, the google_cloud_sap_agent binary is available in the buildoutput directory"
