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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/credential"
	"code.byted.org/lark_search/larksuite-cli/internal/httpmock"
)

const graphSearchTestInternalUID int64 = 7000000000000000001

func TestGraphSearchMetadata(t *testing.T) {
	if MemoryGraphSearch.Service != "memory" || MemoryGraphSearch.Command != "+graph-search" {
		t.Fatalf("shortcut = %s %s, want memory +graph-search", MemoryGraphSearch.Service, MemoryGraphSearch.Command)
	}
	if want := []string{"user"}; !reflect.DeepEqual(MemoryGraphSearch.AuthTypes, want) {
		t.Fatalf("AuthTypes = %#v, want %#v", MemoryGraphSearch.AuthTypes, want)
	}
	if want := []string{"wiki:node:retrieve"}; !reflect.DeepEqual(MemoryGraphSearch.ConditionalUserScopes, want) {
		t.Fatalf("ConditionalUserScopes = %#v, want %#v", MemoryGraphSearch.ConditionalUserScopes, want)
	}
	if flag := mountedGraphSearchCommand(t).Flags().Lookup("user-id"); flag != nil {
		t.Fatal("--user-id must not be exposed")
	}
	if flag := mountedGraphSearchCommand(t).Flags().Lookup("max-hops"); flag != nil {
		t.Fatal("--max-hops must not be exposed; graph-search always performs the initial OneHop only")
	}
	if got := mountedGraphSearchCommand(t).Flags().Lookup("graph-query-mode").DefValue; got != "on" {
		t.Fatalf("--graph-query-mode default = %q, want every query to attempt Graph recall", got)
	}
}

func TestParseGraphSearchSpec(t *testing.T) {
	spec, err := parseGraphSearchSpec(graphSearchInput{
		Query:        " query ",
		Concurrency:  8,
		DetailFormat: " markdown ",
		Format:       "json",
	}, "trace")
	if err != nil {
		t.Fatalf("parseGraphSearchSpec: %v", err)
	}
	if spec.Query != "query" || spec.InternalUserID != 0 || spec.GraphUserID != "" || spec.TraceID != "trace" {
		t.Fatalf("spec = %#v", spec)
	}
	if spec.MemoryTTEnv != graphSearchDefaultTTEnv {
		t.Fatalf("MemoryTTEnv = %q, want %q", spec.MemoryTTEnv, graphSearchDefaultTTEnv)
	}
	if spec.GraphQueryMode != graphSearchGraphQueryModeOn || spec.GraphQueryLookbackDays != graphSearchGraphQueryDefaultDays {
		t.Fatalf("GraphQuery defaults = mode:%q days:%d", spec.GraphQueryMode, spec.GraphQueryLookbackDays)
	}
	customInput := graphSearchInput{Query: "q", Concurrency: 8, DetailFormat: "markdown", Format: "json", MemoryTTEnv: "custom_graph_lane"}
	customSpec, err := parseGraphSearchSpec(customInput, "trace")
	if err != nil || customSpec.MemoryTTEnv != "custom_graph_lane" {
		t.Fatalf("custom MemoryTTEnv spec = %#v, err = %v", customSpec, err)
	}

	valid := graphSearchInput{Query: "q", Concurrency: 8, DetailFormat: "markdown", Format: "json"}
	for _, tc := range []struct {
		name  string
		input graphSearchInput
		param string
	}{
		{name: "empty query", input: graphSearchInput{Concurrency: 8, DetailFormat: "markdown", Format: "json"}, param: "--query"},
		{name: "bad concurrency", input: graphSearchInput{Query: "q", Concurrency: 0, DetailFormat: "markdown", Format: "json"}, param: "--concurrency"},
		{name: "bad detail", input: graphSearchInput{Query: "q", Concurrency: 8, DetailFormat: "xml", Format: "json"}, param: "--detail-format"},
		{name: "bad format", input: graphSearchInput{Query: "q", Concurrency: 8, DetailFormat: "json", Format: "ndjson"}, param: "--format"},
		{name: "bad graph query mode", input: graphSearchInput{Query: "q", Concurrency: 8, DetailFormat: "json", Format: "json", GraphQueryMode: "bad", GraphQueryModeSet: true}, param: "--graph-query-mode"},
		{name: "bad graph query lookback", input: graphSearchInput{Query: "q", Concurrency: 8, DetailFormat: "json", Format: "json", GraphQueryLookbackSet: true}, param: "--graph-query-lookback-days"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseGraphSearchSpec(tc.input, "trace")
			var validation *errs.ValidationError
			if !errors.As(err, &validation) || validation.Param != tc.param {
				t.Fatalf("error = %T %v, want validation param %s", err, err, tc.param)
			}
		})
	}
	invalidLane := valid
	invalidLane.MemoryTTEnv = "bad\nlane"
	if _, err := parseGraphSearchSpec(invalidLane, "trace"); err == nil {
		t.Fatal("parseGraphSearchSpec accepted a multi-line x-tt-env value")
	}
}

func TestGraphSearchGraphQueryAutoDecisionAndPlan(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  bool
	}{
		{query: "Memory项目最近是什么进展", want: true},
		{query: "过去3天有什么问题", want: true},
		{query: "status in the last 2 days", want: true},
		{query: "2026-09-10项目进展", want: true},
		{query: "什么是知识问答", want: false},
	} {
		if got := graphSearchHasTimeIntent(tc.query); got != tc.want {
			t.Errorf("graphSearchHasTimeIntent(%q) = %t, want %t", tc.query, got, tc.want)
		}
	}
	now := time.Unix(1_800_000_000, 0)
	plan := planGraphSearchGraphQuery(graphSearchSpec{
		Query: "最近项目进展", GraphQueryMode: graphSearchGraphQueryModeAuto, GraphQueryLookbackDays: 3,
	}, now)
	if !plan.Enabled || plan.TriggerReason != "auto_time_intent" || len(plan.Windows) != 3 ||
		plan.EndTimeSec != now.Unix() || plan.StartTimeSec != now.Unix()-3*graphQueryWindowSec {
		t.Fatalf("time-intent plan = %#v", plan)
	}
	disabled := planGraphSearchGraphQuery(graphSearchSpec{
		Query: "什么是知识问答", GraphQueryMode: graphSearchGraphQueryModeAuto, GraphQueryLookbackDays: 7,
	}, now)
	if disabled.Enabled || disabled.TriggerReason != "auto_no_time_intent" || len(disabled.Windows) != 0 {
		t.Fatalf("non-time plan = %#v", disabled)
	}
	if enabled, reason := graphSearchGraphQueryDecision(graphSearchGraphQueryModeOn, "定义问题"); !enabled || reason != "forced_on" {
		t.Fatalf("forced-on decision = enabled:%t reason:%q", enabled, reason)
	}
	if enabled, reason := graphSearchGraphQueryDecision(graphSearchGraphQueryModeOff, "最近进展"); enabled || reason != "forced_off" {
		t.Fatalf("forced-off decision = enabled:%t reason:%q", enabled, reason)
	}
}

func TestGraphSearchGraphQueryRootsUseWindowAnchorWhenEventTimeMissing(t *testing.T) {
	roots := graphSearchGraphQueryRoots([]map[string]interface{}{{
		"node_id":    "doc-day-a",
		"node_type":  int64(2),
		"root_id":    "docA",
		"graph_date": "2026-09-10",
		"retrieved_via": []graphSearchRetrievalSource{{
			Kind:         graphSearchRetrievalGraphQuery,
			StartTimeSec: time.Date(2026, 9, 10, 0, 0, 0, 0, graphSearchLocation).Unix(),
		}},
	}})
	if len(roots) != 1 || len(roots[0].SearchOrigins) != 1 {
		t.Fatalf("roots = %#v", roots)
	}
	origin := roots[0].SearchOrigins[0]
	if origin.CandidateSource != "graph_query.nodes" ||
		origin.AnchorTimeSource != "graph_query.window.start_time_sec" ||
		origin.AnchorTimeSec == 0 {
		t.Fatalf("origin = %#v", origin)
	}
}

func TestGraphSearchInfersExactTimeRanges(t *testing.T) {
	now := time.Date(2026, 9, 12, 15, 30, 0, 0, graphSearchLocation)
	for _, tc := range []struct {
		name       string
		query      string
		wantStart  time.Time
		wantEnd    time.Time
		wantSource string
		wantOK     bool
	}{
		{name: "today", query: "今天做了什么", wantStart: time.Date(2026, 9, 12, 0, 0, 0, 0, graphSearchLocation), wantEnd: now, wantSource: "query_today", wantOK: true},
		{name: "yesterday", query: "昨天的工作", wantStart: time.Date(2026, 9, 11, 0, 0, 0, 0, graphSearchLocation), wantEnd: time.Date(2026, 9, 12, 0, 0, 0, 0, graphSearchLocation), wantSource: "query_yesterday", wantOK: true},
		{name: "this week", query: "本周的工作", wantStart: time.Date(2026, 9, 7, 0, 0, 0, 0, graphSearchLocation), wantEnd: now, wantSource: "query_this_week", wantOK: true},
		{name: "last week", query: "上周的工作", wantStart: time.Date(2026, 8, 31, 0, 0, 0, 0, graphSearchLocation), wantEnd: time.Date(2026, 9, 7, 0, 0, 0, 0, graphSearchLocation), wantSource: "query_last_week", wantOK: true},
		{name: "rolling days", query: "过去3天的进展", wantStart: now.Add(-72 * time.Hour), wantEnd: now, wantSource: "query_rolling_duration", wantOK: true},
		{name: "explicit date", query: "2026-09-10有哪些工作", wantStart: time.Date(2026, 9, 10, 0, 0, 0, 0, graphSearchLocation), wantEnd: time.Date(2026, 9, 11, 0, 0, 0, 0, graphSearchLocation), wantSource: "query_explicit_date", wantOK: true},
		{name: "explicit date range", query: "2026-09-08到2026-09-10的工作", wantStart: time.Date(2026, 9, 8, 0, 0, 0, 0, graphSearchLocation), wantEnd: time.Date(2026, 9, 11, 0, 0, 0, 0, graphSearchLocation), wantSource: "query_explicit_date_range", wantOK: true},
		{name: "over limit", query: "过去8天的工作", wantSource: "query_duration_exceeds_limit", wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end, source, ok := inferGraphSearchTimeRange(tc.query, now, 7)
			if ok != tc.wantOK || source != tc.wantSource || ok && (!start.Equal(tc.wantStart) || !end.Equal(tc.wantEnd)) {
				t.Fatalf("range = [%s,%s) source=%q ok=%t, want [%s,%s) source=%q ok=%t", start, end, source, ok, tc.wantStart, tc.wantEnd, tc.wantSource, tc.wantOK)
			}
		})
	}
	plan := planGraphSearchGraphQuery(graphSearchSpec{
		Query: "本周的工作", GraphQueryMode: graphSearchGraphQueryModeAuto, GraphQueryLookbackDays: 7,
	}, now)
	if !plan.Enabled || plan.RangeSource != "query_this_week" || plan.LookbackDays != 6 || len(plan.Windows) != 6 {
		t.Fatalf("this-week plan = %#v", plan)
	}
	overLimit := planGraphSearchGraphQuery(graphSearchSpec{
		Query: "过去8天的工作", GraphQueryMode: graphSearchGraphQueryModeAuto, GraphQueryLookbackDays: 7,
	}, now)
	if overLimit.Enabled || overLimit.TriggerReason != "time_range_unsupported" || overLimit.RangeSource != "query_duration_exceeds_limit" {
		t.Fatalf("over-limit plan = %#v", overLimit)
	}
}

func TestGraphSearchUATPreflightFailsBeforeBusinessRequests(t *testing.T) {
	config := memoryTestConfig(t)
	f, stdout, _, _ := cmdutil.TestFactory(t, config)
	httpCalls := 0
	originalHTTPClient := f.HttpClient
	f.HttpClient = func() (*http.Client, error) {
		httpCalls++
		return originalHTTPClient()
	}
	f.Credential = credential.NewCredentialProvider(
		nil,
		&graphSearchStaticAccountResolver{account: credential.AccountFromCliConfig(config)},
		graphSearchFailingTokenResolver{},
		f.HttpClient,
	)

	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "本周工作", "--as", "user", "--format", "json",
	}, f, stdout)
	problem, ok := errs.ProblemOf(err)
	if !ok || problem.Category != errs.CategoryAuthentication || problem.Subtype != errs.SubtypeTokenInvalid ||
		problem.Message != "Graph Search user access token preflight failed" {
		t.Fatalf("preflight error = %#v, ok=%t, raw=%v", problem, ok, err)
	}
	if httpCalls != 0 || stdout.Len() != 0 {
		t.Fatalf("preflight performed business I/O: httpCalls=%d stdout=%q", httpCalls, stdout.String())
	}
}

func TestGraphSearchDerivesBothUIDsFromVerifiedUAT(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	const (
		openID        = "ou_uat_resolved_user"
		documentToken = "docIdentity"
	)
	internalUID := graphSearchTestInternalUID
	userInfoStub := &httpmock.Stub{
		Method: http.MethodGet,
		URL:    graphSearchUserInfoPath,
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{"open_id": openID},
		},
	}
	registry.Register(userInfoStub)
	identityStub := &httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchIdentityURL,
		Body: map[string]interface{}{
			"out_id_to_in_id_map": map[string]interface{}{openID: internalUID},
			"BaseResp":            map[string]interface{}{"StatusCode": 0},
		},
	}
	registry.Register(identityStub)
	anchor := time.Date(2026, 9, 13, 12, 0, 0, 0, graphSearchLocation).Unix()
	knowledgeStub := &httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Body: map[string]interface{}{
			"passages": []interface{}{
				graphSearchPassage("p-doc", graphSearchSourceDoc, "Doc", "https://bytedance.larkoffice.com/docx/"+documentToken, 1, 0, anchor),
			},
			"BaseResp": map[string]interface{}{"StatusCode": 0},
		},
	}
	registry.Register(knowledgeStub)
	oneHopStub := graphSearchOneHopStub(documentToken, []interface{}{
		graphSearchNode("doc-day-identity", graphSearchDocDayNodeType, documentToken, "2026-09-13", anchor),
	}, []interface{}{})
	registry.Register(oneHopStub)

	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "身份转换", "--as", "user", "--format", "json",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gjson.GetBytes(identityStub.CapturedBody, "out_ids.0").String() != openID ||
		gjson.GetBytes(identityStub.CapturedBody, "id_type").Int() != graphSearchOpenIDType ||
		identityStub.CapturedHeaders.Get("X-Tt-Env") != graphSearchDefaultTTEnv {
		t.Fatalf("identity request body=%s headers=%v", identityStub.CapturedBody, identityStub.CapturedHeaders)
	}
	if knowledgeStub.CapturedHeaders.Get("Rpc-Transit-USER-ID") != strconv.FormatInt(internalUID, 10) ||
		gjson.GetBytes(knowledgeStub.CapturedBody, "Head.Auth.UserID").Int() != internalUID {
		t.Fatalf("Knowledge QA did not use converted UID: body=%s headers=%v", knowledgeStub.CapturedBody, knowledgeStub.CapturedHeaders)
	}
	if gjson.GetBytes(oneHopStub.CapturedBody, "user_id").String() != openID {
		t.Fatalf("OneHop user_id = %q, want UAT-derived open_id", gjson.GetBytes(oneHopStub.CapturedBody, "user_id").String())
	}
}

func TestGraphSearchStopsWhenVerifiedOpenIDCannotBeConverted(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	registry.Register(&httpmock.Stub{
		Method: http.MethodGet,
		URL:    graphSearchUserInfoPath,
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{"open_id": "ou_unmapped_user"},
		},
	})
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchIdentityURL,
		Body: map[string]interface{}{
			"out_id_to_in_id_map": map[string]interface{}{},
			"BaseResp":            map[string]interface{}{"StatusCode": 0},
		},
	})

	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "身份转换", "--as", "user", "--format", "json",
	}, f, stdout)
	problem, ok := errs.ProblemOf(err)
	if !ok || problem.Category != errs.CategoryInternal || problem.Subtype != errs.SubtypeInvalidResponse {
		t.Fatalf("conversion error = %#v, ok=%t, raw=%v", problem, ok, err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no Knowledge QA result", stdout.String())
	}
}

type graphSearchStaticAccountResolver struct {
	account *credential.Account
}

func (r *graphSearchStaticAccountResolver) ResolveAccount(context.Context) (*credential.Account, error) {
	return r.account, nil
}

type graphSearchFailingTokenResolver struct{}

func (graphSearchFailingTokenResolver) ResolveToken(context.Context, credential.TokenSpec) (*credential.TokenResult, error) {
	return nil, errs.NewAuthenticationError(errs.SubtypeTokenInvalid, "expired test UAT")
}

func TestGraphSearchDryRunRedactsUIDAndDescribesDynamicSteps(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "什么是知识问答", "--concurrency", "4", "--dry-run", "--as", "user",
	}, f, stdout)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	got := stdout.String()
	if strings.Contains(got, strconv.FormatInt(graphSearchTestInternalUID, 10)) {
		t.Fatalf("dry-run leaked UID: %s", got)
	}
	if gjson.Get(got, "api.0.url").String() != graphSearchUserInfoPath || gjson.Get(got, "api.0.method").String() != http.MethodGet ||
		gjson.Get(got, "api.1.url").String() != graphSearchIdentityURL || gjson.Get(got, "api.1.method").String() != http.MethodPost ||
		gjson.Get(got, "api.2.url").String() != graphSearchKnowledgeQAURL || gjson.Get(got, "api.2.method").String() != http.MethodPost {
		t.Fatalf("unexpected dry-run request: %s", got)
	}
	if gjson.Get(got, "api.1.body.out_ids.0").String() != "<from user_info.open_id>" ||
		gjson.Get(got, "api.1.body.id_type").Int() != graphSearchOpenIDType ||
		gjson.Get(got, "api.2.body.query").String() != "什么是知识问答" {
		t.Fatalf("query missing from dry-run: %s", got)
	}
	if gjson.Get(got, "identity_conversion_headers.X-Tt-Env").String() != graphSearchDefaultTTEnv ||
		gjson.Get(got, "knowledge_qa_headers.Rpc-Transit-USER-ID").String() != "<from out_id_to_in_id_map>" {
		t.Fatalf("UID placeholder missing: %s", got)
	}
	if gjson.Get(got, "dynamic_steps.#").Int() != 5 {
		t.Fatalf("dynamic steps missing: %s", got)
	}
	if gjson.Get(got, "max_hops").Exists() {
		t.Fatalf("dry-run must not expose a max_hops setting: %s", got)
	}
	if gjson.Get(got, "one_hop_tt_env").String() != graphSearchDefaultTTEnv {
		t.Fatalf("Graph Search default lane missing: %s", got)
	}
}

func TestGraphSearchDryRunUsesMemoryLaneOverride(t *testing.T) {
	t.Setenv(envMemoryTTEnv, "custom_graph_lane")
	f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "query", "--dry-run", "--as", "user",
	}, f, stdout)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if got := gjson.Get(stdout.String(), "one_hop_tt_env").String(); got != "custom_graph_lane" {
		t.Fatalf("one_hop_tt_env = %q, want custom_graph_lane: %s", got, stdout.String())
	}
}

func TestGraphSearchExecutesInitialHopAndMergesOrigins(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	graphDate := "2026-09-10"
	anchor := time.Date(2026, 9, 10, 12, 0, 0, 0, graphSearchLocation).Unix()
	knowledgeStub := &httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Headers: http.Header{
			"Content-Type":          []string{"application/json"},
			"X-Bytefaas-Request-Id": []string{"faas-log"},
		},
		Body: map[string]interface{}{
			"passages": []interface{}{
				graphSearchPassage("p-doc-1", graphSearchSourceDoc, "Doc", "https://bytedance.larkoffice.com/docx/docA", 0.99, 0, anchor),
				graphSearchPassage("p-doc-2", graphSearchSourceDoc, "Doc duplicate", "https://bytedance.larkoffice.com/docx/docA", 0.95, 0, anchor),
				graphSearchPassage("p-wiki", graphSearchSourceWiki, "Wiki", "https://bytedance.larkoffice.com/wiki/wikiA", 0.9, 0, anchor),
				graphSearchPassage("p-wiki-duplicate", graphSearchSourceWiki, "Wiki duplicate", "https://bytedance.larkoffice.com/wiki/wikiA", 0.85, 0, anchor),
				graphSearchPassage("p-message", graphSearchSourceMessage, "Chat", "https://applink.feishu.cn/client/chat/open?chatId=123&position=7", 0.8, anchor, anchor),
				graphSearchMinutesPassage("p-minutes", "https://meetings.larkoffice.com/minutes/minuteA?t=0", anchor),
			},
			"passages_ignore_filter": []interface{}{
				map[string]interface{}{"id": "forbidden", "content": "forbidden unfiltered content"},
			},
			"BaseResp": map[string]interface{}{"StatusCode": 0, "StatusMessage": ""},
		},
	}
	registry.Register(knowledgeStub)
	wikiStub := &httpmock.Stub{
		Method: http.MethodGet,
		URL:    "/open-apis/wiki/v2/spaces/get_node",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{"node": map[string]interface{}{
				"node_token": "wikiA", "obj_token": "docB", "obj_type": "docx", "obj_edit_time": strconv.FormatInt(anchor, 10),
			}},
		},
	}
	registry.Register(wikiStub)
	hopOneStub := graphSearchOneHopStub("docA", []interface{}{
		graphSearchNode("doc-day-a", 2, "docA", graphDate, anchor),
		graphSearchNode("im-day-456", 1, "456", graphDate, anchor),
	}, []interface{}{
		graphSearchEdge("edge-a", "doc-day-a", "im-day-456"),
	})
	registry.Register(hopOneStub)

	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "什么是知识问答", "--concurrency", "4", "--graph-query-mode", "off", "--as", "user", "--format", "json",
	}, f, stdout, registry)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := stdout.String()
	if !gjson.Get(got, "ok").Bool() || !gjson.Get(got, "meta.complete").Bool() {
		t.Fatalf("unexpected result: %s", got)
	}
	if gjson.Get(got, "data.graph_query.called").Bool() || gjson.Get(got, "meta.graph_query_called").Bool() {
		t.Fatalf("non-time query unexpectedly called GraphQuery: %s", got)
	}
	for path, want := range map[string]int64{
		"data.search_candidates.#":              6,
		"data.roots.#":                          3,
		"data.nodes.#":                          2,
		"data.edges.#":                          1,
		"data.skipped_candidates.#":             1,
		"data.hops.#":                           1,
		"data.evidence.candidate_content_count": 6,
		"data.evidence.node_detail_count":       2,
		"data.evidence.edge_detail_count":       1,
		"meta.hops_executed":                    1,
		"meta.failed_batch_count":               0,
	} {
		if value := gjson.Get(got, path).Int(); value != want {
			t.Fatalf("%s = %d, want %d: %s", path, value, want, got)
		}
	}
	if reason := gjson.Get(got, "data.skipped_candidates.0.reason").String(); reason != "minutes_root_unsupported" {
		t.Fatalf("minutes reason = %q: %s", reason, got)
	}
	if !gjson.Get(got, "data.search_candidates.0.permission_filtered").Bool() ||
		gjson.Get(got, "data.search_candidates.0.content").String() != "Doc content" ||
		strings.Contains(got, "forbidden unfiltered content") {
		t.Fatalf("candidate permission/content contract failed: %s", got)
	}
	docRoot := graphSearchFindRoot(gjson.Get(got, "data.roots").Array(), 2, "docA")
	if !docRoot.Exists() || docRoot.Get("search_origins.#").Int() != 2 {
		t.Fatalf("doc origins were not merged: %s", got)
	}
	if docRoot.Get("search_origins.0.root_id_source").String() != "knowledge_qa.passages.url.object_token" ||
		docRoot.Get("search_origins.0.anchor_time_source").String() != "knowledge_qa.passages.extra.update_time" {
		t.Fatalf("root derivation evidence missing: %s", got)
	}
	wikiRoot := graphSearchFindRoot(gjson.Get(got, "data.roots").Array(), 2, "docB")
	if wikiRoot.Get("search_origins.0.root_id_source").String() != "wiki.get_node.obj_token" ||
		wikiRoot.Get("search_origins.0.anchor_time_source").String() != "wiki.get_node.obj_edit_time" ||
		wikiRoot.Get("search_origins.#").Int() != 2 {
		t.Fatalf("Wiki root derivation evidence missing: %s", got)
	}
	messageRoot := graphSearchFindRoot(gjson.Get(got, "data.roots").Array(), 1, "123")
	if messageRoot.Get("search_origins.0.root_id_source").String() != "knowledge_qa.passages.url.chatId" ||
		messageRoot.Get("search_origins.0.anchor_time_source").String() != "knowledge_qa.passages.extra.create_time" {
		t.Fatalf("message root derivation evidence missing: %s", got)
	}
	if gjson.Get(got, "data.hops.0.new_node_count").Int() != 2 ||
		gjson.Get(got, "data.hops.0.new_node_detail_count").Int() != 2 ||
		gjson.Get(got, "data.next_roots.#").Int() != 1 ||
		gjson.Get(got, "meta.stop_reason").String() != "agent_decision_required" {
		t.Fatalf("initial-hop continuation contract missing: %s", got)
	}
	if gjson.Get(got, "data.evidence.node_details.0.detail_path").String() != "nodes[0].detail" ||
		gjson.Get(got, "data.evidence.edge_details.0.detail_path").String() != "edges[0].detail" ||
		gjson.Get(got, "data.evidence.edge_groups.#").Int() != 1 {
		t.Fatalf("node/edge evidence index missing: %s", got)
	}
	if strings.Contains(got, strconv.FormatInt(graphSearchTestInternalUID, 10)) {
		t.Fatalf("output leaked UID: %s", got)
	}
	if knowledgeStub.CapturedHeaders.Get("Rpc-Transit-USER-ID") != strconv.FormatInt(graphSearchTestInternalUID, 10) ||
		knowledgeStub.CapturedHeaders.Get("Rpc-Persist-TENANT-ID") != "1" ||
		knowledgeStub.CapturedHeaders.Get("X-Tt-Env") != defaultMemoryTTEnv {
		t.Fatalf("Knowledge QA headers = %#v", knowledgeStub.CapturedHeaders)
	}
	var request map[string]interface{}
	if err := json.Unmarshal(knowledgeStub.CapturedBody, &request); err != nil {
		t.Fatalf("decode Knowledge QA request: %v", err)
	}
	if commonUID := gjson.GetBytes(knowledgeStub.CapturedBody, "Head.Auth.UserID").Int(); commonUID != graphSearchTestInternalUID {
		t.Fatalf("Knowledge QA body UID = %d", commonUID)
	}
	if gjson.GetBytes(hopOneStub.CapturedBody, "roots.#").Int() != 3 {
		t.Fatalf("Hop 1 roots were not grouped by date: %s", hopOneStub.CapturedBody)
	}
	if gotLane := hopOneStub.CapturedHeaders.Get("x-tt-env"); gotLane != graphSearchDefaultTTEnv {
		t.Fatalf("Graph Search x-tt-env = %q, want %q", gotLane, graphSearchDefaultTTEnv)
	}
	if len(wikiStub.CapturedBodies) != 1 {
		t.Fatalf("duplicate Wiki candidates caused %d get-node calls, want 1", len(wikiStub.CapturedBodies))
	}
	wantStart, wantEnd, _ := graphSearchDateWindow(graphDate)
	if gotStart := gjson.GetBytes(hopOneStub.CapturedBody, "time_range.start_time_sec").Int(); gotStart != wantStart {
		t.Fatalf("Hop 1 start = %d, want %d", gotStart, wantStart)
	}
	if gotEnd := gjson.GetBytes(hopOneStub.CapturedBody, "time_range.end_time_sec").Int(); gotEnd != wantEnd {
		t.Fatalf("Hop 1 end = %d, want %d", gotEnd, wantEnd)
	}
}

func TestGraphSearchSingleHopReturnsNextRootsForAgentDecision(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	anchor := time.Date(2026, 9, 10, 12, 0, 0, 0, graphSearchLocation).Unix()
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Body: map[string]interface{}{
			"passages": []interface{}{
				graphSearchPassage("p-doc", graphSearchSourceDoc, "Seed", "https://bytedance.larkoffice.com/docx/docA", 1, 0, anchor),
			},
			"BaseResp": map[string]interface{}{"StatusCode": 0},
		},
	})
	oneHopStub := graphSearchOneHopStub("docA", []interface{}{
		graphSearchNode("doc-day-a", 2, "docA", "2026-09-10", anchor),
		graphSearchNode("doc-day-b", 2, "docB", "2026-09-10", anchor),
	}, []interface{}{
		graphSearchEdge("edge-a-b", "doc-day-a", "doc-day-b"),
	})
	registry.Register(oneHopStub)

	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "项目决策", "--graph-query-mode", "off",
		"--as", "user", "--format", "json",
	}, f, stdout, registry)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := stdout.String()
	nextRoot := graphSearchFindRoot(gjson.Get(got, "data.next_roots").Array(), 2, "docB")
	if !nextRoot.Exists() || !gjson.Get(got, "data.continuation.requires_model_decision").Bool() ||
		gjson.Get(got, "data.continuation.next_root_count").Int() != 1 ||
		gjson.Get(got, "data.continuation.max_total_hops").Int() != 10 ||
		gjson.Get(got, "meta.stop_reason").String() != "agent_decision_required" {
		t.Fatalf("single-hop continuation contract missing: %s", got)
	}
	if len(oneHopStub.CapturedBodies) != 1 || graphSearchBodyHasRoot(oneHopStub.CapturedBody, "docB") {
		t.Fatalf("next root was auto-expanded: %s", oneHopStub.CapturedBody)
	}
}

func TestGraphSearchAutoGraphQueryRunsConcurrentlyAndMergesWithoutExpanding(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	anchor := time.Date(2026, 9, 10, 12, 0, 0, 0, graphSearchLocation).Unix()
	knowledgeStarted := make(chan struct{})
	graphQueryStarted := make(chan struct{})
	oneHopStarted := make(chan struct{})
	releaseGraphQuery := make(chan struct{})
	knowledgeStub := &httpmock.Stub{
		Method:  http.MethodPost,
		URL:     graphSearchKnowledgeQAURL,
		OnMatch: func(*http.Request) { close(knowledgeStarted) },
		Body: map[string]interface{}{
			"passages": []interface{}{
				graphSearchPassage("p-doc", graphSearchSourceDoc, "Recent project", "https://bytedance.larkoffice.com/docx/docA", 1, 0, anchor),
			},
			"BaseResp": map[string]interface{}{"StatusCode": 0},
		},
	}
	registry.Register(knowledgeStub)
	graphQueryStub := &httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphQueryPath,
		OnMatch: func(*http.Request) {
			close(graphQueryStarted)
			<-releaseGraphQuery
		},
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{"data": map[string]interface{}{
				"nodes": []interface{}{
					graphSearchNode("doc-day-a", 2, "docA", "2026-09-10", anchor),
					graphSearchNode("doc-day-g", 2, "docG", "2026-09-10", anchor),
					graphSearchNode("user:1", 4, "userRoot", "2026-09-10", anchor),
				},
				"edges": []interface{}{
					graphSearchEdge("edge-shared", "doc-day-a", "doc-day-a"),
					graphSearchEdge("edge-graph", "doc-day-a", "doc-day-g"),
					graphSearchEdge("edge-user", "doc-day-a", "user:1"),
				},
			}},
		},
	}
	registry.Register(graphQueryStub)
	oneHopStub := graphSearchOneHopStub("docA", []interface{}{
		graphSearchNode("doc-day-a", 2, "docA", "2026-09-10", anchor),
	}, []interface{}{
		graphSearchEdge("edge-shared", "doc-day-a", "doc-day-a"),
	})
	oneHopStub.OnMatch = func(*http.Request) { close(oneHopStarted) }
	registry.Register(oneHopStub)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runGraphSearchShortcut(t, []string{
			"+graph-search", "--query", "最近项目进展", "--graph-query-mode", "auto",
			"--graph-query-lookback-days", "1", "--as", "user", "--format", "json",
		}, f, stdout, registry)
	}()
	for name, started := range map[string]<-chan struct{}{
		"Knowledge QA": knowledgeStarted,
		"GraphQuery":   graphQueryStarted,
	} {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(releaseGraphQuery)
			t.Fatalf("%s did not start", name)
		}
	}
	select {
	case <-oneHopStarted:
	case <-time.After(5 * time.Second):
		close(releaseGraphQuery)
		t.Fatal("OneHop waited for GraphQuery instead of using the ready Knowledge QA roots")
	}
	close(releaseGraphQuery)
	if err := <-errCh; err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := stdout.String()
	if !gjson.Get(got, "meta.complete").Bool() || !gjson.Get(got, "data.graph_query.called").Bool() ||
		gjson.Get(got, "data.graph_query.window_count").Int() != 1 || gjson.Get(got, "data.graph_query.failed_windows.#").Int() != 0 {
		t.Fatalf("GraphQuery summary missing: %s", got)
	}
	if gjson.Get(got, "data.nodes.#").Int() != 2 || gjson.Get(got, "data.edges.#").Int() != 2 ||
		gjson.Get(got, "data.graph_query.filtered_node_count").Int() != 1 || gjson.Get(got, "data.graph_query.filtered_edge_count").Int() != 1 {
		t.Fatalf("GraphQuery merge/filter result unexpected: %s", got)
	}
	if gjson.Get(got, "data.evidence.corroborated_node_count").Int() != 1 ||
		gjson.Get(got, "data.evidence.graph_query_only_node_count").Int() != 1 ||
		gjson.Get(got, "data.evidence.corroborated_edge_count").Int() != 1 ||
		gjson.Get(got, "data.evidence.graph_query_only_edge_count").Int() != 1 {
		t.Fatalf("retrieval classification missing: %s", got)
	}
	nextRoot := graphSearchFindRoot(gjson.Get(got, "data.next_roots").Array(), 2, "docG")
	if !nextRoot.Exists() ||
		nextRoot.Get("search_origins.0.candidate_source").String() != "graph_query.nodes" ||
		!gjson.Get(got, "data.continuation.requires_model_decision").Bool() ||
		gjson.Get(got, "meta.stop_reason").String() != "agent_decision_required" {
		t.Fatalf("GraphQuery root was not exposed for Agent selection: %s", got)
	}
	if gotLane := graphQueryStub.CapturedHeaders.Get("x-tt-env"); gotLane != graphSearchDefaultTTEnv {
		t.Fatalf("GraphQuery x-tt-env = %q, want %q", gotLane, graphSearchDefaultTTEnv)
	}
	if len(oneHopStub.CapturedBodies) != 1 || graphSearchBodyHasRoot(oneHopStub.CapturedBody, "docG") {
		t.Fatalf("GraphQuery-only node entered OneHop frontier: %s", oneHopStub.CapturedBody)
	}
}

func TestGraphSearchGraphQueryFailureReturnsPrimaryResultAsIncomplete(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	anchor := time.Date(2026, 9, 10, 12, 0, 0, 0, graphSearchLocation).Unix()
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Body: map[string]interface{}{
			"passages": []interface{}{
				graphSearchPassage("p-doc", graphSearchSourceDoc, "Recent project", "https://bytedance.larkoffice.com/docx/docA", 1, 0, anchor),
			},
			"BaseResp": map[string]interface{}{"StatusCode": 0},
		},
	})
	graphQueryFailure := &httpmock.Stub{
		Method:   http.MethodPost,
		URL:      graphQueryPath,
		Reusable: true,
		Body:     map[string]interface{}{"code": graphQueryRetryCode, "msg": "retry graph query"},
	}
	registry.Register(graphQueryFailure)
	registry.Register(graphSearchOneHopStub("docA", []interface{}{
		graphSearchNode("doc-day-a", 2, "docA", "2026-09-10", anchor),
	}, []interface{}{}))

	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "最近项目进展", "--graph-query-mode", "auto",
		"--graph-query-lookback-days", "1", "--as", "user", "--format", "json",
	}, f, stdout, registry)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := stdout.String()
	if gjson.Get(got, "meta.complete").Bool() || !gjson.Get(got, "meta.graph_query_called").Bool() ||
		gjson.Get(got, "meta.graph_query_failed_window_count").Int() != 1 ||
		gjson.Get(got, "data.graph_query.failed_windows.0.attempts").Int() != graphQueryMaxAttempts {
		t.Fatalf("GraphQuery partial metadata missing: %s", got)
	}
	if gjson.Get(got, "data.nodes.#").Int() != 1 || gjson.Get(got, "data.evidence.one_hop_only_node_count").Int() != 1 {
		t.Fatalf("primary OneHop result was not preserved: %s", got)
	}
	if len(graphQueryFailure.CapturedBodies) != graphQueryMaxAttempts {
		t.Fatalf("GraphQuery attempts = %d, want %d", len(graphQueryFailure.CapturedBodies), graphQueryMaxAttempts)
	}
}

func TestBuildGraphSearchEvidenceIndexGroupsEdgesAndIndexesDetails(t *testing.T) {
	nodes := []map[string]interface{}{
		{"node_id": "n1", "node_type": int64(1), "first_seen_hop": 1, "detail": map[string]interface{}{"content": "node detail", "format": "markdown"}},
		{"node_id": "n2", "node_type": int64(2), "first_seen_hop": 2},
	}
	edges := []map[string]interface{}{
		{"edge_id": "e1", "relation_type": "im_reply", "from_node_id": "n1", "to_node_id": "n1", "first_seen_hop": 1, "detail": map[string]interface{}{"content": "reply one"}},
		{"edge_id": "e2", "relation_type": "im_reply", "from_node_id": "n1", "to_node_id": "n1", "first_seen_hop": 2, "detail": map[string]interface{}{"content": "reply two"}},
		{"edge_id": "e3", "relation_type": "im_doc_share", "from_node_id": "n1", "to_node_id": "missing", "first_seen_hop": 2},
	}
	candidates := []graphSearchCandidate{{Content: "allowed", PermissionFiltered: true}}

	index := buildGraphSearchEvidenceIndex(candidates, nodes, edges)
	if !index.PermissionFiltered || index.CandidateContentCount != 1 || index.NodeDetailCount != 1 || index.MissingNodeDetailCount != 1 {
		t.Fatalf("candidate/node evidence index = %#v", index)
	}
	if index.EdgeDetailCount != 2 || index.MissingEdgeDetailCount != 1 || index.SelfLoopEdgeCount != 2 {
		t.Fatalf("edge evidence counts = %#v", index)
	}
	if len(index.EdgeGroups) != 2 || index.EdgeGroups[1].RelationType != "im_reply" || index.EdgeGroups[1].EdgeCount != 2 || index.EdgeGroups[1].DetailCount != 2 {
		t.Fatalf("edge groups = %#v", index.EdgeGroups)
	}
	if !index.EdgeDetails[0].FromNodePresent || !index.EdgeDetails[0].ToNodePresent || index.EdgeDetails[2].ToNodePresent {
		t.Fatalf("edge endpoint evidence = %#v", index.EdgeDetails)
	}
	if index.NodeDetails[0].DetailPath != "nodes[0].detail" || index.EdgeDetails[0].DetailPath != "edges[0].detail" {
		t.Fatalf("detail paths: nodes=%#v edges=%#v", index.NodeDetails, index.EdgeDetails)
	}
}

func TestMergeGraphSearchObjectPreservesLateDetail(t *testing.T) {
	objects := map[string]map[string]interface{}{
		"n1": {"node_id": "n1", "first_seen_hop": 1},
	}
	candidate := map[string]interface{}{
		"node_id": "n1", "first_seen_hop": 2,
		"detail": map[string]interface{}{"content": "late detail"},
	}
	newObject, newDetail := mergeGraphSearchObject(objects, candidate, "node_id")
	if newObject || !newDetail {
		t.Fatalf("merge result newObject=%t newDetail=%t", newObject, newDetail)
	}
	detail, _ := objects["n1"]["detail"].(map[string]interface{})
	if got, _ := detail["content"].(string); got != "late detail" {
		t.Fatalf("merged detail content = %q", got)
	}
}

func TestGraphSearchKnowledgeErrorRedactsUID(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Status: http.StatusBadRequest,
		Body: map[string]interface{}{
			"code": 40001,
			"msg":  "invalid user " + strconv.FormatInt(graphSearchTestInternalUID, 10),
		},
	})
	err := runGraphSearchShortcut(t, []string{"+graph-search", "--query", "q", "--as", "user", "--format", "json"}, f, stdout, registry)
	if err == nil || strings.Contains(err.Error(), strconv.FormatInt(graphSearchTestInternalUID, 10)) || !strings.Contains(err.Error(), "<redacted-user-id>") {
		t.Fatalf("error = %v, want redacted UID", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestGraphSearchReturnsPartialResultsWhenOneBatchFails(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	dateA := "2026-09-09"
	dateB := "2026-09-10"
	anchorA := time.Date(2026, 9, 9, 12, 0, 0, 0, graphSearchLocation).Unix()
	anchorB := time.Date(2026, 9, 10, 12, 0, 0, 0, graphSearchLocation).Unix()
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Body: map[string]interface{}{
			"passages": []interface{}{
				graphSearchPassage("p-doc", graphSearchSourceDoc, "Doc", "https://bytedance.larkoffice.com/docx/docA", 1, 0, anchorA),
				graphSearchPassage("p-message", graphSearchSourceMessage, "Chat", "https://applink.feishu.cn/client/chat/open?chatId=123", 0.9, anchorB, anchorB),
			},
			"BaseResp": map[string]interface{}{"StatusCode": 0},
		},
	})
	registry.Register(graphSearchOneHopStub("docA", []interface{}{
		graphSearchNode("doc-day-a", 2, "docA", dateA, anchorA),
	}, []interface{}{}))
	failureStub := &httpmock.Stub{
		Method:   http.MethodPost,
		URL:      graphOneHopPath,
		Reusable: true,
		BodyFilter: func(body []byte) bool {
			return graphSearchBodyHasRoot(body, "123")
		},
		Body: map[string]interface{}{"code": graphQueryRetryCode, "msg": "retry later"},
	}
	registry.Register(failureStub)

	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "q", "--concurrency", "2", "--as", "user", "--format", "json",
	}, f, stdout, registry)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := stdout.String()
	if gjson.Get(got, "meta.complete").Bool() || gjson.Get(got, "meta.failed_batch_count").Int() != 1 {
		t.Fatalf("partial metadata missing: %s", got)
	}
	if gjson.Get(got, "data.nodes.#").Int() != 1 || gjson.Get(got, "data.failed_batches.0.attempts").Int() != graphQueryMaxAttempts {
		t.Fatalf("partial result unexpected: %s", got)
	}
	if len(failureStub.CapturedBodies) != graphQueryMaxAttempts {
		t.Fatalf("failure attempts = %d, want %d", len(failureStub.CapturedBodies), graphQueryMaxAttempts)
	}
	if gjson.Get(got, "data.failed_batches.0.graph_date").String() != dateB {
		t.Fatalf("failed batch date missing: %s", got)
	}
}

func TestGraphSearchAllOneHopBatchesFail(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	anchor := time.Date(2026, 9, 10, 12, 0, 0, 0, graphSearchLocation).Unix()
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Body: map[string]interface{}{
			"passages": []interface{}{graphSearchPassage("p-doc", graphSearchSourceDoc, "Doc", "https://bytedance.larkoffice.com/docx/docA", 1, 0, anchor)},
			"BaseResp": map[string]interface{}{"StatusCode": 0},
		},
	})
	registry.Register(&httpmock.Stub{
		Method:   http.MethodPost,
		URL:      graphOneHopPath,
		Reusable: true,
		Body:     map[string]interface{}{"code": graphQueryRetryCode, "msg": "retry later"},
	})
	err := runGraphSearchShortcut(t, []string{
		"+graph-search", "--query", "q", "--as", "user", "--format", "json",
	}, f, stdout, registry)
	if err == nil || !errs.IsAPI(err) || !strings.Contains(err.Error(), "all OneHop batches failed") {
		t.Fatalf("error = %T %v, want all-batches API error", err, err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no partial success envelope", stdout.String())
	}
}

func TestGraphSearchMinutesOnlyReturnsNormalEmptyGraph(t *testing.T) {
	f, stdout, _, registry := cmdutil.TestFactory(t, memoryTestConfig(t))
	anchor := time.Date(2026, 9, 10, 12, 0, 0, 0, graphSearchLocation).Unix()
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchKnowledgeQAURL,
		Body: map[string]interface{}{
			"passages": []interface{}{graphSearchMinutesPassage("p-minutes", "https://meetings.larkoffice.com/minutes/minuteA", anchor)},
			"BaseResp": map[string]interface{}{"StatusCode": 0},
		},
	})
	err := runGraphSearchShortcut(t, []string{"+graph-search", "--query", "q", "--as", "user", "--format", "json"}, f, stdout, registry)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := stdout.String()
	if gjson.Get(got, "meta.one_hop_called").Bool() || gjson.Get(got, "meta.stop_reason").String() != "no_query_roots" {
		t.Fatalf("empty result metadata unexpected: %s", got)
	}
	if gjson.Get(got, "data.nodes.#").Int() != 0 || gjson.Get(got, "data.skipped_candidates.0.reason").String() != "minutes_root_unsupported" {
		t.Fatalf("empty result unexpected: %s", got)
	}
}

func TestGraphSearchIndexedExecutesEachJobOnce(t *testing.T) {
	const count = 100
	hits := make([]int, count)
	var mu sync.Mutex
	runGraphSearchIndexed(context.Background(), 8, count, func(index int) {
		mu.Lock()
		hits[index]++
		mu.Unlock()
	})
	for index, hit := range hits {
		if hit != 1 {
			t.Fatalf("job %d executed %d times", index, hit)
		}
	}
}

func TestGraphSearchRequestLimiterCapsCrossStageConcurrency(t *testing.T) {
	spec := graphSearchSpec{RequestLimiter: make(chan struct{}, 2)}
	current := 0
	maximum := 0
	var mu sync.Mutex
	runGraphSearchIndexed(context.Background(), 8, 20, func(_ int) {
		release, err := acquireGraphSearchRequestPermit(context.Background(), spec)
		if err != nil {
			t.Errorf("acquire permit: %v", err)
			return
		}
		mu.Lock()
		current++
		if current > maximum {
			maximum = current
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		current--
		mu.Unlock()
		release()
	})
	if maximum != 2 {
		t.Fatalf("maximum in-flight requests = %d, want 2", maximum)
	}
}

func graphSearchPassage(id string, sourceType int64, title, rawURL string, score float64, createTime, updateTime int64) map[string]interface{} {
	return map[string]interface{}{
		"id": id, "source_type": sourceType, "title": title, "content": title + " content", "url": rawURL, "score": score,
		"extra": map[string]interface{}{
			"create_time": strconv.FormatInt(createTime, 10),
			"update_time": strconv.FormatInt(updateTime, 10),
		},
	}
}

func graphSearchMinutesPassage(id, rawURL string, createTime int64) map[string]interface{} {
	passage := graphSearchPassage(id, graphSearchSourceMinutes, "Minutes", rawURL, 0.7, createTime, createTime)
	passage["transparent_info"] = `{"minutes_id":"7656755056137980873"}`
	return passage
}

func graphSearchNode(nodeID string, nodeType int64, rootID, graphDate string, eventTime int64) map[string]interface{} {
	return map[string]interface{}{
		"node_id": nodeID, "node_type": nodeType, "root_id": rootID, "source_id": rootID,
		"graph_date": graphDate, "event_time_sec": eventTime,
		"detail": map[string]interface{}{"content": nodeID},
	}
}

func graphSearchEdge(edgeID, fromNodeID, toNodeID string) map[string]interface{} {
	return map[string]interface{}{
		"edge_id": edgeID, "relation_type": "related", "from_node_id": fromNodeID, "to_node_id": toNodeID,
		"detail": map[string]interface{}{"content": edgeID},
	}
}

func graphSearchOneHopStub(rootID string, nodes, edges []interface{}) *httpmock.Stub {
	return &httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphOneHopPath,
		BodyFilter: func(body []byte) bool {
			return graphSearchBodyHasRoot(body, rootID)
		},
		Body: map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"data": map[string]interface{}{"nodes": nodes, "edges": edges},
			},
		},
	}
}

func graphSearchBodyHasRoot(body []byte, rootID string) bool {
	for _, root := range gjson.GetBytes(body, "roots").Array() {
		if root.Get("root_id").String() == rootID {
			return true
		}
	}
	return false
}

func graphSearchFindRoot(roots []gjson.Result, nodeType int64, rootID string) gjson.Result {
	for _, root := range roots {
		if root.Get("node_type").Int() == nodeType && root.Get("root_id").String() == rootID {
			return root
		}
	}
	return gjson.Result{}
}

func mountedGraphSearchCommand(t *testing.T) *cobra.Command {
	t.Helper()
	parent := &cobra.Command{Use: "memory"}
	MemoryGraphSearch.Mount(parent, &cmdutil.Factory{})
	return parent.Commands()[0]
}

func runGraphSearchShortcut(t *testing.T, args []string, f *cmdutil.Factory, stdout *bytes.Buffer, registries ...*httpmock.Registry) error {
	t.Helper()
	if !graphSearchTestHasFlag(args, "--graph-query-mode") {
		args = append(args, "--graph-query-mode", "off")
	}
	for _, registry := range registries {
		registerGraphSearchIdentityStubs(registry)
	}
	parent := &cobra.Command{Use: "memory"}
	MemoryGraphSearch.Mount(parent, f)
	parent.SetArgs(args)
	parent.SilenceErrors = true
	parent.SilenceUsage = true
	if stdout != nil {
		stdout.Reset()
	}
	return parent.ExecuteContext(context.Background())
}

func graphSearchTestHasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func registerGraphSearchIdentityStubs(registry *httpmock.Registry) {
	registry.Register(&httpmock.Stub{
		Method: http.MethodGet,
		URL:    graphSearchUserInfoPath,
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{"open_id": "ou_graph_test_user"},
		},
	})
	registry.Register(&httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphSearchIdentityURL,
		Body: map[string]interface{}{
			"out_id_to_in_id_map": map[string]interface{}{"ou_graph_test_user": graphSearchTestInternalUID},
			"BaseResp":            map[string]interface{}{"StatusCode": 0, "StatusMessage": ""},
		},
	})
}
