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

func TestMemoryGraphProtocolDryRun(t *testing.T) {
	setMemoryDryRunEnv(t)
	commands := map[string][]string{
		"+graph-range":   {"--start-time-sec", "100", "--end-time-sec", "86501"},
		"+graph-one-hop": {"--root", "2:docA", "--hop", "1"},
		"+graph-search":  {"--query", "最近项目"},
	}
	for command, flags := range commands {
		t.Run(command, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args := append([]string{"memory", command, "--dry-run"}, flags...)
			result, err := clie2e.RunCmd(ctx, clie2e.Request{Args: args})
			require.NoError(t, err)
			result.AssertExitCode(t, 0)
			plans := gjson.Get(result.Stdout, "api").Array()
			if command == "+graph-range" {
				require.Len(t, plans, 2)
			}
			for _, call := range plans {
				require.False(t, call.Get("body.options").Exists())
				require.False(t, call.Get("body.params.detail_format").Exists())
				require.False(t, call.Get("body.params.detailFormat").Exists())
			}
			require.False(t, gjson.Get(result.Stdout, "graph_query.options").Exists())
			require.False(t, gjson.Get(result.Stdout, "detail_format").Exists())
		})
		for _, flag := range []string{"--detail-format=json", "--use-v2", "--graph-query-use-v2"} {
			t.Run(command+"/"+flag, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				args := append([]string{"memory", command, "--dry-run", flag}, flags...)
				result, err := clie2e.RunCmd(ctx, clie2e.Request{Args: args})
				require.NoError(t, err)
				result.AssertExitCode(t, 2)
				require.Equal(t, "unknown flag", gjson.Get(result.Stderr, "error.params.0.reason").String())
				require.False(t, gjson.Get(result.Stdout, "api").Exists())
			})
		}
	}
}
