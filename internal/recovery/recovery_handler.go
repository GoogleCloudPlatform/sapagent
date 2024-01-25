/*
Copyright 2024 Google LLC

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

// Package recovery contains types and methods to aid in recovering from panics in goroutines.
package recovery

import (
	"context"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/pkg/errors"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// RecoverableRoutine is a set of details about a goroutine, its arguments and how to handle its recovery.
type RecoverableRoutine struct {
	Routine             func(context.Context, any)
	RoutineArg          any
	ErrorCode           int
	ExpectedMinDuration time.Duration
	Backoff             backoff.BackOff
	lastRestart         time.Time
}

// StartRoutine starts a routine and retries if it panics, following the assigned backoff policy.
func (r *RecoverableRoutine) StartRoutine(ctx context.Context) {
	go backoff.Retry(func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				n := time.Now()
				log.CtxLogger(ctx).Debugf("Checking if routine failed too quickly: %v ? %v, %v", r.lastRestart, n, r.ExpectedMinDuration)
				if n.Sub(r.lastRestart) >= r.ExpectedMinDuration {
					// Routine ran long enough, reset the backoff.
					log.CtxLogger(ctx).Debug("Resetting backoff before restart")
					r.Backoff.Reset()
				}
				err = errors.Errorf("Panic in routine, attempting to recover: %v", r)
				usagemetrics.Error(r.ErrorCode)
			}
		}()
		log.CtxLogger(ctx).Infof("Starting routine: %v", r)
		r.lastRestart = time.Now()
		r.Routine(ctx, r.RoutineArg)
		return nil
	}, r.Backoff)
}
