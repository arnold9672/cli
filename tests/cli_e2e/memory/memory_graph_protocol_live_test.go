// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	clie2e "code.byted.org/lark_search/larksuite-cli/tests/cli_e2e"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Opt-in, current-user-only smoke test. Never print graph bodies or identities.
// Empty Detail does not prove the server has deployed the new response fields.
func TestMemoryGraphProtocolLive(t *testing.T) {
	if os.Getenv("TEST_MEMORY_GRAPH_PROTOCOL") != "1" {
		t.Skip("set TEST_MEMORY_GRAPH_PROTOCOL=1 after the Memory Hub protocol is deployed")
	}
	clie2e.SkipWithoutUserToken(t)
	end := time.Now().Unix() - 2*24*60*60
	start := end - 24*60*60
	var discovery string
	if !t.Run("graph-query", func(t *testing.T) {
		args := []string{"memory", "+graph-range", "--start-time-sec", strconv.FormatInt(start, 10), "--end-time-sec", strconv.FormatInt(end, 10)}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err := clie2e.RunCmd(ctx, clie2e.Request{Args: args, DefaultAs: "user", Format: "json"})
		require.NoError(t, err)
		if result.ExitCode != 0 {
			t.Fatal(memoryGraphLiveFailureDetails(result.Stderr))
		}
		require.True(t, gjson.Get(result.Stdout, "ok").Bool(), "GraphQuery must report success")
		discovery = result.Stdout
		checkGraphProtocolLiveDetails(t, gjson.Get(result.Stdout, "data.data"))
	}) {
		return
	}
	t.Run("one-hop", func(t *testing.T) {
		root, ok := firstExpandableGraphRoot(discovery)
		if !ok {
			t.Skip("no reliable current-user root in the discovery window")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err := clie2e.RunCmd(ctx, clie2e.Request{
			Args:      []string{"memory", "+graph-one-hop", "--root", root, "--lookback-days", "4", "--hop", "1"},
			DefaultAs: "user", Format: "json",
		})
		require.NoError(t, err)
		if result.ExitCode != 0 {
			t.Fatal(memoryGraphLiveFailureDetails(result.Stderr))
		}
		require.True(t, gjson.Get(result.Stdout, "ok").Bool(), "OneHop must report success")
		checkGraphProtocolLiveDetails(t, gjson.Get(result.Stdout, "data"))
	})
}

type graphProtocolDetailCoverage struct {
	Items      int
	Markdown   int
	Source     int
	SourceOnly int
}

func (c graphProtocolDetailCoverage) hasSamples() bool {
	return c.Markdown > 0 || c.Source > 0
}

type graphProtocolLiveCoverage struct {
	Nodes graphProtocolDetailCoverage
	Edges graphProtocolDetailCoverage
}

func checkGraphProtocolLiveDetails(t *testing.T, data gjson.Result) {
	t.Helper()
	coverage := inspectGraphProtocolLiveDetails(t, data)
	t.Logf("protocol coverage: nodes=%+v edges=%+v", coverage.Nodes, coverage.Edges)
	if !coverage.Nodes.hasSamples() && !coverage.Edges.hasSamples() {
		t.Skip("this API returned no usable Markdown/source samples; protocol validation remains unverified")
	}
	for _, group := range []struct {
		name     string
		coverage graphProtocolDetailCoverage
	}{
		{name: "nodes", coverage: coverage.Nodes},
		{name: "edges", coverage: coverage.Edges},
	} {
		t.Run(group.name, func(t *testing.T) {
			if !group.coverage.hasSamples() {
				t.Skip("no usable samples for this object kind; other kinds do not validate it")
			}
			t.Logf("validated samples: items=%d markdown=%d source=%d source_only=%d", group.coverage.Items, group.coverage.Markdown, group.coverage.Source, group.coverage.SourceOnly)
		})
	}
}

func inspectGraphProtocolLiveDetails(t *testing.T, data gjson.Result) graphProtocolLiveCoverage {
	t.Helper()
	var coverage graphProtocolLiveCoverage
	for _, group := range []struct {
		name     string
		coverage *graphProtocolDetailCoverage
	}{
		{name: "nodes", coverage: &coverage.Nodes},
		{name: "edges", coverage: &coverage.Edges},
	} {
		items := data.Get(group.name)
		require.True(t, items.IsArray(), "Graph data must contain node/edge lists")
		group.coverage.Items = len(items.Array())
		for _, item := range items.Array() {
			require.True(t, item.IsObject(), "Graph items must be objects")
			detail := item.Get("detail")
			if !detail.Exists() || detail.Type == gjson.Null {
				continue
			}
			require.True(t, detail.IsObject(), "Detail must be an object")
			for _, field := range []string{"markdown"} {
				if value := detail.Get(field); value.Exists() && value.Type != gjson.Null {
					require.True(t, value.Type == gjson.String, "Detail text fields must remain strings")
				}
			}
			hasSource := false
			if sources := detail.Get("source"); sources.Exists() && sources.Type != gjson.Null {
				require.True(t, sources.IsArray(), "Detail.source must be a list")
				for _, source := range sources.Array() {
					require.True(t, source.IsObject(), "Detail source entry must be an object")
					for _, field := range []string{"entity_id", "entity_type"} {
						if value := source.Get(field); value.Exists() && value.Type != gjson.Null {
							require.True(t, value.Type == gjson.String, "Source IDs/types must remain strings")
							hasSource = hasSource || strings.TrimSpace(value.String()) != ""
						}
					}
				}
			}
			hasMarkdown := strings.TrimSpace(detail.Get("markdown").String()) != ""
			if hasMarkdown {
				group.coverage.Markdown++
			}
			if hasSource {
				group.coverage.Source++
				if !hasMarkdown {
					group.coverage.SourceOnly++
				}
			}
		}
	}
	return coverage
}
