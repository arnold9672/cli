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

// TestMemoryGraphOneHopLive discovers a current-user root through GraphQuery,
// then verifies the read-only OneHop command against that stable root.
func TestMemoryGraphOneHopLive(t *testing.T) {
	if os.Getenv("TEST_MEMORY_GRAPH_ONE_HOP") != "1" {
		t.Skip("set TEST_MEMORY_GRAPH_ONE_HOP=1 after the PPE OneHop RPC is deployed")
	}
	if os.Getenv("TEST_USER_ACCESS_TOKEN") == "" {
		clie2e.SkipWithoutUserToken(t)
	}

	endTimeSec := time.Now().UTC().Unix()
	startTimeSec := endTimeSec - int64((24*time.Hour)/time.Second)
	discoveryCtx, discoveryCancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(discoveryCancel)
	discovery, err := clie2e.RunCmd(discoveryCtx, clie2e.Request{
		Args: []string{
			"memory", "+graph-query",
			"--start-time-sec", strconv.FormatInt(startTimeSec, 10),
			"--end-time-sec", strconv.FormatInt(endTimeSec, 10),
		},
		DefaultAs: "user",
		Format:    "json",
	})
	require.NoError(t, err)
	if discovery.ExitCode != 0 {
		t.Fatal(memoryGraphLiveFailureDetails(discovery.Stderr))
	}

	root, ok := firstExpandableGraphRoot(discovery.Stdout)
	if !ok {
		t.Skip("current user has no non-USER graph root in the last 24 hours")
	}
	oneHopCtx, oneHopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(oneHopCancel)
	result, err := clie2e.RunCmd(oneHopCtx, clie2e.Request{
		Args: []string{
			"memory", "+one-hop",
			"--root", root,
			"--lookback-days", "7",
			"--hop", "1",
		},
		DefaultAs: "user",
		Format:    "json",
	})
	require.NoError(t, err)
	if result.ExitCode != 0 {
		t.Fatal(memoryGraphLiveFailureDetails(result.Stderr))
	}
	require.True(t, gjson.Valid(result.Stdout), "Memory Graph OneHop must return JSON")
	require.True(t, gjson.Get(result.Stdout, "ok").Bool(), "Memory Graph OneHop must report success")
	require.True(t, gjson.Get(result.Stdout, "data.nodes").Exists(), "Memory Graph OneHop must include nodes")
	require.True(t, gjson.Get(result.Stdout, "data.edges").Exists(), "Memory Graph OneHop must include edges")
}

func firstExpandableGraphRoot(stdout string) (string, bool) {
	for _, node := range gjson.Get(stdout, "data.data.nodes").Array() {
		nodeType := node.Get("node_type").Int()
		rootID := node.Get("root_id").String()
		if nodeType < 1 || nodeType > 3 || rootID == "" {
			continue
		}
		return fmt.Sprintf("%d:%s", nodeType, rootID), true
	}
	return "", false
}

func TestFirstExpandableGraphRoot(t *testing.T) {
	stdout := `{"data":{"data":{"nodes":[{"node_type":4,"root_id":"user"},{"node_type":2,"root_id":"doc_123"}]}}}`
	root, ok := firstExpandableGraphRoot(stdout)
	require.True(t, ok)
	require.Equal(t, "2:doc_123", root)
}
