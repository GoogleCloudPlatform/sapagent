#!/bin/bash
# Copyright 2024 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#
# Build script that will get the module dependencies and build Linux and
# Windows binaries. The google_cloud_sap_agent binary will be built into
# the buildoutput/ dir.
#

# Use the following command to build the sapagent if there are outstanding protocol
# buffer changes in the protos directory:
#
# ./build.sh -c
#

set -exu

POSITIONAL_ARGS=()
COMPILE_PROTOS="FALSE"

while [[ $# -gt 0 ]]; do
  case $1 in
    -c|--compileprotos)
      COMPILE_PROTOS="TRUE"
      shift # past argument
      ;;
    -*|--*)
      echo "Unknown option $1"
      exit 1
      ;;
    *)
      POSITIONAL_ARGS+=("$1") # save positional arg
      shift # past argument
      ;;
  esac
done

echo "Starting the build process for the SAP Agent..."

if [ "${COMPILE_PROTOS}" == "TRUE" ] && [ ! -d "workloadagentplatform" ]; then
  echo "**************  Adding the workloadagent submodule"
    git submodule add https://github.com/GoogleCloudPlatform/workloadagentplatform
    cd workloadagentplatform
    # this is the hash of the workloadagentplatform submodule
    # get the hash by running: go list -m -json github.com/GoogleCloudPlatform/workloadagentplatform@main
    git checkout d9a9b45e4977c3237989311ee6b5e7103796f677
    cd ..
    # replace the proto imports in the platform that reference the platform
    find workloadagentplatform/sharedprotos -type f -exec sed -i 's|"sharedprotos|"workloadagentplatform/sharedprotos|g' {} +
fi

echo "**************  Getting go 1.24.2"
curl -sLOS https://go.dev/dl/go1.24.2.linux-amd64.tar.gz
chmod -fR u+rwx /tmp/sapagent || :
rm -fr /tmp/sapagent
mkdir -p /tmp/sapagent
tar -C /tmp/sapagent -xzf go1.24.2.linux-amd64.tar.gz

export GOROOT=/tmp/sapagent/go
export GOPATH=/tmp/sapagent/gopath
mkdir -p "${GOPATH}"
mkdir -p $GOROOT/.cache
mkdir -p $GOROOT/pkg/mod
export GOMODCACHE=$GOROOT/pkg/mod
export GOCACHE=$GOROOT/.cache
export GOBIN=$GOROOT/bin

PATH=${GOBIN}:${GOROOT}/packages/bin:$PATH

if [ "${COMPILE_PROTOS}" == "TRUE" ]; then
  echo "**************  Getting unzip 5.51"
  curl -sLOS https://oss.oracle.com/el4/unzip/unzip.tar
  tar -C /tmp/sapagent -xf unzip.tar

  echo "**************  Getting protoc 28.2"
  pb_rel="https://github.com/protocolbuffers/protobuf/releases"
  pb_dest="/tmp/sapagent/protobuf"
  curl -sLOS ${pb_rel}/download/v28.2/protoc-28.2-linux-x86_64.zip
  rm -fr "${pb_dest}"
  mkdir -p "${pb_dest}"
  /tmp/sapagent/unzip -q protoc-28.2-linux-x86_64.zip -d "${pb_dest}"

  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

  echo "**************  Compiling protobufs"
  protoc --go_opt=paths=source_relative protos/**/*.proto workloadagentplatform/sharedprotos/**/*.proto --go_out=.
fi

mkdir -p buildoutput
echo "**************  Generating the latest go.mod and go.sum dependencies"
cp go.mod go.mod.orig
cp go.sum go.sum.orig
go clean -modcache
go mod tidy
mv go.mod buildoutput/go.mod.latest
mv go.sum buildoutput/go.sum.latest
mv go.mod.orig go.mod
mv go.sum.orig go.sum

echo "**************  Getting the repo module dependencies using go mod vendor"
go clean -modcache
go mod vendor

echo "**************  Running all tests"
go test ./...

pushd cmd
echo "**************  Building Linux binary"
env GOOS=linux GOARCH=amd64 go build -mod=vendor -v -o ../buildoutput/google_cloud_sap_agent

echo "**************  Building Windows binary"
env GOOS=windows GOARCH=amd64 go build -mod=vendor -v -o ../buildoutput/google_cloud_sap_agent.exe
popd

echo "**************  Finished building the SAP Agent, the binaries and latest go.mod/go.sum are available in the buildoutput directory"
