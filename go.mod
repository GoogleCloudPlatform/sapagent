module github.com/GoogleCloudPlatform/sapagent

go 1.24

replace github.com/GoogleCloudPlatform/sapagent/internal => ./internal

replace github.com/GoogleCloudPlatform/sapagent/shared => ./shared

replace github.com/GoogleCloudPlatform/sapagent/protos => ./protos

require (
	cloud.google.com/go/aiplatform v1.70.0
	cloud.google.com/go/artifactregistry v1.16.1
	cloud.google.com/go/iam v1.3.1
	cloud.google.com/go/logging v1.13.0
	cloud.google.com/go/monitoring v1.23.0
	cloud.google.com/go/secretmanager v1.14.4
	cloud.google.com/go/storage v1.50.0
	// Get the version by running:
	// go list -m -json github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries@main
	github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries v0.0.0-20250506155553-97cf733eda7e
	// Get the version by running:
	// go list -m -json github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos@main
	github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos v0.0.0-20250506155553-97cf733eda7e
	github.com/SAP/go-hdb v1.12.12
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/fsouza/fake-gcs-server v1.52.1
	github.com/gammazero/workerpool v1.1.3
	github.com/go-yaml/yaml v2.1.0+incompatible
	github.com/google/go-cmp v0.6.0
	github.com/google/safetext v0.0.0-20240722112252-5a72de7e7962
	github.com/google/subcommands v1.2.0
	github.com/googleapis/gax-go/v2 v2.14.1
	github.com/jonboulle/clockwork v0.5.0
	github.com/kardianos/service v1.2.2
	github.com/pkg/errors v0.9.1
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/zieckey/goini v0.0.0-20240615065340-08ee21c836fb // indirect
	go.uber.org/zap v1.27.0
	golang.org/x/exp v0.0.0-20250128182459-e0ece0dbea4c
	golang.org/x/oauth2 v0.26.0
	golang.org/x/sys v0.31.0
	google.golang.org/api v0.220.0
	google.golang.org/genproto v0.0.0-20250204164813-702378808489
	google.golang.org/genproto/googleapis/api v0.0.0-20250204164813-702378808489
	google.golang.org/protobuf v1.36.5
)

require (
	cel.dev/expr v0.19.1 // indirect
	cloud.google.com/go v0.118.0 // indirect
	cloud.google.com/go/auth v0.14.1 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.7 // indirect
	cloud.google.com/go/compute/metadata v0.6.0 // indirect
	cloud.google.com/go/longrunning v0.6.4 // indirect
	cloud.google.com/go/pubsub v1.45.3 // indirect
	github.com/GoogleCloudPlatform/agentcommunication_client v0.0.0-20250227185639-b70667e4a927 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.25.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.49.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.49.0 // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cncf/xds/go v0.0.0-20241223141626-cff3c89139a3 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.32.3 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.1.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/gammazero/deque v0.2.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/google/renameio/v2 v2.0.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/handlers v1.5.2 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/natefinch/lumberjack v2.0.0+incompatible // indirect
	github.com/pkg/xattr v0.4.10 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.33.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.58.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.58.0 // indirect
	go.opentelemetry.io/otel v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.34.0 // indirect
	go.opentelemetry.io/otel/sdk v1.34.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.33.0 // indirect
	go.opentelemetry.io/otel/trace v1.34.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/crypto v0.36.0 // indirect
	golang.org/x/mod v0.22.0 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sync v0.12.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	golang.org/x/time v0.9.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250127172529-29210b9bc287 // indirect
	google.golang.org/grpc v1.70.0 // indirect
	mvdan.cc/sh/v3 v3.7.0 // indirect
)
