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
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	metricURL   = "workload.googleapis.com"
	agentCPU    = "/sap/agent/cpu/utilization"
	agentMemory = "/sap/agent/memory/utilization"
	agentHealth = "/sap/agent/health"
)

// HealthMonitor is anything that can register and monitor entities capable of producing heart beats.
type HealthMonitor interface {
	Register(name string) (*heartbeat.Spec, error)
	GetStatuses() map[string]bool
}

// Service encapsulates the logic required to collect information on the agent process and to submit the data to cloud monitoring.
type Service struct {
	config              *cfgpb.Configuration
	healthMonitor       HealthMonitor
	timeSeriesSubmitter timeSeriesSubmitter
	timeSeriesCreator   cloudmonitoring.TimeSeriesCreator
	usageReader         usageReader
	now                 now
}

// Parameters aggregates the potential configuration values and inputs for Service.
type Parameters struct {
	BackOffs            *cloudmonitoring.BackOffIntervals
	Config              *cfgpb.Configuration
	HealthMonitor       HealthMonitor
	now                 now
	timeSeriesCreator   cloudmonitoring.TimeSeriesCreator
	timeSeriesSubmitter timeSeriesSubmitter
	usageReader         usageReader
}

// timeSeriesSubmitter is a strategy by which metrics can be submitted to a monitoring service.
type timeSeriesSubmitter func(ctx context.Context, request *monpb.CreateTimeSeriesRequest) error

// usageReader is a strategy through which agent process metrics can be read.
type usageReader func(ctx context.Context) (usage, error)

// now is a strategy for getting the current timestamp.
type now func() *tspb.Timestamp

// usage represents a snapshot of the agent process resource usage.
type usage struct {
	// cpu utilization percentage
	cpu float64
	// virtual memory consumption in bytes
	memory uint64
}

// NewService constructs and initializes a Service instance by using the provided parameters.
func NewService(ctx context.Context, params Parameters) (*Service, error) {
	if err := validateParameters(params); err != nil {
		return nil, fmt.Errorf("Invalid parameters for Service creation: %v", err)
	}
	service := &Service{
		config:              params.Config,
		healthMonitor:       params.HealthMonitor,
		now:                 params.now,
		timeSeriesCreator:   params.timeSeriesCreator,
		timeSeriesSubmitter: params.timeSeriesSubmitter,
		usageReader:         params.usageReader,
	}

	if service.timeSeriesCreator == nil {
		creator, err := monitoring.NewMetricClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("Failed during attempt to create default TimeSeriesCreator: %v", err)
		}
		service.timeSeriesCreator = creator
	}

	if service.timeSeriesSubmitter == nil {
		service.timeSeriesSubmitter = func(ctx context.Context, req *monpb.CreateTimeSeriesRequest) error {
			return cloudmonitoring.CreateTimeSeriesWithRetry(ctx, service.timeSeriesCreator, req, params.BackOffs)
		}
	}

	if service.usageReader == nil {
		usageReader, err := newDefaultUsageReader(ctx)
		if err != nil {
			return nil, fmt.Errorf("Failed during attempt to create usage reader: %v", err)
		}
		service.usageReader = func(ctx context.Context) (usage, error) {
			return usageReader.read(ctx)
		}
	}

	if service.now == nil {
		service.now = tspb.Now
	}
	return service, nil
}

// validateParameters checks the parameters for a minimum viable set of information, and returns an error if the parameters are insufficient.
func validateParameters(params Parameters) error {
	if params.Config == nil || params.Config.GetCollectionConfiguration() == nil {
		return fmt.Errorf("Config with a CollectionConfiguration must be provided")
	}
	collectionConfig := params.Config.GetCollectionConfiguration()
	if collectionConfig.CollectAgentMetrics && (collectionConfig.AgentMetricsFrequency < 5 || collectionConfig.AgentHealthFrequency < 5) {
		return fmt.Errorf("If agent metrics are being collected, the metric frequency and health frequency must be at least 5")
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
	// metricTicker will signal when metrics like cpu and memory are collected and submitted.
	metricInterval := time.Second * time.Duration(s.config.GetCollectionConfiguration().AgentMetricsFrequency)
	metricTicker := time.NewTicker(metricInterval)
	defer metricTicker.Stop()

	// healthTicker will signal when in-process service health is collected and submitted.
	healthInterval := time.Second * time.Duration(s.config.GetCollectionConfiguration().AgentHealthFrequency)
	healthTicker := time.NewTicker(healthInterval)
	defer healthTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Logger.Infow("Stopping agent metrics service", "reason", ctx.Err())
			return
		case <-healthTicker.C:
			log.Logger.Debug("Collecting and submitting agent health")
			if err := s.collectAndSubmitHealth(ctx); err != nil {
				log.Logger.Warnw("Failure during agent health collection and submission", "error", err)
			}
		case <-metricTicker.C:
			log.Logger.Debug("Collecting and submitting agent metrics")
			if err := s.collectAndSubmitMetrics(ctx); err != nil {
				log.Logger.Warnw("Failure during agent metrics collection and submission", "error", err)
			}
		}
	}
}

// collectHealthStatus will determine if the agent is healthy or unhealthy.
func (s *Service) collectHealthStatus(ctx context.Context) bool {
	statuses := s.healthMonitor.GetStatuses()
	healthy := true
	for registrant, health := range statuses {
		if !health {
			log.Logger.Warnw("Registered service is unhealthy", "name", registrant)
			healthy = false
		}
	}
	return healthy
}

// collectAndSubmitHealth will orchestrate the collection of agent health metrics and submit them to cloud monitoring.
func (s *Service) collectAndSubmitHealth(ctx context.Context) error {
	healthy := s.collectHealthStatus(ctx)
	timeSeries := s.createHealthTimeSeries(healthy)
	request := s.createTimeSeriesRequestFactory(timeSeries)
	if err := s.timeSeriesSubmitter(ctx, request); err != nil {
		return fmt.Errorf("Failed submitting agent health to cloud monitoring: %v", err)
	}
	return nil
}

// collectAndSubmitMetrics performs a single usage collection and submits it to cloud monitoring.
func (s *Service) collectAndSubmitMetrics(ctx context.Context) error {
	usage, err := s.usageReader(ctx)
	if err != nil {
		return fmt.Errorf("Failed collecting agent process metrics: %v", err)
	}
	timeSeries := s.createMetricTimeSeries(usage)
	request := s.createTimeSeriesRequestFactory(timeSeries)
	if err := s.timeSeriesSubmitter(ctx, request); err != nil {
		return fmt.Errorf("Failed submitting agent metrics to cloud monitoring: %v", err)
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

// createHealthTimeSeries constructs TimeSeries instances from usage data.
func (s *Service) createHealthTimeSeries(healthy bool) []*monrespb.TimeSeries {
	var timeSeries []*monrespb.TimeSeries
	params := timeseries.Params{
		BareMetal:  s.config.BareMetal,
		BoolValue:  healthy,
		CloudProp:  s.config.CloudProperties,
		MetricType: metricURL + agentHealth,
		Timestamp:  s.now(),
	}
	return append(timeSeries, timeseries.BuildBool(params))
}

// createMetricTimeSeries constructs TimeSeries instances from usage data.
func (s *Service) createMetricTimeSeries(u usage) []*monrespb.TimeSeries {
	timeSeries := make([]*monrespb.TimeSeries, 2)
	now := s.now()
	params := timeseries.Params{
		BareMetal:    s.config.BareMetal,
		CloudProp:    s.config.CloudProperties,
		Float64Value: u.cpu,
		MetricType:   metricURL + agentCPU,
		Timestamp:    now,
	}
	timeSeries[0] = timeseries.BuildFloat64(params)

	params = timeseries.Params{
		BareMetal:    s.config.BareMetal,
		CloudProp:    s.config.CloudProperties,
		Float64Value: float64(u.memory),
		MetricType:   metricURL + agentMemory,
		Timestamp:    now,
	}
	timeSeries[1] = timeseries.BuildFloat64(params)
	return timeSeries
}

// defaultUsageReader is the usageReader used when no alternative is given when constructing a Service instance.
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
		return nil, fmt.Errorf("Failed creating process abstraction for agent process using process id: %v", err)
	}
	u.proc = proc
	return u, nil
}

// cpu reads the agent process' current cpu usage as a percentage of the host.
func (u *defaultUsageReader) cpu(ctx context.Context) (float64, error) {
	percent, err := u.proc.CPUPercentWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("Failed reading cpu usage: %v", err)
	}
	return percent, nil
}

// memory reads the agent process' current virtual memory usage in bytes.
func (u *defaultUsageReader) memory(ctx context.Context) (uint64, error) {
	memInfo, err := u.proc.MemoryInfoWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("Failed reading memory usage: %v", err)
	}
	return memInfo.VMS, nil
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
	log.Logger.Debugw("Collected agent metrics", "cpu", cpu, "memory", mem)
	return usage{cpu: cpu, memory: mem}, nil
}
