// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	clie2e "code.byted.org/lark_search/larksuite-cli/tests/cli_e2e"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestMemoryGraphQueryLive verifies an authenticated, read-only Graph query.
// It intentionally asserts only the success envelope, never graph contents.
func TestMemoryGraphQueryLive(t *testing.T) {
	if os.Getenv("TEST_USER_ACCESS_TOKEN") == "" {
		clie2e.SkipWithoutUserToken(t)
	}

	endTimeSec := time.Now().UTC().Unix()
	startTimeSec := endTimeSec - int64((5*time.Minute)/time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	result, err := clie2e.RunCmd(ctx, clie2e.Request{
		Args: []string{
			"memory", "+graph-query",
			"--start-time-sec", strconv.FormatInt(startTimeSec, 10),
			"--end-time-sec", strconv.FormatInt(endTimeSec, 10),
		},
		DefaultAs: "user",
		Format:    "json",
	})
	require.NoError(t, err)
	if result.ExitCode != 0 {
		t.Fatal(memoryGraphLiveFailureDetails(result.Stderr))
	}
	require.True(t, gjson.Valid(result.Stdout), "Memory Graph query must return JSON")
	require.True(t, gjson.Get(result.Stdout, "ok").Exists(), "Memory Graph query JSON must include ok")
	require.True(t, gjson.Get(result.Stdout, "ok").Bool(), "Memory Graph query JSON must report success")
	require.True(t, gjson.Get(result.Stdout, "data").Exists(), "Memory Graph query JSON must include data")
}

func memoryGraphLiveFailureDetails(stderr string) string {
	return fmt.Sprintf(
		"error.type=%q error.subtype=%q error.code=%q error.log_id=%q",
		gjson.Get(stderr, "error.type").String(),
		gjson.Get(stderr, "error.subtype").String(),
		gjson.Get(stderr, "error.code").Raw,
		gjson.Get(stderr, "error.log_id").String(),
	)
}

func TestMemoryGraphLiveFailureDetails(t *testing.T) {
	stderr := `{"error":{"type":"api_error","subtype":"unknown","code":99992351,"log_id":"safe-log-id","message":"ou_runtime_open_id","hint":"graph payload must not appear"}}`
	got := memoryGraphLiveFailureDetails(stderr)
	require.Equal(t, `error.type="api_error" error.subtype="unknown" error.code="99992351" error.log_id="safe-log-id"`, got)
	require.NotContains(t, got, "ou_runtime_open_id")
	require.NotContains(t, got, "graph payload")
}
