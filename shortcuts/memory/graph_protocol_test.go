// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"

	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/httpmock"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

func TestGraphProtocolRemovedFlags(t *testing.T) {
	for _, shortcut := range []common.Shortcut{MemoryGraphRange, MemoryGraphOneHop, MemoryGraphSearch} {
		for _, flag := range []string{"--detail-format=json", "--use-v2", "--graph-query-use-v2"} {
			t.Run(shortcut.Command+"/"+flag, func(t *testing.T) {
				f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
				err := runMemoryShortcut(t, shortcut, []string{shortcut.Command, flag, "--dry-run", "--as", "user"}, f, stdout)
				if err == nil || !strings.Contains(err.Error(), "unknown flag") || stdout.Len() != 0 {
					t.Fatalf("removed flag must fail without a request: err=%v stdout=%q", err, stdout.String())
				}
			})
		}
	}
}

func TestGraphProtocolRangeAndOneHopPreserveNewDetails(t *testing.T) {
	nodeDetail := map[string]interface{}{"markdown": "# 节点正文", "source": []interface{}{graphProtocolSource("9223372036854775808", "document")}, "future_field": false}
	edgeDetail := map[string]interface{}{"markdown": "边的正文", "source": []interface{}{graphProtocolSource("message-id", "message")}}
	for _, command := range []string{"range", "one-hop"} {
		t.Run(command, func(t *testing.T) {
			f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
			node := graphSearchNode("node-1", 2, "docA", "2026-10-29", 100)
			node["detail"] = nodeDetail
			edge := graphSearchEdge("edge-1", "node-1", "node-1")
			edge["detail"] = edgeDetail
			path, shortcut, prefix := graphOneHopPath, MemoryGraphOneHop, "data."
			args := []string{"+graph-one-hop", "--root", "2:docA", "--hop", "1", "--as", "user", "--format", "json"}
			if command == "range" {
				path, shortcut, prefix = graphQueryPath, MemoryGraphRange, "data.data."
				args = []string{"+graph-range", "--start-time-sec", "100", "--end-time-sec", "101", "--as", "user", "--format", "ndjson"}
			}
			stub := &httpmock.Stub{Method: http.MethodPost, URL: path, Body: map[string]interface{}{"code": 0, "data": map[string]interface{}{"data": map[string]interface{}{
				"nodes": []interface{}{node}, "edges": []interface{}{edge},
			}}}}
			registry.Register(stub)
			if err := runMemoryShortcut(t, shortcut, args, f, stdout); err != nil {
				t.Fatal(err)
			}
			assertGraphProtocolDefaultRequest(t, string(stub.CapturedBody))
			for path, want := range map[string]map[string]interface{}{"nodes.0.detail": nodeDetail, "edges.0.detail": edgeDetail} {
				var actual map[string]interface{}
				if err := json.Unmarshal([]byte(gjson.Get(stdout.String(), prefix+path).Raw), &actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("%s = %#v, want %#v", path, actual, want)
				}
			}
			if command == "one-hop" && gjson.Get(stdout.String(), "meta.detail_bytes").Int() != int64(len("# 节点正文边的正文")) {
				t.Fatal("detail_bytes must count UTF-8 Markdown")
			}
		})
	}
}

func TestGraphProtocolSearchMergesMarkdownAndSources(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	anchor := time.Now().Unix() - 60
	date := time.Unix(anchor, 0).In(graphSearchLocation).Format("2006-01-02")
	registry.Register(&httpmock.Stub{Method: http.MethodPost, URL: graphSearchKnowledgeQAURL, Body: map[string]interface{}{
		"passages": []interface{}{graphSearchPassage("p1", graphSearchSourceDoc, "Project", "https://bytedance.larkoffice.com/docx/docA", 1, 0, anchor)},
	}})
	node := graphSearchNode("node-1", 2, "docA", date, anchor)
	node["detail"] = map[string]interface{}{"source": []interface{}{graphProtocolSource("docA", "document")}}
	edge := graphSearchEdge("edge-1", "node-1", "node-1")
	edge["detail"] = map[string]interface{}{"source": []interface{}{graphProtocolSource("m1", "message")}}
	oneHop := graphSearchOneHopStub("docA", []interface{}{node}, []interface{}{edge})
	registry.Register(oneHop)
	queryNode := graphSearchNode("node-1", 2, "docA", date, anchor)
	queryNode["detail"] = map[string]interface{}{"markdown": "新正文", "source": []interface{}{graphProtocolSource("docB", "document")}}
	queryEdge := graphSearchEdge("edge-1", "node-1", "node-1")
	queryEdge["detail"] = map[string]interface{}{"markdown": "新关系", "source": []interface{}{graphProtocolSource("m1", "message")}}
	query := &httpmock.Stub{Method: http.MethodPost, URL: graphQueryPath, Body: map[string]interface{}{"code": 0, "data": map[string]interface{}{"data": map[string]interface{}{
		"nodes": []interface{}{queryNode}, "edges": []interface{}{queryEdge},
	}}}}
	registry.Register(query)
	args := []string{"+graph-search", "--query", "最近项目", "--graph-query-mode", "on", "--graph-query-lookback-days", "1", "--as", "user", "--format", "json"}
	if err := runGraphSearchShortcut(t, args, f, stdout, registry); err != nil {
		t.Fatal(err)
	}
	assertGraphProtocolDefaultRequest(t, string(query.CapturedBody))
	assertGraphProtocolDefaultRequest(t, string(oneHop.CapturedBody))
	got := stdout.String()
	if gjson.Get(got, "data.nodes.0.detail.markdown").String() != "新正文" || gjson.Get(got, "data.edges.0.detail.markdown").String() != "新关系" ||
		!gjson.Get(got, "data.evidence.edge_details.0.content_present").Bool() || gjson.Get(got, "data.evidence.node_details.0.source_count").Int() != 2 ||
		gjson.Get(got, "data.edges.0.detail.source.#").Int() != 1 {
		t.Fatal("Markdown bodies or source union missing")
	}
}

func TestGraphProtocolDetailEvidence(t *testing.T) {
	for _, tc := range []struct {
		name             string
		detail           interface{}
		present, content bool
		bytes, sources   int
	}{
		{name: "missing"},
		{name: "empty", detail: map[string]interface{}{}},
		{name: "empty fields", detail: map[string]interface{}{"markdown": "", "source": nil}},
		{name: "markdown", detail: map[string]interface{}{"markdown": "正文"}, present: true, content: true, bytes: 6},
		{name: "source only", detail: map[string]interface{}{"source": []interface{}{graphProtocolSource("9223372036854775808", "document")}}, present: true, sources: 1},
		{name: "old fields ignored", detail: map[string]interface{}{"format": "json", "schema_version": "v1", "content": "old body"}},
		{name: "string ignored", detail: "old body"},
		{name: "no old body fallback", detail: map[string]interface{}{"markdown": " ", "content": "old body"}, bytes: 1},
		{name: "only markdown counted", detail: map[string]interface{}{"markdown": "正文", "content": "old body"}, present: true, content: true, bytes: 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index := buildGraphSearchEvidenceIndex(nil, []map[string]interface{}{{"node_id": "n", "detail": tc.detail}}, []map[string]interface{}{{"edge_id": "e", "detail": tc.detail}})
			n, e := index.NodeDetails[0], index.EdgeDetails[0]
			if n.DetailPresent != tc.present || e.DetailPresent != tc.present || n.ContentPresent != tc.content || e.ContentPresent != tc.content ||
				n.ContentBytes != tc.bytes || e.ContentBytes != tc.bytes || n.SourceCount != tc.sources || e.SourceCount != tc.sources {
				t.Fatalf("node=%#v edge=%#v", n, e)
			}
		})
	}
}

func TestGraphProtocolPrettyDetails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detail map[string]interface{}
		want   string
	}{
		{name: "markdown", detail: map[string]interface{}{"markdown": "# 正文"}, want: "detail.markdown:\n    # 正文"},
		{name: "source only", detail: map[string]interface{}{"source": []interface{}{graphProtocolSource("9223372036854775808", "doc")}}, want: "detail: source_only"},
		{name: "missing", want: "detail: missing"},
		{name: "old fields ignored", detail: map[string]interface{}{"content": "OLD_BODY", "format": "json", "schema_version": "v1"}, want: "detail: missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			data := map[string]interface{}{"nodes": []interface{}{map[string]interface{}{"node_id": "n", "detail": tc.detail}}, "edges": []interface{}{map[string]interface{}{"edge_id": "e", "detail": tc.detail}}}
			if err := renderGraphDataPretty(&out, data); err != nil {
				t.Fatal(err)
			}
			if strings.Count(out.String(), tc.want) != 2 || strings.Contains(out.String(), "OLD_BODY") {
				t.Fatalf("unexpected rendering: %s", out.String())
			}
		})
	}
}

func TestGraphProtocolPrettySourcesAreComplete(t *testing.T) {
	nodeSources := []map[string]interface{}{
		graphProtocolSource("9223372036854775808", "document"),
		graphProtocolSource("node-second-source", "message"),
	}
	edgeSources := []map[string]interface{}{graphProtocolSource("9223372036854775809", "message")}
	for _, withMarkdown := range []bool{false, true} {
		name := "source_only"
		if withMarkdown {
			name = "markdown_and_source"
		}
		t.Run(name, func(t *testing.T) {
			nodeDetail := map[string]interface{}{"source": []interface{}{nodeSources[0], nodeSources[1]}}
			edgeDetail := map[string]interface{}{"source": []interface{}{edgeSources[0]}}
			if withMarkdown {
				nodeDetail["markdown"] = "node Markdown"
				edgeDetail["markdown"] = "edge Markdown"
			}
			var out bytes.Buffer
			data := map[string]interface{}{
				"nodes": []interface{}{map[string]interface{}{"node_id": "n", "detail": nodeDetail}},
				"edges": []interface{}{map[string]interface{}{"edge_id": "e", "detail": edgeDetail}},
			}
			if err := renderGraphDataPretty(&out, data); err != nil {
				t.Fatal(err)
			}
			nodes, edges, ok := strings.Cut(out.String(), "Edges (1)\n")
			if !ok {
				t.Fatalf("missing edge section: %s", out.String())
			}
			assertGraphPrettySources(t, nodes, nodeSources)
			assertGraphPrettySources(t, edges, edgeSources)
			for _, section := range []string{nodes, edges} {
				if withMarkdown {
					if !strings.Contains(section, "detail.markdown:") || strings.Contains(section, "detail: source_only") {
						t.Fatalf("unexpected Markdown detail state: %s", section)
					}
				} else if !strings.Contains(section, "detail: source_only") || strings.Contains(section, "detail.markdown:") {
					t.Fatalf("unexpected source-only detail state: %s", section)
				}
			}
		})
	}
}

func assertGraphPrettySources(t *testing.T, section string, want []map[string]interface{}) {
	t.Helper()
	const prefix = "  detail.source: "
	var raw string
	count := 0
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, prefix) {
			count++
			raw = strings.TrimPrefix(line, prefix)
		}
	}
	if count != 1 {
		t.Fatalf("source must appear exactly once in each object section, got %d: %s", count, section)
	}
	var actual []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &actual); err != nil {
		t.Fatalf("source must render as JSON: %v", err)
	}
	// Decode into interface values so numeric coercion also fails this comparison.
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("source IDs/types changed, truncated, or lost: got %#v, want %#v", actual, want)
	}
}

func TestGraphProtocolDetailMergeDoesNotAlias(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		left := map[string]interface{}{"source": []interface{}{graphProtocolSource("a", "doc")}, "future": false}
		right := map[string]interface{}{"source": []interface{}{graphProtocolSource("a", "doc"), graphProtocolSource("b", "doc")}, "markdown": "new"}
		if reverse {
			left, right = right, left
		}
		beforeLeft, _ := json.Marshal(left)
		beforeRight, _ := json.Marshal(right)
		value, changed := mergeGraphDetail(left, right)
		merged := value.(map[string]interface{})
		if !changed || merged["markdown"] != "new" || merged["future"] != false || graphDetailSourceCount(merged) != 2 {
			t.Fatalf("lossy merge: %#v", merged)
		}
		if _, changed := mergeGraphDetail(merged, right); changed {
			t.Fatal("repeated Detail is not new evidence")
		}
		merged["source"].([]interface{})[0].(map[string]interface{})["entity_id"] = "changed"
		afterLeft, _ := json.Marshal(left)
		afterRight, _ := json.Marshal(right)
		if !bytes.Equal(beforeLeft, afterLeft) || !bytes.Equal(beforeRight, afterRight) {
			t.Fatal("merge mutated input Detail")
		}
	}
}

func graphProtocolSource(id, kind string) map[string]interface{} {
	return map[string]interface{}{"entity_id": id, "entity_type": kind}
}

func assertGraphProtocolDefaultRequest(t *testing.T, body string) {
	t.Helper()
	if gjson.Get(body, "options").Exists() || gjson.Get(body, "params").Exists() {
		t.Fatalf("Graph requests must not override backend version or detail format: %s", body)
	}
}
