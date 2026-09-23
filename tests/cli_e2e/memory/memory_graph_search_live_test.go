// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"os"
	"testing"
	"time"

	clie2e "code.byted.org/lark_search/larksuite-cli/tests/cli_e2e"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestMemoryGraphSearchLive validates the intranet Knowledge QA -> OneHop
// composition. It is opt-in until the OneHop endpoint/ACL is deployed for the
// test profile.
func TestMemoryGraphSearchLive(t *testing.T) {
	if os.Getenv("TEST_MEMORY_GRAPH_SEARCH") != "1" {
		t.Skip("set TEST_MEMORY_GRAPH_SEARCH=1 after the intranet FaaS and OneHop endpoint are available")
	}
	if os.Getenv("TEST_USER_ACCESS_TOKEN") == "" {
		clie2e.SkipWithoutUserToken(t)
	}
	query := os.Getenv("TEST_MEMORY_GRAPH_SEARCH_QUERY")
	if query == "" {
		query = "什么是知识问答"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	result, err := clie2e.RunCmd(ctx, clie2e.Request{
		Args: []string{
			"memory", "+graph-search",
			"--query", query,
			"--concurrency", "4",
			"--graph-query-mode", "on",
			"--graph-query-lookback-days", "1",
		},
		DefaultAs: "user",
		Format:    "json",
	})
	require.NoError(t, err)
	if result.ExitCode != 0 {
		t.Fatal(memoryGraphLiveFailureDetails(result.Stderr))
	}
	require.True(t, gjson.Valid(result.Stdout), "Graph Search must return JSON")
	require.True(t, gjson.Get(result.Stdout, "ok").Bool(), "Graph Search must report success")
	require.True(t, gjson.Get(result.Stdout, "data.search_candidates").Exists(), "Graph Search must include search candidates")
	require.True(t, gjson.Get(result.Stdout, "data.roots").Exists(), "Graph Search must include roots")
	require.True(t, gjson.Get(result.Stdout, "data.next_roots").Exists(), "Graph Search must include Agent-selectable next roots")
	require.Equal(t, "agent_controlled", gjson.Get(result.Stdout, "data.continuation.mode").String())
	require.Equal(t, int64(10), gjson.Get(result.Stdout, "data.continuation.max_total_hops").Int())
	require.True(t, gjson.Get(result.Stdout, "data.nodes").Exists(), "Graph Search must include nodes")
	require.True(t, gjson.Get(result.Stdout, "data.edges").Exists(), "Graph Search must include edges")
	require.True(t, gjson.Get(result.Stdout, "data.graph_query").Exists(), "Graph Search must describe supplemental GraphQuery")
	require.Equal(t, "knowledge_qa.passages", gjson.Get(result.Stdout, "data.evidence.candidate_source").String())
	require.True(t, gjson.Get(result.Stdout, "data.evidence.permission_filtered").Bool())
	require.True(t, gjson.Get(result.Stdout, "data.evidence.node_details").Exists())
	require.True(t, gjson.Get(result.Stdout, "data.evidence.edge_details").Exists())
	require.True(t, gjson.Get(result.Stdout, "data.evidence.edge_groups").Exists())
}
