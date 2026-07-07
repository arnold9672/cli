// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"testing"
	"time"

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
			Args:      []string{"memory", "+list", "--dry-run"},
			DefaultAs: "user",
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
			DefaultAs: "user",
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 0)

		require.Equal(t, "POST", gjson.Get(result.Stdout, "api.0.method").String())
		require.Equal(t, "/open-apis/search/v2/memory_hub/get_memory", gjson.Get(result.Stdout, "api.0.url").String())
		require.Equal(t, "personal_memory_snapshot", gjson.Get(result.Stdout, "api.0.body.memory_key").String())
		require.Equal(t, "full", gjson.Get(result.Stdout, "api.0.body.payload_mode").String())
	})

	t.Run("get accepts variant_key alias", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)

		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args: []string{
				"memory", "+get",
				"--memory-key", "personal_memory_snapshot",
				"--variant_key", "default",
				"--dry-run",
			},
			DefaultAs: "user",
		})
		require.NoError(t, err)
		result.AssertExitCode(t, 0)

		require.Equal(t, "POST", gjson.Get(result.Stdout, "api.0.method").String())
		require.Equal(t, "/open-apis/search/v2/memory_hub/get_memory", gjson.Get(result.Stdout, "api.0.url").String())
		require.Equal(t, "personal_memory_snapshot", gjson.Get(result.Stdout, "api.0.body.memory_key").String())
		require.Equal(t, "default", gjson.Get(result.Stdout, "api.0.body.variant_key").String())
		require.Equal(t, "full", gjson.Get(result.Stdout, "api.0.body.payload_mode").String())
	})
}

func setMemoryDryRunEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LARKSUITE_CLI_APP_ID", "memory_dryrun_test")
	t.Setenv("LARKSUITE_CLI_APP_SECRET", "memory_dryrun_secret")
	t.Setenv("LARKSUITE_CLI_BRAND", "feishu")
	t.Setenv("LARKSUITE_CLI_NO_UPDATE_NOTIFIER", "1")
	t.Setenv("LARKSUITE_CLI_NO_SKILLS_NOTIFIER", "1")
}
