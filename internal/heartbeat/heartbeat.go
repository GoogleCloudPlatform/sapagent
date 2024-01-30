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

// Package heartbeat provides a mechanism for long running services to register with a health monitor and for observers to query the monitor for collective health.
package heartbeat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/recovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// Spec provides the means of performing heartbeats at some regular interval.
type Spec struct {
	Interval time.Duration
	BeatFunc func()
}

// registration is tracking information for a single registered entity.
type registration struct {
	lock             sync.RWMutex
	name             string
	missedHeartbeats int64
	spec             Spec
}

// runSpec provides internal-only configuration for the run method. This was added to allow for
// deterministic control over the run loop execution for testing purposes because after 9 months of
// relying on timings to test cancellation we had a test failure. We acknowledge this adds code only
// useful to tests, but now that there is evidence of test failures we must address it in a more
// controllable fashion and this seems like the least bad solution.
type runSpec struct {
	maxTicks int
	// done will be closed by run to signal to any interested parties that execution is complete.
	done chan struct{}
}

// Monitor is the entity which will accept requests to monitor long running services.
type Monitor struct {
	registrations    map[string]*registration
	frequency        time.Duration
	threshold        int64
	registrationLock sync.RWMutex
	runSpec          *runSpec
	runRoutine       *recovery.RecoverableRoutine
}

// Parameters aggregates the data required to create and run a Monitor.
type Parameters struct {
	Config *cfgpb.Configuration
}

// validateParameters is capable of producing errors describing why provided parameters are invalid for Monitor creation.
func validateParameters(params Parameters) error {
	if params.Config == nil || params.Config.GetCollectionConfiguration() == nil {
		return fmt.Errorf("config with a CollectionConfiguration must be provided")
	}
	collection := params.Config.GetCollectionConfiguration()
	if collection.GetMissedHeartbeatThreshold() < 0 {
		return fmt.Errorf("missed heartbeat threshold must be non-negative")
	}
	if collection.GetHeartbeatFrequency() <= 0 {
		return fmt.Errorf("heartbeat frequency must be positive")
	}
	return nil
}

// NewMonitor constructs a new Monitor instance using provided parameters.
func NewMonitor(params Parameters) (*Monitor, error) {
	if err := validateParameters(params); err != nil {
		return nil, err
	}
	configurationCollection := params.Config.GetCollectionConfiguration()
	frequencySeconds := configurationCollection.GetHeartbeatFrequency()
	threshold := configurationCollection.GetMissedHeartbeatThreshold()
	monitor := Monitor{
		frequency:        time.Duration(frequencySeconds) * time.Second,
		registrationLock: sync.RWMutex{},
		registrations:    map[string]*registration{},
		runSpec:          nil,
		threshold:        threshold,
	}
	return &monitor, nil
}

// Register will provide heartbeat instructions for services registering for health monitoring.
func (m *Monitor) Register(name string) (*Spec, error) {
	m.registrationLock.Lock()
	defer m.registrationLock.Unlock()
	if _, ok := m.registrations[name]; ok {
		return nil, fmt.Errorf("registration already exists for provided name")
	}
	reg := &registration{
		lock:             sync.RWMutex{},
		missedHeartbeats: 0,
		name:             name,
		spec:             Spec{},
	}
	beat := func() {
		reg.lock.Lock()
		defer reg.lock.Unlock()
		reg.missedHeartbeats = 0
	}
	reg.spec = Spec{BeatFunc: beat, Interval: m.frequency}
	m.registrations[name] = reg
	return &reg.spec, nil
}

// Run will begin the asynchronous monitoring of registered services.
func (m *Monitor) Run(ctx context.Context) {
	m.runRoutine = &recovery.RecoverableRoutine{
		Routine:             m.run,
		ErrorCode:           usagemetrics.HeartbeatRoutineFailure,
		ExpectedMinDuration: m.frequency,
	}
	m.runRoutine.StartRoutine(ctx)
}

// run will periodically increment the missed heartbeat count for registrants until instructed to stop.
func (m *Monitor) run(ctx context.Context, _ any) {
	ticker := time.NewTicker(m.frequency)
	defer ticker.Stop()
	// This is needed so that we can, in effect, set ticker.C = nil.
	ch := ticker.C
	hasSpec := m.runSpec != nil
	numTicks := 0
	for {
		select {
		case <-ctx.Done():
			if hasSpec && ch != nil {
				// Cancellation has been initiated before max ticks was reached.
				close(m.runSpec.done)
			}
			return
		case <-ch:
			if hasSpec {
				if numTicks >= m.runSpec.maxTicks {
					ch = nil
					close(m.runSpec.done)
					// We continue instead of return here because we want to allow code coverage to hit the
					// ctx.Done case.
					continue
				}
				numTicks++
			}
			m.incrementAll()
		}
	}
}

// incrementAll will atomically increment the missed heartbeat count for all registrants.
func (m *Monitor) incrementAll() {
	m.registrationLock.Lock()
	defer m.registrationLock.Unlock()
	for _, r := range m.registrations {
		r.lock.Lock()
		if r.missedHeartbeats < m.threshold {
			log.Logger.Debug("missed heartbeats incremented", log.String("service", r.name), log.Int64("count", r.missedHeartbeats))
			r.missedHeartbeats++
		}
		r.lock.Unlock()
	}
}

// GetStatuses will return a mapping of service name to health status.
func (m *Monitor) GetStatuses() map[string]bool {
	m.registrationLock.RLock()
	defer m.registrationLock.RUnlock()
	statuses := map[string]bool{}
	for name, reg := range m.registrations {
		reg.lock.RLock()
		statuses[name] = reg.missedHeartbeats < m.threshold
		reg.lock.RUnlock()
	}
	return statuses
}

// Beat will indicate to the monitor that the service associated with this Spec is healthy.
func (h *Spec) Beat() {
	if h == nil {
		return
	}
	h.BeatFunc()
}

// CreateTicker will create a new Ticker that ticks at the frequency described by the Spec.
func (h *Spec) CreateTicker() *time.Ticker {
	if h == nil {
		return time.NewTicker(time.Hour * 24)
	}
	return time.NewTicker(h.Interval)
}

// NullMonitor is a monitor that will perform sensible no-ops for all requests.
type NullMonitor struct {
}

// Register will perform no action and return no errors.
func (n *NullMonitor) Register(name string) (*Spec, error) {
	return nil, nil
}

// GetStatuses will return an empty map. Calls to Register will have no impact.
func (n *NullMonitor) GetStatuses() map[string]bool {
	return map[string]bool{}
}
