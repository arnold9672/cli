// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These are offline tests of the live-test gate, not requests to a service.
func TestGraphProtocolLiveCoverageCounts(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want graphProtocolLiveCoverage
	}{
		{name: "empty graph", data: `{"nodes":[],"edges":[]}`},
		{
			name: "missing and old fields are not protocol samples",
			data: `{"nodes":[{}, {"detail":null}, {"detail":{}}, {"detail":{"content":"old body"}}],"edges":[]}`,
			want: graphProtocolLiveCoverage{Nodes: graphProtocolDetailCoverage{Items: 4}},
		},
		{
			name: "nodes cannot provide edge coverage",
			data: `{"nodes":[{"detail":{"markdown":"node body"}}],"edges":[{"detail":{}}]}`,
			want: graphProtocolLiveCoverage{Nodes: graphProtocolDetailCoverage{Items: 1, Markdown: 1}, Edges: graphProtocolDetailCoverage{Items: 1}},
		},
		{
			name: "source-only edges are valid",
			data: `{"nodes":[],"edges":[{"detail":{"source":[{"entity_id":"9223372036854775808","entity_type":"message"}]}}]}`,
			want: graphProtocolLiveCoverage{Edges: graphProtocolDetailCoverage{Items: 1, Source: 1, SourceOnly: 1}},
		},
		{
			name: "empty fields are not usable samples",
			data: `{"nodes":[{"detail":{"markdown":"  ","source":[{}, {"entity_id":" ","entity_type":""}]}}],"edges":[{"detail":{"markdown":null,"source":null}}]}`,
			want: graphProtocolLiveCoverage{Nodes: graphProtocolDetailCoverage{Items: 1}, Edges: graphProtocolDetailCoverage{Items: 1}},
		},
		{
			name: "count detail objects instead of source entries",
			data: `{"nodes":[{"detail":{"markdown":"node body","source":[{"entity_id":"a"},{"entity_id":"b"}]}},{"detail":{"source":[{"entity_type":"document"}]}}],"edges":[{"detail":{"markdown":"edge body"}}]}`,
			want: graphProtocolLiveCoverage{Nodes: graphProtocolDetailCoverage{Items: 2, Markdown: 1, Source: 2, SourceOnly: 1}, Edges: graphProtocolDetailCoverage{Items: 1, Markdown: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := inspectGraphProtocolLiveDetails(t, gjson.Parse(tc.data))
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.want.Nodes.Markdown > 0 || tc.want.Nodes.Source > 0, got.Nodes.hasSamples())
			require.Equal(t, tc.want.Edges.Markdown > 0 || tc.want.Edges.Source > 0, got.Edges.hasSamples())
		})
	}
}

func TestGraphProtocolLiveCoverageSkipsEachAPIIndependently(t *testing.T) {
	for _, tc := range []struct {
		name        string
		data        string
		wantSkipped bool
	}{
		{name: "graph-query has a sample", data: `{"nodes":[{"detail":{"markdown":"body"}}],"edges":[]}`},
		{name: "one-hop empty must not inherit graph-query coverage", data: `{"nodes":[],"edges":[]}`, wantSkipped: true},
		{name: "one-hop missing protocol fields", data: `{"nodes":[{"detail":{"content":"old body"}}],"edges":[{"detail":{}}]}`, wantSkipped: true},
		{name: "one-hop source-only is valid", data: `{"nodes":[],"edges":[{"detail":{"source":[{"entity_id":"9223372036854775809","entity_type":"message"}]}}]}`},
	} {
		var skipped bool
		passed := t.Run(tc.name, func(t *testing.T) {
			defer func() { skipped = t.Skipped() }()
			checkGraphProtocolLiveDetails(t, gjson.Parse(tc.data))
		})
		require.True(t, passed, "coverage gate must not fail valid empty/source-only payloads")
		require.Equal(t, tc.wantSkipped, skipped, "each API must report its own coverage")
	}
}
