// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/httpmock"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

func assertMemoryLaneHeader(t *testing.T, headers http.Header, want string) {
	t.Helper()
	var values []string
	present := false
	for key, headerValues := range headers {
		if strings.EqualFold(key, "x-tt-env") {
			present = true
			values = append(values, headerValues...)
		}
	}
	if want == "" {
		if present {
			t.Fatal("x-tt-env must be absent, not an empty header")
		}
		return
	}
	if !present || len(values) != 1 || values[0] != want {
		t.Fatalf("x-tt-env = %q, want exactly %q", values, want)
	}
}

func TestDirectMemoryCommandsDoNotDefaultToALane(t *testing.T) {
	commands := []struct {
		shortcut common.Shortcut
		args     []string
		method   string
		path     string
		data     map[string]interface{}
	}{
		{shortcut: MemoryList, args: []string{"+list"}, method: http.MethodGet, path: memoryAPIBasePath + "/list_memory", data: map[string]interface{}{"memories": []interface{}{}}},
		{shortcut: MemoryGet, args: []string{"+get", "--memory-key", "personal_memory_snapshot"}, method: http.MethodPost, path: memoryAPIBasePath + "/get_memory", data: map[string]interface{}{"memory_key": "personal_memory_snapshot", "status": "ready", "payload": "sample"}},
		{shortcut: MemoryGraphRange, args: []string{"+graph-range", "--start-time-sec", "100", "--end-time-sec", "86501"}},
		{shortcut: MemoryGraphOneHop, args: []string{"+graph-one-hop", "--root", "2:doc_123", "--hop", "1"}, method: http.MethodPost, path: graphOneHopPath, data: map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}}},
	}
	for _, lane := range []struct {
		name  string
		value string
		unset bool
		want  string
	}{
		{name: "unset", unset: true},
		{name: "empty"},
		{name: "whitespace", value: " \t "},
		{name: "explicit override", value: " custom_memory_lane ", want: "custom_memory_lane"},
	} {
		for _, command := range commands {
			t.Run(command.shortcut.Command+"/"+lane.name, func(t *testing.T) {
				t.Setenv(envMemoryTTEnv, lane.value)
				if lane.unset {
					if err := os.Unsetenv(envMemoryTTEnv); err != nil {
						t.Fatal(err)
					}
				}
				f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
				stubs := []*httpmock.Stub{{Method: command.method, URL: command.path, Body: map[string]interface{}{"code": 0, "data": command.data}}}
				if command.shortcut.Command == "+graph-range" {
					stubs = []*httpmock.Stub{
						graphQueryStubForWindow(100, 86500, graphNode("n1", "s1", 100), graphEdge("e1", "n1", "n1", 100)),
						graphQueryStubForWindow(86500, 86501, graphNode("n2", "s2", 86500), graphEdge("e2", "n2", "n2", 86500)),
					}
				}
				for _, stub := range stubs {
					registry.Register(stub)
				}
				args := append(append([]string(nil), command.args...), "--as", "user", "--format", "json")
				if err := runMemoryShortcut(t, command.shortcut, args, f, stdout); err != nil {
					t.Fatal(err)
				}
				registry.Verify(t)
				for _, stub := range stubs {
					assertMemoryLaneHeader(t, stub.CapturedHeaders, lane.want)
				}
			})
		}
	}
}

func TestGraphSearchRestrictsLaneToIdentityAndKnowledgeQA(t *testing.T) {
	for _, tc := range []struct {
		name, env, want string
		unset           bool
	}{
		{name: "unset", unset: true, want: graphSearchDefaultFaaSTTEnv},
		{name: "empty", want: graphSearchDefaultFaaSTTEnv},
		{name: "whitespace", env: " \t ", want: graphSearchDefaultFaaSTTEnv},
		{name: "override", env: " custom_search_lane ", want: "custom_search_lane"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envMemoryTTEnv, tc.env)
			if tc.unset {
				if err := os.Unsetenv(envMemoryTTEnv); err != nil {
					t.Fatal(err)
				}
			}
			f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
			userInfo := &httpmock.Stub{Method: http.MethodGet, URL: graphSearchUserInfoPath, Body: map[string]interface{}{"code": 0, "data": map[string]interface{}{"open_id": "ou_graph_test_user"}}}
			identity := &httpmock.Stub{Method: http.MethodPost, URL: graphSearchIdentityURL, Body: map[string]interface{}{"out_id_to_in_id_map": map[string]interface{}{"ou_graph_test_user": graphSearchTestInternalUID}, "BaseResp": map[string]interface{}{"StatusCode": 0}}}
			anchor := time.Now().Unix() - 60
			date := time.Unix(anchor, 0).In(graphSearchLocation).Format("2006-01-02")
			knowledge := &httpmock.Stub{Method: http.MethodPost, URL: graphSearchKnowledgeQAURL, Body: map[string]interface{}{"passages": []interface{}{graphSearchPassage("p1", graphSearchSourceDoc, "Project", "https://bytedance.larkoffice.com/docx/docA", 1, 0, anchor)}}}
			node := graphSearchNode("node-1", 2, "docA", date, anchor)
			hop := graphSearchOneHopStub("docA", []interface{}{node}, []interface{}{})
			query := &httpmock.Stub{Method: http.MethodPost, URL: graphQueryPath, Body: map[string]interface{}{"code": 0, "data": map[string]interface{}{"nodes": []interface{}{node}, "edges": []interface{}{}}}}
			for _, stub := range []*httpmock.Stub{userInfo, identity, knowledge, hop, query} {
				registry.Register(stub)
			}
			args := []string{"+graph-search", "--query", "project", "--graph-query-mode", "on", "--graph-query-lookback-days", "1", "--as", "user", "--format", "json"}
			if err := runGraphSearchShortcut(t, args, f, stdout); err != nil {
				t.Fatal(err)
			}
			registry.Verify(t)
			for _, stub := range []*httpmock.Stub{identity, knowledge} {
				assertMemoryLaneHeader(t, stub.CapturedHeaders, tc.want)
			}
			for _, stub := range []*httpmock.Stub{userInfo, hop, query} {
				assertMemoryLaneHeader(t, stub.CapturedHeaders, "")
			}
		})
	}
}
