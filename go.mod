module github.com/GoogleCloudPlatform/sapagent

go 1.21

replace github.com/GoogleCloudPlatform/sapagent/internal => ./internal

replace github.com/GoogleCloudPlatform/sapagent/shared => ./shared

replace github.com/GoogleCloudPlatform/sapagent/protos => ./protos

require (
	cloud.google.com/go/logging v1.7.0
	cloud.google.com/go/monitoring v1.15.1
	cloud.google.com/go/secretmanager v1.11.1
	cloud.google.com/go/storage v1.30.1
	github.com/SAP/go-hdb v1.1.6
	github.com/cenkalti/backoff/v4 v4.1.3
	github.com/fsouza/fake-gcs-server v1.45.2
	github.com/gammazero/workerpool v1.1.3
	github.com/go-yaml/yaml v2.1.0+incompatible
	github.com/golang/protobuf v1.5.3
	github.com/google/go-cmp v0.5.9
	github.com/google/subcommands v1.2.0
	github.com/googleapis/gax-go/v2 v2.12.0
	github.com/jonboulle/clockwork v0.3.0
	github.com/natefinch/lumberjack v0.0.0-20230119042236-215739b3bcdc
	github.com/pkg/errors v0.9.1
	github.com/shirou/gopsutil/v3 v3.22.12
	github.com/zieckey/goini v0.0.0-20180118150432-0da17d361d26
	go.uber.org/zap v1.24.0
	golang.org/x/exp v0.0.0-20230321023759-10a507213a29
	golang.org/x/oauth2 v0.12.0
	google.golang.org/api v0.144.0
	google.golang.org/genproto v0.0.0-20230913181813-007df8e322eb
	google.golang.org/genproto/googleapis/api v0.0.0-20230913181813-007df8e322eb
	google.golang.org/protobuf v1.31.0
)

require (
	cloud.google.com/go v0.110.7 // indirect
	cloud.google.com/go/compute v1.23.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	cloud.google.com/go/iam v1.1.1 // indirect
	cloud.google.com/go/longrunning v0.5.1 // indirect
	cloud.google.com/go/pubsub v1.33.0 // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/felixge/httpsnoop v1.0.2 // indirect
	github.com/gammazero/deque v0.2.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/renameio/v2 v2.0.0 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/google/uuid v1.3.1 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.1 // indirect
	github.com/gorilla/handlers v1.5.1 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/pkg/xattr v0.4.9 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/sirupsen/logrus v1.9.2 // indirect
	github.com/tklauser/go-sysconf v0.3.11 // indirect
	github.com/tklauser/numcpus v0.6.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/sync v0.3.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230920204549-e6e6cdab5c13 // indirect
	google.golang.org/grpc v1.58.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
