/*
Copyright 2022 Google LLC

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

// Package agentmetrics collects metrics from the agent process itself and submits them to cloud monitoring.
package agentmetrics

import (
	"context"
	"fmt"
	"os"
	"time"

	monpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monrespb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	cfgpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
)

const (
	metricURL   = "workload.googleapis.com"
	agentCPU    = "/sap/agent/cpu/utilization"
	agentMemory = "/sap/agent/memory/utilization"
)

// Service encapsulates the logic required to collect information on the agent process and to submit the data to cloud monitoring.
type Service struct {
	config              *cfgpb.Configuration
	timeSeriesSubmitter timeSeriesSubmitter
	timeSeriesCreator   cloudmonitoring.TimeSeriesCreator
	usageReader         usageReader
}

// Parameters aggregates the potential configuration values and inputs for Service.
type Parameters struct {
	Config              *cfgpb.Configuration
	timeSeriesCreator   cloudmonitoring.TimeSeriesCreator
	timeSeriesSubmitter timeSeriesSubmitter
	usageReader         usageReader
}

// timeSeriesSubmitter is a strategy by which metrics can be submitted to a monitoring service.
type timeSeriesSubmitter func(ctx context.Context, request *monpb.CreateTimeSeriesRequest) error

// usageReader is a strategy through which agent process metrics can be read.
type usageReader func(ctx context.Context) (usage, error)

// usage represents a snapshot of the agent process resource usage.
type usage struct {
	// cpu utilization percentage
	cpu float64
	// memory utilization percentage
	memory float64
}

// NewService constructs and initializes a Service instance by using the provided parameters.
func NewService(ctx context.Context, params Parameters) (*Service, error) {
	if err := validateParameters(params); err != nil {
		return nil, fmt.Errorf("invalid parameters for Service creation: %v", err)
	}
	service := &Service{
		config:              params.Config,
		timeSeriesCreator:   params.timeSeriesCreator,
		timeSeriesSubmitter: params.timeSeriesSubmitter,
		usageReader:         params.usageReader,
	}

	if service.timeSeriesCreator == nil {
		creator, err := monitoring.NewMetricClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed during attempt to create default TimeSeriesCreator: %v", err)
		}
		service.timeSeriesCreator = creator
	}

	if service.timeSeriesSubmitter == nil {
		service.timeSeriesSubmitter = func(ctx context.Context, req *monpb.CreateTimeSeriesRequest) error {
			return cloudmonitoring.CreateTimeSeriesWithRetry(ctx, service.timeSeriesCreator, req)
		}
	}

	if service.usageReader == nil {
		usageReader, err := newDefaultUsageReader(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed during attempt to create usage reader: %v", err)
		}
		service.usageReader = func(ctx context.Context) (usage, error) {
			return usageReader.read(ctx)
		}
	}

	return service, nil
}

// validateParameters checks the parameters for an mimimum viable set of information, and returns an error if the parameters are insufficent.
func validateParameters(params Parameters) error {
	if params.Config == nil || params.Config.GetCollectionConfiguration() == nil {
		return fmt.Errorf("config with a CollectionConfiguration must be provided")
	}
	collectionConfig := params.Config.GetCollectionConfiguration()
	if collectionConfig.CollectAgentMetrics && collectionConfig.AgentMetricsFrequency <= 0 {
		return fmt.Errorf("if agent metrics are being collected, the frequency must be positive")
	}
	return nil
}

// Start performs any initial checks and then begins the collect-submit loop.
func (s *Service) Start(ctx context.Context) {
	if !s.config.GetCollectionConfiguration().GetCollectAgentMetrics() {
		log.Logger.Info("Agent process metrics not configured for collection")
		return
	}
	log.Logger.Info("Agent process metric collection beginning")
	go s.collectAndSubmitLoop(ctx)
}

// collectAndSubmitLoop collects agent process metrics and submits them periodically until it is instructed to stop.
func (s *Service) collectAndSubmitLoop(ctx context.Context) {
	tickerHiatus := time.Millisecond * time.Duration(s.config.GetCollectionConfiguration().AgentMetricsFrequency)
	loopTicker := time.NewTicker(tickerHiatus)
	defer loopTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Logger.Info("Stopping agent metrics service", log.String("reason", ctx.Err().Error()))
			return
		case <-loopTicker.C:
			log.Logger.Debug("Collecting and submitting agent metrics")
			err := s.collectAndSubmit(ctx)
			if err != nil {
				log.Logger.Warn("Failure during agent metrics collection and submition", log.Error(err))
			}
		}
	}
}

// collectAndSubmit performs a single usage collection and submits it to cloud monitoring.
func (s *Service) collectAndSubmit(ctx context.Context) error {
	usage, err := s.usageReader(ctx)
	if err != nil {
		return fmt.Errorf("failed collecting agent process metrics: %v", err)
	}
	timeSeries := s.createTimeSeries(usage)
	request := s.createTimeSeriesRequestFactory(timeSeries)
	err = s.timeSeriesSubmitter(ctx, request)
	if err != nil {
		return fmt.Errorf("failed submitting metrics to cloud monitoring: %v", err)
	}
	return nil
}

// createTimeSeriesRequestFactory creates a time series request for cloud monitoring from TimeSeries instances.
func (s *Service) createTimeSeriesRequestFactory(timeSeries []*monrespb.TimeSeries) *monpb.CreateTimeSeriesRequest {
	projectID := s.config.GetCloudProperties().GetProjectId()
	return &monpb.CreateTimeSeriesRequest{
		Name:       fmt.Sprintf("projects/%s", projectID),
		TimeSeries: timeSeries,
	}
}

// createTimeSeries constructs TimeSeries instances from usage data.
func (s *Service) createTimeSeries(u usage) []*monrespb.TimeSeries {
	timeSeries := make([]*monrespb.TimeSeries, 2)
	params := timeseries.Params{
		CloudProp:    s.config.CloudProperties,
		MetricType:   metricURL + agentCPU,
		Timestamp:    tspb.Now(),
		Float64Value: u.cpu,
		BareMetal:    s.config.BareMetal,
	}
	timeSeries[0] = timeseries.BuildFloat64(params)

	params = timeseries.Params{
		MetricType:   metricURL + agentMemory,
		Float64Value: u.memory,
	}
	timeSeries[1] = timeseries.BuildFloat64(params)
	return timeSeries
}

// defaultUsageReader is the usageReader used when nothing no alternative is given when constructing a Service instance.
type defaultUsageReader struct {
	pid  int
	proc *process.Process
}

// newDefaultUsageReader constructs a new defaultUsageReader.
func newDefaultUsageReader(ctx context.Context) (*defaultUsageReader, error) {
	u := &defaultUsageReader{}
	u.pid = os.Getpid()
	proc, err := process.NewProcessWithContext(ctx, int32(u.pid))
	if err != nil {
		return nil, fmt.Errorf("failed creating process abstraction for agent process using process id: %v", err)
	}
	u.proc = proc
	return u, nil
}

// cpu reads the agent process' current cpu usage as a percentage of the host.
func (u *defaultUsageReader) cpu(ctx context.Context) (float64, error) {
	percent, err := u.proc.CPUPercentWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed reading cpu usage: %v", err)
	}
	return percent, nil
}

// memory reads the agent process' current memory as a percentage of the host.
func (u *defaultUsageReader) memory(ctx context.Context) (float64, error) {
	percent, err := u.proc.MemoryPercentWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed reading memory usage: %v", err)
	}
	return float64(percent), nil
}

// read determines the current usage of the agent process.
func (u *defaultUsageReader) read(ctx context.Context) (usage, error) {
	cpu, err := u.cpu(ctx)
	if err != nil {
		return usage{}, err
	}
	mem, err := u.memory(ctx)
	if err != nil {
		return usage{}, err
	}
	log.Logger.Debug("Collected agent metrics", log.Float64("cpu", cpu), log.Float64("memory", mem))
	return usage{cpu: cpu, memory: mem}, nil
}
