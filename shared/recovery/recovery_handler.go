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
	"reflect"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/pkg/errors"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/usagemetrics"
)

// RecoverableRoutine is a set of details about a goroutine, its arguments and how to handle its recovery.
type RecoverableRoutine struct {
	Routine             func(context.Context, any)
	RoutineArg          any
	UsageLogger         usagemetrics.Logger
	ErrorCode           int
	ExpectedMinDuration time.Duration
	Backoff             backoff.BackOff
	lastRestart         time.Time
}

// StartRoutine starts a routine and retries if it panics, following the assigned backoff policy.
func (r *RecoverableRoutine) StartRoutine(ctx context.Context) {
	if r.Backoff == nil {
		r.Backoff = backoff.NewExponentialBackOff()
	}

	rName := runtime.FuncForPC(reflect.ValueOf(r.Routine).Pointer()).Name()
	go backoff.Retry(func() (err error) {
		routineCtx, cancel := context.WithCancel(ctx)
		defer func() {
			if p := recover(); p != nil {
				log.CtxLogger(routineCtx).Warnw("Panic in routine, attempting to recover", "panic", p)
				log.CtxLogger(routineCtx).Debugw("Stack trace", "stack", string(debug.Stack()))
				n := time.Now()
				log.CtxLogger(routineCtx).Debugf("Checking if routine failed too quickly: %v ? %v, %v", r.lastRestart, n, r.ExpectedMinDuration)
				if n.Sub(r.lastRestart) >= r.ExpectedMinDuration {
					// Routine ran long enough, reset the backoff.
					log.CtxLogger(routineCtx).Debug("Resetting backoff before restart")
					r.Backoff.Reset()
				}
				err = errors.Errorf("panic in routine, attempting to recover: %v", rName)
				r.UsageLogger.Error(r.ErrorCode)
				// Cancel the context to cancel any subroutines
				cancel()
			}
		}()
		log.CtxLogger(routineCtx).Debugw("Starting routine", "routine", rName)
		r.lastRestart = time.Now()
		r.Routine(routineCtx, r.RoutineArg)
		return nil
	}, r.Backoff)
}
