module github.com/GoogleCloudPlatform/sapagent

go 1.19

replace github.com/GoogleCloudPlatform/sapagent/internal => ./internal

replace github.com/GoogleCloudPlatform/sapagent/protos => ./protos

require (
	cloud.google.com/go/monitoring v1.9.0
	cloud.google.com/go/secretmanager v1.9.0
	github.com/cenkalti/backoff/v4 v4.1.3
	github.com/gammazero/workerpool v1.1.3
	github.com/golang/protobuf v1.5.2
	github.com/google/go-cmp v0.5.9
	github.com/googleapis/gax-go/v2 v2.7.0
	github.com/jonboulle/clockwork v0.3.0
	github.com/natefinch/lumberjack v0.0.0-20230119042236-215739b3bcdc
	github.com/pkg/errors v0.9.1
	github.com/shirou/gopsutil/v3 v3.22.12
	github.com/zieckey/goini v0.0.0-20180118150432-0da17d361d26
	go.uber.org/zap v1.24.0
	golang.org/x/exp v0.0.0-20221114191408-850992195362
	golang.org/x/oauth2 v0.2.0
	google.golang.org/api v0.103.0
	google.golang.org/genproto v0.0.0-20221114212237-e4508ebdbee1
	google.golang.org/protobuf v1.28.1
)

require (
	cloud.google.com/go/compute v1.12.1 // indirect
	cloud.google.com/go/compute/metadata v0.2.1 // indirect
	cloud.google.com/go/iam v0.7.0 // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/gammazero/deque v0.2.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/tklauser/go-sysconf v0.3.11 // indirect
	github.com/tklauser/numcpus v0.6.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/text v0.7.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/grpc v1.50.1 // indirect
)
