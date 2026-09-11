// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/httpmock"
)

func TestGraphOneHopMetadata(t *testing.T) {
	if MemoryOneHop.Service != "memory" || MemoryOneHop.Command != "+one-hop" {
		t.Fatalf("shortcut = %s %s, want memory +one-hop", MemoryOneHop.Service, MemoryOneHop.Command)
	}
	if want := []string{"user"}; !reflect.DeepEqual(MemoryOneHop.AuthTypes, want) {
		t.Fatalf("AuthTypes = %#v, want %#v", MemoryOneHop.AuthTypes, want)
	}
	if flag := mountedGraphOneHopCommand(t).Flags().Lookup("user-id"); flag != nil {
		t.Fatal("--user-id must not be exposed")
	}
}

func TestParseGraphOneHopSpec(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	spec, err := parseGraphOneHopSpec(graphOneHopInput{
		Roots:         []string{" 2:doc:with:colon ", "3:meeting_123"},
		LookbackDays:  2,
		NodeTypes:     []int{3, 2, 3},
		RelationTypes: []string{" meeting_discusses_doc ", "meeting_discusses_doc"},
		DetailFormat:  " markdown ",
		Hop:           2,
		HopSet:        true,
		Scene:         " graphcli ",
		Format:        "json",
	}, now, "generated-trace")
	if err != nil {
		t.Fatalf("parseGraphOneHopSpec: %v", err)
	}
	if spec.StartTimeSec != now.Unix()-2*graphOneHopDaySec || spec.EndTimeSec != now.Unix() {
		t.Fatalf("time range = [%d,%d), want [%d,%d)", spec.StartTimeSec, spec.EndTimeSec, now.Unix()-2*graphOneHopDaySec, now.Unix())
	}
	if got := spec.Roots[0]; got.NodeType != 2 || got.RootID != "doc:with:colon" {
		t.Fatalf("first root = %#v", got)
	}
	if !reflect.DeepEqual(spec.NodeTypes, []int64{3, 2}) {
		t.Fatalf("node types = %#v, want [3 2]", spec.NodeTypes)
	}
	if !reflect.DeepEqual(spec.RelationTypes, []string{"meeting_discusses_doc"}) {
		t.Fatalf("relation types = %#v", spec.RelationTypes)
	}
	if spec.TraceID != "generated-trace" || spec.DetailFormat != "markdown" || spec.Scene != "graphcli" || spec.Hop != 2 {
		t.Fatalf("spec = %#v", spec)
	}
}

func TestParseGraphOneHopSpecDefaultsAndValidation(t *testing.T) {
	valid := graphOneHopInput{
		Roots:        []string{"2:doc_123"},
		LookbackDays: 7,
		DetailFormat: "markdown",
		Hop:          1,
		HopSet:       true,
		Scene:        "graphcli",
		Format:       "json",
	}
	spec, err := parseGraphOneHopSpec(valid, time.Unix(1_800_000_000, 0), "trace")
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if len(spec.NodeTypes) != 0 {
		t.Fatalf("default node types = %#v, want no node type filter", spec.NodeTypes)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*graphOneHopInput)
		param  string
	}{
		{name: "missing root", mutate: func(in *graphOneHopInput) { in.Roots = nil }, param: "--root"},
		{name: "malformed root", mutate: func(in *graphOneHopInput) { in.Roots = []string{"2"} }, param: "--root"},
		{name: "user root", mutate: func(in *graphOneHopInput) { in.Roots = []string{"4:user_123"} }, param: "--root"},
		{name: "unsupported calendar root", mutate: func(in *graphOneHopInput) { in.Roots = []string{"5:calendar_123"} }, param: "--root"},
		{name: "nonpositive lookback", mutate: func(in *graphOneHopInput) { in.LookbackDays = 0 }, param: "--lookback-days"},
		{name: "missing hop", mutate: func(in *graphOneHopInput) { in.HopSet = false }, param: "--hop"},
		{name: "hop too high", mutate: func(in *graphOneHopInput) { in.Hop = 6 }, param: "--hop"},
		{name: "user node filter", mutate: func(in *graphOneHopInput) { in.NodeTypes = []int{4} }, param: "--node-type"},
		{name: "unsupported calendar filter", mutate: func(in *graphOneHopInput) { in.NodeTypes = []int{5} }, param: "--node-type"},
		{name: "empty relation", mutate: func(in *graphOneHopInput) { in.RelationTypes = []string{" "} }, param: "--relation-type"},
		{name: "bad detail format", mutate: func(in *graphOneHopInput) { in.DetailFormat = "xml" }, param: "--detail-format"},
		{name: "bad scene", mutate: func(in *graphOneHopInput) { in.Scene = "other" }, param: "--scene"},
		{name: "bad output format", mutate: func(in *graphOneHopInput) { in.Format = "ndjson" }, param: "--format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			input.Roots = append([]string(nil), valid.Roots...)
			tc.mutate(&input)
			_, err := parseGraphOneHopSpec(input, time.Unix(1_800_000_000, 0), "trace")
			var validation *errs.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T %v, want validation", err, err)
			}
			if validation.Param != tc.param {
				t.Fatalf("param = %q, want %q", validation.Param, tc.param)
			}
		})
	}
}

func TestGraphOneHopDryRunShape(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
	err := runGraphOneHopShortcut(t, []string{
		"+one-hop",
		"--root", "2:doc_123",
		"--root", "3:meeting_123",
		"--lookback-days", "2",
		"--node-type", "2",
		"--node-type", "3",
		"--relation-type", "meeting_discusses_doc",
		"--detail-format", "json",
		"--hop", "2",
		"--trace-id", "trace-123",
		"--scene", "graphcli",
		"--dry-run",
		"--as", "user",
	}, f, stdout)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if gjson.Get(stdout.String(), "api.#").Int() != 1 {
		t.Fatalf("dry-run must plan exactly one API call: %s", stdout.String())
	}
	if got := gjson.Get(stdout.String(), "api.0.method").String(); got != http.MethodPost {
		t.Fatalf("method = %q", got)
	}
	if got := gjson.Get(stdout.String(), "api.0.url").String(); got != graphOneHopPath {
		t.Fatalf("url = %q, want %q", got, graphOneHopPath)
	}
	if got := gjson.Get(stdout.String(), "api.0.body.user_id").String(); got != "ou_graph_test_user" {
		t.Fatalf("user_id = %q", got)
	}
	if got := gjson.Get(stdout.String(), "api.0.body.roots.#").Int(); got != 2 {
		t.Fatalf("roots count = %d", got)
	}
	if got := gjson.Get(stdout.String(), "api.0.body.filters.node_types.#").Int(); got != 2 {
		t.Fatalf("node type count = %d", got)
	}
	if got := gjson.Get(stdout.String(), "api.0.body.params.detailFormat").String(); got != "json" {
		t.Fatalf("detailFormat = %q", got)
	}
	if got := len(gjson.Get(stdout.String(), "api.0.body.params").Map()); got != 1 {
		t.Fatalf("params count = %d, want 1: %s", got, stdout.String())
	}
	start := gjson.Get(stdout.String(), "api.0.body.time_range.start_time_sec").Int()
	end := gjson.Get(stdout.String(), "api.0.body.time_range.end_time_sec").Int()
	if end-start != 2*graphOneHopDaySec {
		t.Fatalf("time range = [%d,%d), want two days", start, end)
	}
}

func TestGraphOneHopExecuteFiltersUsersAndAddsProvenance(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, memoryTestConfig(t))
	stub := &httpmock.Stub{
		Method:  http.MethodPost,
		URL:     graphOneHopPath,
		Headers: http.Header{"Content-Type": []string{"application/json"}, "X-Tt-Logid": []string{"log-one-hop-123"}},
		Body: map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"nodes": []interface{}{
						map[string]interface{}{"node_id": "doc_day:doc_123:2026-09-11", "node_type": 2, "source_id": "doc_123", "root_id": "doc_123", "event_time_sec": 1, "detail": map[string]interface{}{"content": "root"}},
						map[string]interface{}{"node_id": "meeting:meeting_123", "node_type": 3, "source_id": "meeting_123", "root_id": "meeting_123", "event_time_sec": 2, "detail": map[string]interface{}{"content": "meet"}},
						map[string]interface{}{"node_id": "calendar:calendar_123", "node_type": 5, "source_id": "calendar_123", "event_time_sec": 3},
						map[string]interface{}{"node_id": "user:user_123", "node_type": 4, "source_id": "user_123", "root_id": "user_123", "event_time_sec": 4},
					},
					"edges": []interface{}{
						map[string]interface{}{"edge_id": "edge-doc-meeting", "relation_type": "meeting_discusses_doc", "from_node_id": "doc_day:doc_123:2026-09-11", "to_node_id": "meeting:meeting_123", "detail": map[string]interface{}{"content": "edge"}},
						map[string]interface{}{"edge_id": "edge-user-doc", "relation_type": "edit_doc", "from_node_id": "user:user_123", "to_node_id": "doc_day:doc_123:2026-09-11"},
					},
				},
			},
		},
	}
	reg.Register(stub)

	err := runGraphOneHopShortcut(t, []string{
		"+one-hop", "--root", "2:doc_123", "--hop", "1", "--trace-id", "trace-one-hop", "--as", "user", "--format", "json",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := stub.CapturedHeaders.Get("x-tt-env"); got != defaultMemoryTTEnv {
		t.Fatalf("x-tt-env = %q, want %q", got, defaultMemoryTTEnv)
	}
	var request map[string]interface{}
	if err := json.Unmarshal(stub.CapturedBody, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if _, ok := request["filters"]; ok {
		t.Fatalf("filters must be omitted when no filter flag is set: %#v", request["filters"])
	}
	params, _ := request["params"].(map[string]interface{})
	if !reflect.DeepEqual(params, map[string]interface{}{"detailFormat": "markdown"}) {
		t.Fatalf("params = %#v, want only detailFormat", params)
	}

	got := stdout.String()
	if strings.Contains(got, "user:user_123") {
		t.Fatalf("USER data leaked into output: %s", got)
	}
	if gjson.Get(got, "data.nodes.#").Int() != 3 || gjson.Get(got, "data.edges.#").Int() != 1 {
		t.Fatalf("unexpected filtered data: %s", got)
	}
	if !gjson.Get(got, "data.nodes.0.expandable").Bool() || gjson.Get(got, "data.nodes.2.expandable").Bool() {
		t.Fatalf("unexpected expandable flags: %s", got)
	}
	if gjson.Get(got, "data.nodes.0.expanded_from.0.kind").String() != "timeline" {
		t.Fatalf("root provenance missing: %s", got)
	}
	if gjson.Get(got, "data.nodes.1.expanded_from.0.edge_id").String() != "edge-doc-meeting" {
		t.Fatalf("neighbour provenance missing: %s", got)
	}
	if gjson.Get(got, "data.edges.0.expanded_from.0.root.root_id").String() != "doc_123" {
		t.Fatalf("edge provenance missing: %s", got)
	}
	for path, want := range map[string]int64{
		"meta.root_count":               1,
		"meta.node_count":               3,
		"meta.edge_count":               1,
		"meta.filtered_user_node_count": 1,
		"meta.filtered_user_edge_count": 1,
		"meta.detail_bytes":             12,
		"meta.hop":                      1,
	} {
		if value := gjson.Get(got, path).Int(); value != want {
			t.Fatalf("%s = %d, want %d: %s", path, value, want, got)
		}
	}
	if gjson.Get(got, "meta.trace_id").String() != "trace-one-hop" || gjson.Get(got, "meta.log_id").String() != "log-one-hop-123" {
		t.Fatalf("trace/log metadata missing: %s", got)
	}
	if !gjson.Get(got, "meta.took_ms").Exists() {
		t.Fatalf("took_ms missing: %s", got)
	}
}

func TestNormalizeGraphOneHopResponsePreservesMultipleRootOrigins(t *testing.T) {
	data := map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{"node_id": "doc-node", "node_type": int64(2), "source_id": "doc", "root_id": "doc"},
			map[string]interface{}{"node_id": "meeting-node", "node_type": int64(3), "source_id": "meeting", "root_id": "meeting"},
			map[string]interface{}{"node_id": "im-node", "node_type": int64(1), "source_id": "im", "root_id": "im"},
		},
		"edges": []interface{}{
			map[string]interface{}{"edge_id": "doc-im", "relation_type": "im_doc_share", "from_node_id": "doc-node", "to_node_id": "im-node"},
			map[string]interface{}{"edge_id": "meeting-im", "relation_type": "meeting_chat", "from_node_id": "meeting-node", "to_node_id": "im-node"},
		},
	}
	out, _, err := normalizeGraphOneHopResponse(data, graphOneHopSpec{
		Roots:   []graphOneHopRoot{{NodeType: 2, RootID: "doc"}, {NodeType: 3, RootID: "meeting"}},
		Hop:     2,
		TraceID: "trace",
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	nodes := out["nodes"].([]interface{})
	imNode := nodes[2].(map[string]interface{})
	origins := imNode["expanded_from"].([]graphOneHopExpansion)
	if len(origins) != 2 || origins[0].Root.RootID != "doc" || origins[1].Root.RootID != "meeting" {
		t.Fatalf("origins = %#v, want doc and meeting", origins)
	}
}

func TestGraphOneHopRejectsMalformedResponse(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, memoryTestConfig(t))
	reg.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphOneHopPath,
		Body:   map[string]interface{}{"code": 0, "msg": "ok", "data": map[string]interface{}{"data": map[string]interface{}{"nodes": []interface{}{}}}},
	})
	err := runGraphOneHopShortcut(t, []string{
		"+one-hop", "--root", "2:doc_123", "--hop", "1", "--as", "user",
	}, f, stdout)
	var internal *errs.InternalError
	if !errors.As(err, &internal) || internal.Subtype != errs.SubtypeInvalidResponse {
		t.Fatalf("error = %T %v, want invalid_response", err, err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func mountedGraphOneHopCommand(t *testing.T) *cobra.Command {
	t.Helper()
	parent := &cobra.Command{Use: "memory"}
	MemoryOneHop.Mount(parent, &cmdutil.Factory{})
	return parent.Commands()[0]
}

func runGraphOneHopShortcut(t *testing.T, args []string, f *cmdutil.Factory, stdout *bytes.Buffer) error {
	t.Helper()
	parent := &cobra.Command{Use: "memory"}
	MemoryOneHop.Mount(parent, f)
	parent.SetArgs(args)
	parent.SilenceErrors = true
	parent.SilenceUsage = true
	if stdout != nil {
		stdout.Reset()
	}
	return parent.ExecuteContext(context.Background())
}
