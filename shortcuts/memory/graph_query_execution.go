package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"code.byted.org/lark_search/larksuite-cli/errs"
)

const (
	graphQueryMaxAttempts   = 3
	graphQueryRetryInterval = 200 * time.Millisecond
	graphQueryRetryCode     = 2200
)

type graphWindowCall func(context.Context, graphTimeWindow) (map[string]interface{}, error)

type graphWindowRetryWait func(context.Context, time.Duration) error

type graphWindowExecution struct {
	Position int
	Window   graphTimeWindow
	Data     map[string]interface{}
	Err      error
	Attempts int
}

type graphWindowEmit func(graphWindowResult) error

func executeGraphWindow(ctx context.Context, position int, window graphTimeWindow, call graphWindowCall, retryDelay time.Duration) graphWindowExecution {
	return executeGraphWindowWithRetryWait(ctx, position, window, call, retryDelay, waitGraphQueryRetry)
}

func executeGraphWindowWithRetryWait(ctx context.Context, position int, window graphTimeWindow, call graphWindowCall, retryDelay time.Duration, wait graphWindowRetryWait) graphWindowExecution {
	result := graphWindowExecution{Position: position, Window: window}
	for attempt := 1; attempt <= graphQueryMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			result.Err = err
			return result
		}
		result.Attempts = attempt
		data, err := call(ctx, window)
		if err == nil {
			result.Data = data
			return result
		}
		if attempt == graphQueryMaxAttempts || !isGraphQueryRetryable(err) {
			result.Err = err
			return result
		}
		if err := wait(ctx, retryDelay); err != nil {
			result.Err = err
			return result
		}
		if err := ctx.Err(); err != nil {
			result.Err = err
			return result
		}
	}
	return result
}

func isGraphQueryRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errs.IsAuthentication(err) || errs.IsPermission(err) || errs.IsValidation(err) || errs.IsContentSafety(err) {
		return false
	}
	if problem, ok := errs.ProblemOf(err); ok && errs.IsAPI(err) && problem.Code == graphQueryRetryCode {
		return true
	}
	return errs.IsNetwork(err) || errs.IsRetryable(err)
}

func waitGraphQueryRetry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func runGraphQueryWindows(ctx context.Context, windows []graphTimeWindow, call graphWindowCall, emit graphWindowEmit, retryDelay time.Duration) error {
	if len(windows) == 0 {
		return nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	results := make(chan graphWindowExecution, len(windows))
	var workers sync.WaitGroup
	for position, window := range windows {
		workers.Add(1)
		go func(position int, window graphTimeWindow) {
			defer workers.Done()
			results <- executeGraphWindow(workerCtx, position, window, call, retryDelay)
		}(position, window)
	}
	defer func() {
		cancel()
		workers.Wait()
	}()

	return consumeGraphWindowResults(results, len(windows), emit)
}

func consumeGraphWindowResults(results <-chan graphWindowExecution, windowCount int, emit graphWindowEmit) error {
	pending := make(map[int]graphWindowExecution, windowCount)
	next := 0
	for completed := 0; completed < windowCount; completed++ {
		result := <-results
		pending[result.Position] = result
		for next < windowCount {
			ready, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			if ready.Err != nil {
				return annotateGraphWindowError(ready.Err, ready.Window, ready.Attempts)
			}
			out := graphWindowResult{
				WindowIndex:  ready.Window.Index,
				StartTimeSec: ready.Window.StartTimeSec,
				EndTimeSec:   ready.Window.EndTimeSec,
				Data:         unwrapMemoryData(ready.Data),
			}
			if err := emit(out); err != nil {
				return annotateGraphWindowError(err, ready.Window, ready.Attempts)
			}
			next++
		}
	}
	return nil
}
