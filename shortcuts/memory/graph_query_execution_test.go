package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"code.byted.org/lark_search/larksuite-cli/errs"
)

const graphQueryTestReceiveTimeout = time.Second

func receiveGraphQueryTest[T any](t *testing.T, values <-chan T, description string) T {
	t.Helper()
	select {
	case value, ok := <-values:
		if !ok {
			t.Fatalf("%s channel closed before a value arrived", description)
		}
		return value
	case <-time.After(graphQueryTestReceiveTimeout):
		t.Fatalf("timed out after %s waiting for %s", graphQueryTestReceiveTimeout, description)
		var zero T
		return zero
	}
}

func waitForGraphQueryTestSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(graphQueryTestReceiveTimeout):
		t.Fatalf("timed out after %s waiting for %s", graphQueryTestReceiveTimeout, description)
	}
}

func TestExecuteGraphWindowDoesNotCallWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	result := executeGraphWindow(ctx, 0, graphTimeWindow{Index: 1}, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
		calls++
		return map[string]interface{}{}, nil
	}, time.Nanosecond)
	if !errors.Is(result.Err, context.Canceled) || result.Attempts != 0 || calls != 0 {
		t.Fatalf("result = attempts:%d err:%v calls:%d, want canceled before any call", result.Attempts, result.Err, calls)
	}
}

func TestExecuteGraphWindowRechecksContextAfterRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	waits := 0
	retryErr := errs.NewAPIError(errs.SubtypeUnknown, "retry later").WithCode(graphQueryRetryCode)
	result := executeGraphWindowWithRetryWait(ctx, 0, graphTimeWindow{Index: 1}, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
		calls++
		return nil, retryErr
	}, time.Hour, func(waitCtx context.Context, _ time.Duration) error {
		waits++
		cancel()
		if !errors.Is(waitCtx.Err(), context.Canceled) {
			t.Fatalf("wait context error = %v, want context.Canceled", waitCtx.Err())
		}
		return nil
	})
	if !errors.Is(result.Err, context.Canceled) || result.Attempts != 1 || calls != 1 || waits != 1 {
		t.Fatalf("result = attempts:%d err:%v calls:%d waits:%d, want canceled after one call and one nil wait", result.Attempts, result.Err, calls, waits)
	}
}

func TestExecuteGraphWindowRetriesCode2200ThenSucceeds(t *testing.T) {
	window := graphTimeWindow{Index: 1, StartTimeSec: 100, EndTimeSec: 200}
	calls := 0
	result := executeGraphWindow(context.Background(), 0, window, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
		calls++
		if calls < 3 {
			return nil, errs.NewAPIError(errs.SubtypeUnknown, "temporary graph failure").WithCode(2200)
		}
		return map[string]interface{}{"nodes": []interface{}{}}, nil
	}, time.Nanosecond)
	if result.Err != nil || result.Attempts != 3 || calls != 3 {
		t.Fatalf("result = attempts:%d err:%v calls:%d, want success on call 3", result.Attempts, result.Err, calls)
	}
}

func TestExecuteGraphWindowRetriesTypedNonNetworkRetryableError(t *testing.T) {
	calls := 0
	retryErr := errs.NewInternalError(errs.SubtypeUnknown, "temporary internal failure").WithCode(1234).WithRetryable()
	problem, ok := errs.ProblemOf(retryErr)
	if !ok || !errs.IsRetryable(retryErr) || errs.IsNetwork(retryErr) || errs.IsAPI(retryErr) || problem.Code == graphQueryRetryCode {
		t.Fatalf("retry error = %#v, want typed generic retryable error outside network and code %d paths", problem, graphQueryRetryCode)
	}
	result := executeGraphWindow(context.Background(), 0, graphTimeWindow{Index: 1}, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
		calls++
		if calls == 1 {
			return nil, retryErr
		}
		return map[string]interface{}{}, nil
	}, time.Nanosecond)
	if result.Err != nil || result.Attempts != 2 || calls != 2 {
		t.Fatalf("result = attempts:%d err:%v calls:%d, want success on call 2", result.Attempts, result.Err, calls)
	}
}

func TestExecuteGraphWindowRetriesNetworkErrorWithoutRetryableThreeTimes(t *testing.T) {
	calls := 0
	networkErr := errs.NewNetworkError(errs.SubtypeNetworkTransport, "temporary transport failure")
	result := executeGraphWindow(context.Background(), 0, graphTimeWindow{Index: 1}, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
		calls++
		return nil, networkErr
	}, time.Nanosecond)
	if result.Err != networkErr || result.Attempts != 3 || calls != 3 {
		t.Fatalf("result = attempts:%d err:%v calls:%d, want final network error after 3 calls", result.Attempts, result.Err, calls)
	}
	problem, ok := errs.ProblemOf(result.Err)
	if !ok || problem.Retryable {
		t.Fatalf("problem = %#v, want non-retryable typed network error", problem)
	}
}

func TestExecuteGraphWindowDoesNotRetryExcludedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "context canceled", err: context.Canceled},
		{name: "context deadline exceeded", err: context.DeadlineExceeded},
		{name: "authentication", err: errs.NewAuthenticationError(errs.SubtypeTokenMissing, "login required").WithRetryable()},
		{name: "permission", err: errs.NewPermissionError(errs.SubtypeMissingScope, "scope required").WithRetryable()},
		{name: "validation", err: errs.NewValidationError(errs.SubtypeInvalidArgument, "bad input").WithRetryable()},
		{name: "content safety", err: errs.NewContentSafetyError(errs.SubtypeContentSafety, "blocked").WithRetryable()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			result := executeGraphWindow(context.Background(), 0, graphTimeWindow{Index: 1}, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
				calls++
				return nil, tt.err
			}, time.Nanosecond)
			if result.Err == nil || result.Attempts != 1 || calls != 1 {
				t.Fatalf("result = attempts:%d err:%v calls:%d, want one failed call", result.Attempts, result.Err, calls)
			}
		})
	}
}

func TestExecuteGraphWindowStopsAfterThreeRetryableFailures(t *testing.T) {
	calls := 0
	finalErr := errs.NewAPIError(errs.SubtypeUnknown, "still unavailable").WithCode(2200)
	result := executeGraphWindow(context.Background(), 0, graphTimeWindow{Index: 1}, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
		calls++
		return nil, finalErr
	}, time.Nanosecond)
	if result.Err != finalErr || result.Attempts != 3 || calls != 3 {
		t.Fatalf("result = attempts:%d err:%v calls:%d, want final error after 3 calls", result.Attempts, result.Err, calls)
	}
}

func TestExecuteGraphWindowCancellationInterruptsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	firstCall := make(chan struct{})
	done := make(chan graphWindowExecution, 1)
	calls := 0
	go func() {
		done <- executeGraphWindow(ctx, 0, graphTimeWindow{Index: 1}, func(context.Context, graphTimeWindow) (map[string]interface{}, error) {
			calls++
			close(firstCall)
			return nil, errs.NewAPIError(errs.SubtypeUnknown, "retry later").WithCode(2200)
		}, time.Hour)
	}()
	waitForGraphQueryTestSignal(t, firstCall, "first graph window call")
	cancel()
	result := receiveGraphQueryTest(t, done, "canceled graph window result")
	if !errors.Is(result.Err, context.Canceled) || result.Attempts != 1 || calls != 1 {
		t.Fatalf("result = attempts:%d err:%v calls:%d, want canceled backoff after one call", result.Attempts, result.Err, calls)
	}
}

func TestRunGraphQueryWindowsStartsAllWorkersConcurrently(t *testing.T) {
	windows := []graphTimeWindow{
		{Index: 1, StartTimeSec: 100, EndTimeSec: 86500},
		{Index: 2, StartTimeSec: 86500, EndTimeSec: 172900},
		{Index: 3, StartTimeSec: 172900, EndTimeSec: 172901},
	}
	started := make(chan int, len(windows))
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runGraphQueryWindows(context.Background(), windows, func(_ context.Context, window graphTimeWindow) (map[string]interface{}, error) {
			started <- window.Index
			<-release
			return map[string]interface{}{"window": window.Index}, nil
		}, func(graphWindowResult) error { return nil }, time.Nanosecond)
	}()
	seen := map[int]bool{}
	for range windows {
		seen[receiveGraphQueryTest(t, started, "concurrently started graph window")] = true
	}
	close(release)
	if err := receiveGraphQueryTest(t, done, "concurrent graph window coordinator result"); err != nil {
		t.Fatalf("runGraphQueryWindows: %v", err)
	}
	for _, window := range windows {
		if !seen[window.Index] {
			t.Fatalf("window %d did not start before release", window.Index)
		}
	}
}

func TestConsumeGraphWindowResultsEmitsOutOfOrderArrivalsChronologically(t *testing.T) {
	results := make(chan graphWindowExecution, 3)
	results <- graphWindowExecution{Position: 2, Window: graphTimeWindow{Index: 3}, Data: map[string]interface{}{}}
	results <- graphWindowExecution{Position: 1, Window: graphTimeWindow{Index: 2}, Data: map[string]interface{}{}}
	results <- graphWindowExecution{Position: 0, Window: graphTimeWindow{Index: 1}, Data: map[string]interface{}{}}

	var emitted []int
	err := consumeGraphWindowResults(results, 3, func(out graphWindowResult) error {
		emitted = append(emitted, out.WindowIndex)
		return nil
	})
	if err != nil {
		t.Fatalf("consumeGraphWindowResults: %v", err)
	}
	if want := []int{1, 2, 3}; len(emitted) != len(want) || emitted[0] != want[0] || emitted[1] != want[1] || emitted[2] != want[2] {
		t.Fatalf("emitted = %v, want %v", emitted, want)
	}
}

func TestConsumeGraphWindowResultsReturnsEarliestFailureAfterOnlyPrefix(t *testing.T) {
	window2Err := errs.NewAPIError(errs.SubtypeUnknown, "window two failed").WithCode(1040)
	window3Err := errs.NewAPIError(errs.SubtypeUnknown, "window three failed").WithCode(1041)
	results := make(chan graphWindowExecution, 3)
	results <- graphWindowExecution{Position: 2, Window: graphTimeWindow{Index: 3}, Err: window3Err, Attempts: 1}
	results <- graphWindowExecution{Position: 1, Window: graphTimeWindow{Index: 2}, Err: window2Err, Attempts: 1}
	results <- graphWindowExecution{Position: 0, Window: graphTimeWindow{Index: 1}, Data: map[string]interface{}{}, Attempts: 1}

	emitted := []int{}
	err := consumeGraphWindowResults(results, 3, func(out graphWindowResult) error {
		emitted = append(emitted, out.WindowIndex)
		return nil
	})
	if !errors.Is(err, window2Err) || len(emitted) != 1 || emitted[0] != 1 {
		t.Fatalf("err = %v emitted = %v, want window 2 error after prefix [1]", err, emitted)
	}
	problem, ok := errs.ProblemOf(err)
	if !ok || problem.Code != 1040 || problem.Hint != "failed window: window_index=2 start_time_sec=0 end_time_sec=0 attempts=1" {
		t.Fatalf("problem = %#v, want annotated earliest failure", problem)
	}
}

func TestRunGraphQueryWindowsOutputFailureCancelsAndJoinsWorkers(t *testing.T) {
	windows := []graphTimeWindow{{Index: 1}, {Index: 2}}
	started := make(chan int, len(windows))
	releaseFirst := make(chan struct{})
	secondExited := make(chan struct{})
	writeErr := errors.New("write failed")
	done := make(chan error, 1)
	go func() {
		done <- runGraphQueryWindows(context.Background(), windows, func(ctx context.Context, window graphTimeWindow) (map[string]interface{}, error) {
			started <- window.Index
			if window.Index == 1 {
				<-releaseFirst
				return map[string]interface{}{}, nil
			}
			<-ctx.Done()
			close(secondExited)
			return nil, ctx.Err()
		}, func(graphWindowResult) error { return writeErr }, time.Nanosecond)
	}()
	for range windows {
		receiveGraphQueryTest(t, started, "graph window worker start")
	}
	close(releaseFirst)
	if err := receiveGraphQueryTest(t, done, "output failure coordinator result"); !errors.Is(err, writeErr) {
		t.Fatalf("error = %v, want write failure", err)
	}
	select {
	case <-secondExited:
	default:
		t.Fatal("coordinator returned before the canceled worker exited")
	}
}
