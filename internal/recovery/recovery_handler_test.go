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

package recovery

import (
	"context"
	"testing"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
)

type routineArgs struct {
	inCh  chan int
	outCh chan int
}

func testRoutine(ctx context.Context, a any) {
	var args routineArgs
	var ok bool
	if args, ok = a.(routineArgs); !ok {
		return
	}
	// Send a signal that the routine started.
	args.outCh <- 1
	select {
	case <-ctx.Done():
		return
	case x := <-args.inCh:
		switch x {
		case 0:
			return
		case 1:
			panic("test panic")
		}
		return
	}
}

func waitForSignal(ch chan int, signal int, t *testing.T) bool {
	i := 0
	for {
		select {
		case x := <-ch:
			if x != signal {
				t.Errorf("Received unexpected signal. got: %d, want %d", x, signal)
			}
			return true
		default:
			i++
			if i >= 100 {
				return false
			}
			time.Sleep(time.Millisecond)
		}
	}
}

type fakeBackoff struct {
	numBackoff int
	numReset   int
	t          *testing.T
}

func (f *fakeBackoff) NextBackOff() time.Duration {
	f.numBackoff++
	return 5 * time.Millisecond
}

func (f *fakeBackoff) Reset() {
	f.numReset++
}

// Tests various scenarios of routines restarting.
func TestRoutineRestarts(t *testing.T) {
	tests := []struct {
		name             string
		panicDelays      []time.Duration
		maxRetries       uint64
		expectedBackoffs int
		expectedResets   int
	}{{
		name:             "noPanic",
		panicDelays:      []time.Duration{},
		expectedBackoffs: 0,
		expectedResets:   1,
	}, {
		name:             "singleRapidPanic",
		panicDelays:      []time.Duration{time.Millisecond},
		maxRetries:       1,
		expectedBackoffs: 1,
		expectedResets:   1,
	}, {
		name:             "panicAfterNormalRuntime",
		panicDelays:      []time.Duration{2 * time.Second},
		expectedBackoffs: 1,
		expectedResets:   2,
	}, {
		name:             "shortLongShortPanic",
		panicDelays:      []time.Duration{time.Millisecond, 2 * time.Second, time.Millisecond},
		expectedBackoffs: 2,
		expectedResets:   2,
	}, {
		name:             "exceedMaxRetries",
		panicDelays:      []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
		maxRetries:       2,
		expectedBackoffs: 2,
		expectedResets:   1,
	}, {
		name:             "maxRetryAfterNormalRun",
		panicDelays:      []time.Duration{time.Millisecond, 2 * time.Second, time.Millisecond, time.Millisecond},
		maxRetries:       2,
		expectedBackoffs: 3,
		expectedResets:   2,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			ctx, cancel := context.WithCancel(context.Background())

			inCh := make(chan int)
			outCh := make(chan int)
			a := routineArgs{
				inCh:  inCh,
				outCh: outCh,
			}
			fbo := &fakeBackoff{
				numReset:   0,
				numBackoff: 0,
				t:          t,
			}
			// Registering routine.
			routine := &RecoverableRoutine{
				Routine:             testRoutine,
				RoutineArg:          a,
				ErrorCode:           1011,
				ExpectedMinDuration: time.Second,
				Backoff:             backoff.WithMaxRetries(fbo, max(test.maxRetries, 1)),
			}
			routine.StartRoutine(ctx)

			for _, delay := range test.panicDelays {
				// Waiting for start signal from routine.
				t.Logf("Waiting for start signal from routine.")
				ok := waitForSignal(outCh, 1, t)
				if !ok {
					t.Fatalf("routine not started")
				}

				// Wait to panic the routine.
				t.Logf("Waiting for panic.")
				time.Sleep(delay)
				inCh <- 1
			}
			t.Logf("Waiting for final routine start.")
			// Waiting for final routine start, some tests may or may not start one extra time.
			waitForSignal(outCh, 1, t)

			if test.expectedBackoffs != fbo.numBackoff {
				t.Errorf("Expected backoffs: %d, got: %d", test.expectedBackoffs, fbo.numBackoff)
			}
			if test.expectedResets != fbo.numReset {
				t.Errorf("Expected resets: %d, got: %d", test.expectedResets, fbo.numReset)
			}

			cancel()
		})
	}
}
