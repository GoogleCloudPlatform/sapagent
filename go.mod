module github.com/GoogleCloudPlatform/sapagent

go 1.25.8

replace github.com/GoogleCloudPlatform/sapagent/internal => ./internal

replace github.com/GoogleCloudPlatform/sapagent/shared => ./shared

replace github.com/GoogleCloudPlatform/sapagent/protos => ./protos

require (
  cloud.google.com/go/artifactregistry v1.25.0
  cloud.google.com/go/iam v1.11.0
  cloud.google.com/go/logging v1.18.0
  cloud.google.com/go/monitoring v1.29.0
  cloud.google.com/go/secretmanager v1.16.0 // indirect
  cloud.google.com/go/storage v1.62.2
  // Get the version by running:
  // go list -m -json github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos@main
  github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos v0.0.0-20260302132144-7a203cf65cdd
  github.com/Masterminds/semver/v3 v3.5.0
  github.com/SAP/go-hdb v1.16.11
  github.com/cenkalti/backoff/v4 v4.3.0
  github.com/fsouza/fake-gcs-server v1.54.0
  github.com/gammazero/workerpool v1.2.1
  gopkg.in/yaml.v2 v2.4.0
  github.com/google/go-cmp v0.7.0
  github.com/google/safetext v0.0.0-20260330151545-1fb717a317c5
  github.com/google/subcommands v1.2.0
  github.com/googleapis/gax-go/v2 v2.22.0
  github.com/jonboulle/clockwork v0.5.0
  github.com/kardianos/service v1.2.4
  github.com/pkg/errors v0.9.1
  github.com/shirou/gopsutil/v3 v3.24.5
  github.com/zieckey/goini v0.0.0-20240615065340-08ee21c836fb // indirect
  go.uber.org/zap v1.28.0
  golang.org/x/exp v0.0.0-20260529124908-c761662dc8c9
  golang.org/x/oauth2 v0.36.0
  golang.org/x/sys v0.45.0
  google.golang.org/api v0.282.0
  google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7
  google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9
  google.golang.org/protobuf v1.36.11
)

require (
  cloud.google.com/go/kms v1.31.0
  cloud.google.com/go/pubsub v1.50.2
  github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries v0.0.0-20260304143153-4729a3b634b7
  go.uber.org/multierr v1.11.0
)

require (
  cel.dev/expr v0.25.1 // indirect
  cloud.google.com/go v0.123.0 // indirect
  cloud.google.com/go/auth v0.20.0 // indirect
  cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
  cloud.google.com/go/compute/metadata v0.9.0 // indirect
  cloud.google.com/go/longrunning v0.9.0 // indirect
  cloud.google.com/go/pubsub/v2 v2.4.0 // indirect
  github.com/GoogleCloudPlatform/agentcommunication_client v0.0.0-20250227185639-b70667e4a927 // indirect
  github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.31.0 // indirect
  github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.55.0 // indirect
  github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.55.0 // indirect
  github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
  github.com/cespare/xxhash/v2 v2.3.0 // indirect
  github.com/cncf/xds/go v0.0.0-20260202195803-dba9d589def2 // indirect
  github.com/envoyproxy/go-control-plane/envoy v1.37.0 // indirect
  github.com/envoyproxy/protoc-gen-validate v1.3.3 // indirect
  github.com/fatih/color v1.18.0 // indirect
  github.com/felixge/httpsnoop v1.0.4 // indirect
  github.com/gammazero/deque v1.2.1 // indirect
  github.com/go-jose/go-jose/v4 v4.1.4 // indirect
  github.com/go-logr/logr v1.4.3 // indirect
  github.com/go-logr/stdr v1.2.2 // indirect
  github.com/go-ole/go-ole v1.2.6 // indirect
  github.com/google/renameio/v2 v2.0.0 // indirect
  github.com/google/s2a-go v0.1.9 // indirect
  github.com/google/uuid v1.6.0 // indirect
  github.com/googleapis/enterprise-certificate-proxy v0.3.16 // indirect
  github.com/gorilla/handlers v1.5.2 // indirect
  github.com/gorilla/mux v1.8.1 // indirect
  github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
  github.com/mattn/go-colorable v0.1.13 // indirect
  github.com/mattn/go-isatty v0.0.20 // indirect
  github.com/natefinch/lumberjack v2.0.0+incompatible // indirect
  github.com/pkg/xattr v0.4.12 // indirect
  github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
  github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
  github.com/shoenig/go-m1cpu v0.1.6 // indirect
  github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
  github.com/tklauser/go-sysconf v0.3.12 // indirect
  github.com/tklauser/numcpus v0.6.1 // indirect
  github.com/yusufpapurcu/wmi v1.2.4 // indirect
  go.opencensus.io v0.24.0 // indirect
  go.opentelemetry.io/auto/sdk v1.2.1 // indirect
  go.opentelemetry.io/contrib/detectors/gcp v1.42.0 // indirect
  go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
  go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
  go.opentelemetry.io/otel v1.43.0 // indirect
  go.opentelemetry.io/otel/metric v1.43.0 // indirect
  go.opentelemetry.io/otel/sdk v1.43.0 // indirect
  go.opentelemetry.io/otel/sdk/metric v1.43.0 // indirect
  go.opentelemetry.io/otel/trace v1.43.0 // indirect
  golang.org/x/crypto v0.52.0 // indirect
  golang.org/x/mod v0.36.0 // indirect
  golang.org/x/net v0.55.0 // indirect
  golang.org/x/sync v0.20.0 // indirect
  golang.org/x/text v0.37.0 // indirect
  golang.org/x/time v0.15.0 // indirect
  google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
  google.golang.org/grpc v1.81.1 // indirect
  mvdan.cc/sh/v3 v3.7.0 // indirect
)
