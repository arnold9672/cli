// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"strconv"
	"testing"
	"time"

	"code.byted.org/lark_search/larksuite-cli/internal/core"
	clie2e "code.byted.org/lark_search/larksuite-cli/tests/cli_e2e"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMemoryDryRun(t *testing.T) {
	setMemoryDryRunEnv(t)

	t.Run("list", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)

		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args: []string{"memory", "+list", "--dry-run"},
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 0)

		require.Equal(t, "GET", gjson.Get(result.Stdout, "api.0.method").String())
		require.Equal(t, "/open-apis/search/v2/memory_hub/list_memory", gjson.Get(result.Stdout, "api.0.url").String())
	})

	t.Run("get defaults full payload mode", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)

		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args: []string{
				"memory", "+get",
				"--memory-key", "personal_memory_snapshot",
				"--dry-run",
			},
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 0)

		require.Equal(t, "POST", gjson.Get(result.Stdout, "api.0.method").String())
		require.Equal(t, "/open-apis/search/v2/memory_hub/get_memory", gjson.Get(result.Stdout, "api.0.url").String())
		require.Equal(t, "personal_memory_snapshot", gjson.Get(result.Stdout, "api.0.body.memory_key").String())
		require.Equal(t, "full", gjson.Get(result.Stdout, "api.0.body.payload_mode").String())
	})

	t.Run("graph query plans one window", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)

		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args: []string{
				"memory", "+graph-query",
				"--start-time-sec", "1784476800",
				"--end-time-sec", "1784563200",
				"--dry-run",
			},
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 0)

		assertGraphQueryDryRunPlan(t, result.Stdout, "ou_graph_test_user", "markdown", [][2]int64{
			{1784476800, 1784563200},
		})
	})

	t.Run("graph query plans two exact-start windows", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)

		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args: []string{
				"memory", "+graph-query",
				"--start-time-sec", "100",
				"--end-time-sec", "86501",
				"--detail-format", "json",
				"--dry-run",
			},
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 0)

		assertGraphQueryDryRunPlan(t, result.Stdout, "ou_graph_test_user", "json", [][2]int64{
			{100, 86500},
			{86500, 86501},
		})
	})

	for _, tc := range []struct {
		name             string
		detailFormatFlag string
		wantDetailFormat string
	}{
		{name: "graph query normalizes padded markdown", detailFormatFlag: " markdown ", wantDetailFormat: "markdown"},
		{name: "graph query normalizes padded json", detailFormatFlag: " json ", wantDetailFormat: "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)

			result, err := clie2e.RunCmd(ctx, clie2e.Request{
				Args: []string{
					"memory", "+graph-query",
					"--start-time-sec", "100",
					"--end-time-sec", "101",
					"--detail-format", tc.detailFormatFlag,
					"--dry-run",
				},
			})
			require.NoError(t, err)
			result.AssertExitCode(t, 0)
			assertGraphQueryDryRunPlan(t, result.Stdout, "ou_graph_test_user", tc.wantDetailFormat, [][2]int64{{100, 101}})
		})
	}

	t.Run("graph query rejects ranges over seven days before planning", func(t *testing.T) {
		t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
		t.Setenv("LARKSUITE_CLI_APP_ID", "")
		t.Setenv("LARKSUITE_CLI_APP_SECRET", "")
		t.Setenv("LARKSUITE_CLI_BRAND", "")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)

		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args: []string{
				"memory", "+graph-query",
				"--start-time-sec", "100",
				"--end-time-sec", "604901",
				"--dry-run",
			},
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 2)

		require.Equal(t, "validation", gjson.Get(result.Stderr, "error.type").String(), "stderr:\n%s", result.Stderr)
		require.Equal(t, "invalid_argument", gjson.Get(result.Stderr, "error.subtype").String(), "stderr:\n%s", result.Stderr)
		require.Equal(t, "--end-time-sec", gjson.Get(result.Stderr, "error.param").String(), "stderr:\n%s", result.Stderr)
		require.False(t, gjson.Get(result.Stdout, "api").Exists(), "stdout must not contain an API plan:\n%s", result.Stdout)
	})

	t.Run("graph query rejects deprecated user ID flag without planning", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)

		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args: []string{
				"memory", "+graph-query",
				"--user-id", "ou_other_user",
				"--start-time-sec", "100",
				"--end-time-sec", "101",
				"--dry-run",
			},
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 2)
		require.Equal(t, "--user-id", gjson.Get(result.Stderr, "error.params.0.name").String(), "stderr:\n%s", result.Stderr)
		require.Equal(t, "unknown flag", gjson.Get(result.Stderr, "error.params.0.reason").String(), "stderr:\n%s", result.Stderr)
		require.False(t, gjson.Get(result.Stdout, "api").Exists(), "stdout must not contain an API plan:\n%s", result.Stdout)
	})

	for _, tc := range []struct {
		name       string
		formatArgs []string
	}{
		{name: "graph query rejects unsupported detail format before config", formatArgs: []string{"--detail-format", "xml"}},
		{name: "graph query rejects explicit empty detail format before config", formatArgs: []string{"--detail-format="}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)

			args := []string{
				"memory", "+graph-query",
				"--start-time-sec", "100",
				"--end-time-sec", "101",
				"--dry-run",
			}
			args = append(args, tc.formatArgs...)
			result, err := clie2e.RunCmd(ctx, clie2e.Request{Args: args})
			require.NoError(t, err)
			result.AssertExitCode(t, 2)
			require.Equal(t, "validation", gjson.Get(result.Stderr, "error.type").String(), "stderr:\n%s", result.Stderr)
			require.Equal(t, "invalid_argument", gjson.Get(result.Stderr, "error.subtype").String(), "stderr:\n%s", result.Stderr)
			require.Equal(t, "--detail-format", gjson.Get(result.Stderr, "error.param").String(), "stderr:\n%s", result.Stderr)
			require.False(t, gjson.Get(result.Stdout, "api").Exists(), "stdout must not contain an API plan:\n%s", result.Stdout)
		})
	}

	for _, tc := range []struct {
		name  string
		args  []string
		param string
	}{
		{
			name:  "graph query missing start time reports typed param",
			args:  []string{"memory", "+graph-query", "--end-time-sec", "101", "--dry-run"},
			param: "--start-time-sec",
		},
		{
			name:  "graph query missing end time reports typed param",
			args:  []string{"memory", "+graph-query", "--start-time-sec", "100", "--dry-run"},
			param: "--end-time-sec",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)

			result, err := clie2e.RunCmd(ctx, clie2e.Request{Args: tc.args})
			require.NoError(t, err)
			result.AssertExitCode(t, 2)
			require.Equal(t, "validation", gjson.Get(result.Stderr, "error.type").String(), "stderr:\n%s", result.Stderr)
			require.Equal(t, "invalid_argument", gjson.Get(result.Stderr, "error.subtype").String(), "stderr:\n%s", result.Stderr)
			require.Equal(t, tc.param, gjson.Get(result.Stderr, "error.param").String(), "stderr:\n%s", result.Stderr)
			require.False(t, gjson.Get(result.Stdout, "api").Exists(), "stdout must not contain an API plan:\n%s", result.Stdout)
		})
	}
}

func assertGraphQueryDryRunPlan(t *testing.T, stdout, userID, detailFormat string, windows [][2]int64) {
	t.Helper()
	require.Equal(t, int64(len(windows)), gjson.Get(stdout, "api.#").Int(), "stdout:\n%s", stdout)
	for i, window := range windows {
		path := "api." + strconv.Itoa(i)
		require.Equal(t, "POST", gjson.Get(stdout, path+".method").String(), "stdout:\n%s", stdout)
		require.Equal(t, "/open-apis/search/v2/memory_hub/graph_query", gjson.Get(stdout, path+".url").String(), "stdout:\n%s", stdout)
		require.Equal(t, userID, gjson.Get(stdout, path+".body.user_id").String(), "stdout:\n%s", stdout)
		require.Equal(t, detailFormat, gjson.Get(stdout, path+".body.params.detail_format").String(), "stdout:\n%s", stdout)
		require.Equal(t, window[0], gjson.Get(stdout, path+".body.time_range.start_time_sec").Int(), "stdout:\n%s", stdout)
		require.Equal(t, window[1], gjson.Get(stdout, path+".body.time_range.end_time_sec").Int(), "stdout:\n%s", stdout)
	}
}

func setMemoryDryRunEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv("LARKSUITE_CLI_APP_ID", "")
	t.Setenv("LARKSUITE_CLI_APP_SECRET", "")
	t.Setenv("LARKSUITE_CLI_BRAND", "")
	t.Setenv("LARKSUITE_CLI_USER_ACCESS_TOKEN", "")
	t.Setenv("LARKSUITE_CLI_TENANT_ACCESS_TOKEN", "")
	t.Setenv("LARKSUITE_CLI_NO_UPDATE_NOTIFIER", "1")
	t.Setenv("LARKSUITE_CLI_NO_SKILLS_NOTIFIER", "1")
	require.NoError(t, core.SaveMultiAppConfig(&core.MultiAppConfig{
		Apps: []core.AppConfig{{
			AppId:     "memory_dryrun_test",
			AppSecret: core.PlainSecret("memory_dryrun_secret"),
			Brand:     core.BrandFeishu,
			DefaultAs: core.AsUser,
			Users:     []core.AppUser{{UserOpenId: "ou_graph_test_user"}},
		}},
	}))
}
