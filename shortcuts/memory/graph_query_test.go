// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/spf13/cobra"

	"code.byted.org/lark_search/larksuite-cli/errs"
	extcs "code.byted.org/lark_search/larksuite-cli/extension/contentsafety"
	"code.byted.org/lark_search/larksuite-cli/internal/client"
	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/core"
	"code.byted.org/lark_search/larksuite-cli/internal/credential"
	"code.byted.org/lark_search/larksuite-cli/internal/httpmock"
	"code.byted.org/lark_search/larksuite-cli/internal/output"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

func TestMemoryGraphQueryRegistered(t *testing.T) {
	got := Shortcuts()
	if len(got) != 4 {
		t.Fatalf("len(Shortcuts()) = %d, want 4", len(got))
	}
	if got[0].Command != "+list" || got[1].Command != "+get" || got[2].Command != "+graph-query" || got[3].Command != "+one-hop" {
		t.Fatalf("commands = %q, %q, %q, %q; want +list, +get, +graph-query, +one-hop", got[0].Command, got[1].Command, got[2].Command, got[3].Command)
	}
}

func TestMemoryGraphQueryDoesNotExposeUserIDFlag(t *testing.T) {
	parent := &cobra.Command{Use: "memory"}
	MemoryGraphQuery.Mount(parent, &cmdutil.Factory{})
	cmd := parent.Commands()[0]

	if flag := cmd.Flags().Lookup("user-id"); flag != nil {
		t.Fatalf("--user-id must not be exposed")
	}
}

func TestMemoryGraphQueryRejectsLegacyUserIDFlag(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--dry-run", "--as", "user",
		"--user-id", "ou_legacy_target",
	}, f, stdout)
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --user-id") {
		t.Fatalf("error = %v, want unknown flag: --user-id", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no dry-run request plan", stdout.String())
	}
}

func TestMemoryGraphQueryOnlySupportsUserIdentity(t *testing.T) {
	if want := []string{"user"}; !reflect.DeepEqual(MemoryGraphQuery.AuthTypes, want) {
		t.Fatalf("AuthTypes = %#v, want %#v", MemoryGraphQuery.AuthTypes, want)
	}

	config := memoryTestConfig(t)
	// Keep both identities available so --as bot reaches the shortcut AuthTypes gate.
	config.SupportedIdentities = 3
	f, stdout, _, _ := cmdutil.TestFactory(t, config)
	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--dry-run", "--as", "bot",
	}, f, stdout)
	var validation *errs.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want *errs.ValidationError", err, err)
	}
	if validation.Subtype != errs.SubtypeInvalidArgument || validation.Param != "--as" {
		t.Fatalf("validation = subtype %q param %q, want invalid_argument --as", validation.Subtype, validation.Param)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no dry-run request plan", stdout.String())
	}
}

func TestMemoryGraphQueryRequiresAuthenticatedSelfOpenID(t *testing.T) {
	for _, tc := range []struct {
		name       string
		userOpenID string
	}{
		{name: "missing open id"},
		{name: "invalid open id", userOpenID: "on_graph_test_user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := memoryTestConfig(t)
			config.UserOpenId = tc.userOpenID
			f, stdout, _, _ := cmdutil.TestFactory(t, config)

			err := runMemoryShortcut(t, MemoryGraphQuery, []string{
				"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--dry-run", "--as", "user",
			}, f, stdout)
			var authErr *errs.AuthenticationError
			if !errors.As(err, &authErr) {
				t.Fatalf("error = %T %v, want *errs.AuthenticationError", err, err)
			}
			problem, ok := errs.ProblemOf(err)
			if !ok || problem.Category != errs.CategoryAuthentication || problem.Subtype != errs.SubtypeTokenMissing {
				t.Fatalf("problem = %#v, ok = %v; want authentication/token_missing", problem, ok)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want no dry-run request plan", stdout.String())
			}
		})
	}
}

func TestMemoryGraphQueryDryRunShape(t *testing.T) {
	cases := []struct {
		name string
		s    common.Shortcut
		args []string
		want []string
	}{
		{
			name: "graph query plans every window",
			s:    MemoryGraphQuery,
			args: []string{"+graph-query", "--start-time-sec", "100", "--end-time-sec", "86501", "--dry-run", "--as", "user"},
			want: []string{"graph_query", "ou_graph_test_user", "markdown", "86500", "86501"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
			if err := runMemoryShortcut(t, tc.s, tc.args, f, stdout); err != nil {
				t.Fatalf("dry-run: %v", err)
			}
			got := stdout.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("dry-run output missing %q: %s", want, got)
				}
			}
		})
	}
}

func TestMemoryGraphQueryDryRunPlansEveryWindow(t *testing.T) {
	cases := []struct {
		name             string
		detailFormatFlag string
		wantDetailFormat string
	}{
		{name: "default markdown", wantDetailFormat: "markdown"},
		{name: "explicit json", detailFormatFlag: "json", wantDetailFormat: "json"},
		{name: "normalized markdown", detailFormatFlag: " markdown ", wantDetailFormat: "markdown"},
		{name: "normalized json", detailFormatFlag: " json ", wantDetailFormat: "json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
			args := []string{"+graph-query", "--start-time-sec", "100", "--end-time-sec", "86501", "--dry-run", "--as", "user"}
			if tc.detailFormatFlag != "" {
				args = append(args, "--detail-format", tc.detailFormatFlag)
			}
			if err := runMemoryShortcut(t, MemoryGraphQuery, args, f, stdout); err != nil {
				t.Fatalf("dry-run: %v", err)
			}

			var result struct {
				API []struct {
					Method string                 `json:"method"`
					URL    string                 `json:"url"`
					Body   map[string]interface{} `json:"body"`
				} `json:"api"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("decode dry-run output: %v\n%s", err, stdout.String())
			}
			if len(result.API) != 2 {
				t.Fatalf("dry-run calls = %d, want 2: %s", len(result.API), stdout.String())
			}

			wantWindows := [][2]float64{{100, 86500}, {86500, 86501}}
			for i, call := range result.API {
				if call.Method != "POST" || call.URL != memoryAPIBasePath+"/graph_query" {
					t.Fatalf("call %d = %s %s, want POST %s", i+1, call.Method, call.URL, memoryAPIBasePath+"/graph_query")
				}
				if userID, _ := call.Body["user_id"].(string); userID != "ou_graph_test_user" {
					t.Fatalf("call %d user_id = %q, want ou_graph_test_user", i+1, userID)
				}
				timeRange, _ := call.Body["time_range"].(map[string]interface{})
				if timeRange["start_time_sec"] != wantWindows[i][0] || timeRange["end_time_sec"] != wantWindows[i][1] {
					t.Fatalf("call %d time_range = %#v, want [%v, %v)", i+1, timeRange, wantWindows[i][0], wantWindows[i][1])
				}
				params, _ := call.Body["params"].(map[string]interface{})
				if detail, _ := params["detail_format"].(string); detail != tc.wantDetailFormat {
					t.Fatalf("call %d detail_format = %q, want %q", i+1, detail, tc.wantDetailFormat)
				}
			}
		})
	}
}

func TestMemoryGraphQueryPreflightReturnsTypedErrorWhenNormalizationWritebackFails(t *testing.T) {
	setErr := errors.New("set detail format failed")
	cmd := &cobra.Command{Use: "graph-query"}
	cmd.Flags().Var(&rejectingGraphStringValue{value: " markdown ", err: setErr}, "detail-format", "")
	cmd.Flags().String("start-time-sec", "100", "")
	cmd.Flags().String("end-time-sec", "101", "")
	cmd.Flags().String("jq", "", "")
	cmd.Flags().String("format", "json", "")

	err := validateGraphQueryRawFlags(context.Background(), cmd)
	var internal *errs.InternalError
	if !errors.As(err, &internal) {
		t.Fatalf("error = %T %v, want typed internal error", err, err)
	}
	if !errors.Is(err, setErr) {
		t.Fatalf("errors.Is(error, setErr) = false: %v", err)
	}
}

type rejectingGraphStringValue struct {
	value string
	err   error
}

func (v *rejectingGraphStringValue) String() string   { return v.value }
func (v *rejectingGraphStringValue) Type() string     { return "string" }
func (v *rejectingGraphStringValue) Set(string) error { return v.err }

func TestMemoryGraphQueryValidation(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		param string
	}{
		{
			name:  "rejects jq",
			args:  []string{"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--jq", ".data", "--as", "user"},
			param: "--jq",
		},
		{
			name:  "rejects jq with ndjson",
			args:  []string{"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--jq", ".data", "--format", "ndjson", "--as", "user"},
			param: "--jq",
		},
		{
			name:  "rejects jq with pretty",
			args:  []string{"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--jq", ".data", "--format", "pretty", "--as", "user"},
			param: "--jq",
		},
		{
			name:  "rejects table format",
			args:  []string{"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--format", "table", "--as", "user"},
			param: "--format",
		},
		{
			name:  "rejects csv format",
			args:  []string{"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--format", "csv", "--as", "user"},
			param: "--format",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
			err := runMemoryShortcut(t, MemoryGraphQuery, tc.args, f, stdout)
			var validation *errs.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected validation error, got %T: %v", err, err)
			}
			if validation.Param != tc.param {
				t.Fatalf("param = %q, want %s", validation.Param, tc.param)
			}
		})
	}
}

func TestMemoryGraphQueryPreflightReportsMissingRequiredFlag(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		param string
	}{
		{
			name:  "missing start time",
			args:  []string{"+graph-query", "--end-time-sec", "101", "--dry-run"},
			param: "--start-time-sec",
		},
		{
			name:  "missing end time",
			args:  []string{"+graph-query", "--start-time-sec", "100", "--dry-run"},
			param: "--end-time-sec",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, stdout, _, _ := cmdutil.TestFactory(t, nil)
			err := runMemoryShortcut(t, MemoryGraphQuery, tc.args, f, stdout)
			problem, ok := errs.ProblemOf(err)
			if !ok || problem.Category != errs.CategoryValidation {
				t.Fatalf("problem = %#v, ok = %v; want validation", problem, ok)
			}
			var validation *errs.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T %v, want typed validation", err, err)
			}
			if validation.Subtype != errs.SubtypeInvalidArgument {
				t.Fatalf("subtype = %q, want %q", validation.Subtype, errs.SubtypeInvalidArgument)
			}
			if validation.Param != tc.param {
				t.Fatalf("param = %q, want %q", validation.Param, tc.param)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestMemoryGraphQueryPreflightRejectsOversizedRangeBeforeDependencies(t *testing.T) {
	err, stdout, calls := runGraphPreflightWithDependencyProbe(t, []string{
		"+graph-query",
		"--start-time-sec", "100",
		"--end-time-sec", "604901",
		"--dry-run",
	})
	assertGraphPreflightValidation(t, err, stdout, calls, "--end-time-sec")
}

func TestMemoryGraphQueryPreflightRejectsInvalidDetailFormatBeforeDependencies(t *testing.T) {
	for _, tc := range []struct {
		name       string
		formatArgs []string
	}{
		{name: "unsupported value", formatArgs: []string{"--detail-format", "xml"}},
		{name: "explicit empty value", formatArgs: []string{"--detail-format="}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{
				"+graph-query",
				"--start-time-sec", "100",
				"--end-time-sec", "101",
				"--dry-run",
			}
			args = append(args, tc.formatArgs...)
			err, stdout, calls := runGraphPreflightWithDependencyProbe(t, args)
			assertGraphPreflightValidation(t, err, stdout, calls, "--detail-format")
		})
	}
}

type graphPreflightDependencyCalls struct {
	config     int
	credential int
	token      int
	http       int
	lark       int
}

func runGraphPreflightWithDependencyProbe(t *testing.T, args []string) (error, string, graphPreflightDependencyCalls) {
	t.Helper()
	f, stdout, _, _ := cmdutil.TestFactory(t, nil)
	parent := &cobra.Command{Use: "memory", SilenceErrors: true, SilenceUsage: true}
	MemoryGraphQuery.Mount(parent, f)

	var calls graphPreflightDependencyCalls
	f.Config = func() (*core.CliConfig, error) {
		calls.config++
		return nil, errors.New("config must not be called")
	}
	f.HttpClient = func() (*http.Client, error) {
		calls.http++
		return nil, errors.New("http client must not be called")
	}
	f.LarkClient = func() (*lark.Client, error) {
		calls.lark++
		return nil, errors.New("lark client must not be called")
	}
	f.Credential = credential.NewCredentialProvider(
		nil,
		&countingGraphAccountResolver{calls: &calls.credential},
		&countingGraphTokenResolver{calls: &calls.token},
		f.HttpClient,
	)

	stdout.Reset()
	parent.SetArgs(args)
	return parent.Execute(), stdout.String(), calls
}

func assertGraphPreflightValidation(t *testing.T, err error, stdout string, calls graphPreflightDependencyCalls, wantParam string) {
	t.Helper()
	problem, ok := errs.ProblemOf(err)
	if !ok || problem.Category != errs.CategoryValidation {
		t.Fatalf("problem = %#v, ok = %v; want validation", problem, ok)
	}
	var validation *errs.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want typed validation", err, err)
	}
	if validation.Subtype != errs.SubtypeInvalidArgument || validation.Param != wantParam {
		t.Fatalf("validation = subtype %q param %q, want invalid_argument %s", validation.Subtype, validation.Param, wantParam)
	}
	if calls != (graphPreflightDependencyCalls{}) {
		t.Fatalf("dependency calls = %+v, want all zero", calls)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want no dry-run plan", stdout)
	}
}

type countingGraphAccountResolver struct {
	calls *int
}

func (r *countingGraphAccountResolver) ResolveAccount(context.Context) (*credential.Account, error) {
	(*r.calls)++
	return nil, errors.New("credential must not be called")
}

type countingGraphTokenResolver struct {
	calls *int
}

func (r *countingGraphTokenResolver) ResolveToken(context.Context, credential.TokenSpec) (*credential.TokenResult, error) {
	(*r.calls)++
	return nil, errors.New("token must not be called")
}

func TestMemoryGraphQueryExecuteOneWindowEmitsOneEnvelope(t *testing.T) {
	f, stdout, _, reg := memoryGraphTestFactory(t)
	stub := graphQueryStub(
		graphNode("node-only-window", "source-only-window", 100),
		graphEdge("edge-only-window", "node-only-window", "node-target", 100),
	)
	reg.Register(stub)

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--as", "user", "--format", "ndjson",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	reg.Verify(t)
	if len(stub.CapturedBodies) != 1 {
		t.Fatalf("Graph requests = %d, want exactly 1", len(stub.CapturedBodies))
	}
	assertGraphRequestWindow(t, stub.CapturedBody, 100, 101)

	lines := graphOutputLines(t, stdout)
	if len(lines) != 1 {
		t.Fatalf("stdout lines = %d, want 1: %q", len(lines), stdout.String())
	}
	var env output.Envelope
	if err := json.Unmarshal([]byte(lines[0]), &env); err != nil {
		t.Fatalf("decode output.Envelope: %v\n%s", err, lines[0])
	}
	if !env.OK || env.Identity != "user" {
		t.Fatalf("envelope = ok:%v identity:%q", env.OK, env.Identity)
	}
	window := decodeGraphWindow(t, env.Data)
	if window.WindowIndex != 1 || window.StartTimeSec != 100 || window.EndTimeSec != 101 {
		t.Fatalf("boundaries = index:%d [%d,%d), want index:1 [100,101)", window.WindowIndex, window.StartTimeSec, window.EndTimeSec)
	}
	if got := firstGraphNodeID(t, window.Data); got != "node-only-window" {
		t.Fatalf("node_id = %q, want node-only-window", got)
	}
	if !strings.Contains(lines[0], "edge-only-window") || strings.Contains(lines[0], "node-first") || strings.Contains(lines[0], "node-second") {
		t.Fatalf("single-window envelope does not isolate its response: %s", lines[0])
	}
}

func TestMemoryGraphQueryExecuteStreamsWindowsChronologically(t *testing.T) {
	f, stdout, _, reg := memoryGraphTestFactory(t)
	first := graphQueryStubForWindow(100, 86500, graphNode("node-first", "source-first", 100), graphEdge("edge-first", "node-first", "node-next", 101))
	second := graphQueryStubForWindow(86500, 86501, graphNode("node-second", "source-second", 86500), graphEdge("edge-second", "node-second", "node-last", 86501))
	reg.Register(first)
	reg.Register(second)

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "86501", "--as", "user", "--format", "json",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	reg.Verify(t)

	wantWindows := [][2]int64{{100, 86500}, {86500, 86501}}
	for i, stub := range []*httpmock.Stub{first, second} {
		assertGraphRequestWindow(t, stub.CapturedBody, wantWindows[i][0], wantWindows[i][1])
		if got := stub.CapturedHeaders.Get("x-tt-env"); got != "ppe_memory_hub" {
			t.Fatalf("call %d x-tt-env = %q, want ppe_memory_hub", i+1, got)
		}
	}

	lines := graphOutputLines(t, stdout)
	if len(lines) != 2 {
		t.Fatalf("stdout lines = %d, want 2: %q", len(lines), stdout.String())
	}
	wantNodes := []string{"node-first", "node-second"}
	for i, line := range lines {
		var env output.Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("decode line %d as output.Envelope: %v\n%s", i+1, err, line)
		}
		if !env.OK || env.Identity != "user" {
			t.Fatalf("line %d envelope = ok:%v identity:%q", i+1, env.OK, env.Identity)
		}
		window := decodeGraphWindow(t, env.Data)
		if window.WindowIndex != i+1 || window.StartTimeSec != wantWindows[i][0] || window.EndTimeSec != wantWindows[i][1] {
			t.Fatalf("line %d boundaries = index:%d [%d,%d), want index:%d [%d,%d)", i+1, window.WindowIndex, window.StartTimeSec, window.EndTimeSec, i+1, wantWindows[i][0], wantWindows[i][1])
		}
		if got := firstGraphNodeID(t, window.Data); got != wantNodes[i] {
			t.Fatalf("line %d node_id = %q, want %q", i+1, got, wantNodes[i])
		}
		if strings.Contains(line, wantNodes[1-i]) {
			t.Fatalf("line %d contains another window's response: %s", i+1, line)
		}
	}
}

func TestMemoryGraphQueryStopsAfterFailureAndPreservesTypedError(t *testing.T) {
	const (
		messageSentinel = "sentinel-private-graph-message"
		hintSentinel    = "sentinel-private-graph-detail"
	)
	f, stdout, stderr, reg := memoryGraphTestFactory(t)
	first := graphQueryStubForWindow(100, 86500, graphNode("node-first", "source-first", 100), graphEdge("edge-first", "node-first", "node-next", 101))
	failure := &httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphQueryPath,
		BodyFilter: func(body []byte) bool {
			return graphRequestMatchesWindow(body, 86500, 172900)
		},
		Body: map[string]interface{}{
			"code":   1040,
			"msg":    messageSentinel,
			"log_id": "log-graph-window-2",
			"error": map[string]interface{}{
				"troubleshooter": "https://open.feishu.cn/document/troubleshooter/graph-window",
				"details": []interface{}{
					map[string]interface{}{"value": hintSentinel},
				},
			},
		},
	}
	third := graphQueryStubForWindow(172900, 172901, graphNode("node-third-secret", "source-third", 172900), graphEdge("edge-third", "node-third-secret", "node-last", 172901))
	reg.Register(first)
	reg.Register(failure)
	reg.Register(third)

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "172901", "--as", "user", "--format", "json",
	}, f, stdout)
	if err == nil {
		t.Fatal("execute error = nil, want middle-window API error")
	}
	lines := graphOutputLines(t, stdout)
	if len(lines) != 1 || !strings.Contains(lines[0], "node-first") {
		t.Fatalf("stdout = %q, want only first successful window", stdout.String())
	}
	if strings.Contains(stdout.String(), "node-second") || strings.Contains(stdout.String(), "node-third-secret") {
		t.Fatalf("stdout rendered a window after the blocking failure: %q", stdout.String())
	}

	var apiErr *errs.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T, want concrete *errs.APIError", err)
	}
	problem, ok := errs.ProblemOf(err)
	if !ok {
		t.Fatalf("errs.ProblemOf(%T) failed", err)
	}
	if problem.Category != errs.CategoryAPI || problem.Code != 1040 || problem.LogID != "log-graph-window-2" || problem.Retryable {
		t.Fatalf("problem = category:%s code:%d log_id:%q retryable:%v", problem.Category, problem.Code, problem.LogID, problem.Retryable)
	}
	if problem.Troubleshooter != "https://open.feishu.cn/document/troubleshooter/graph-window" {
		t.Fatalf("troubleshooter = %q, want upstream extension preserved", problem.Troubleshooter)
	}
	if problem.Message != "graph query request failed" {
		t.Fatalf("message = %q, want fixed Graph-local message", problem.Message)
	}
	for _, want := range []string{"window_index=2", "start_time_sec=86500", "end_time_sec=172900"} {
		if !strings.Contains(problem.Hint, want) {
			t.Fatalf("hint missing %q: %q", want, problem.Hint)
		}
	}
	if !output.WriteTypedErrorEnvelope(stderr, err, "user") {
		t.Fatal("mounted command error was not serializable")
	}
	for _, forbidden := range []string{messageSentinel, hintSentinel, "user_id", "ou_graph_test_user", "node-third-secret"} {
		if strings.Contains(problem.Message, forbidden) || strings.Contains(problem.Hint, forbidden) || strings.Contains(err.Error(), forbidden) || strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("Graph error leaks %q: message=%q hint=%q err=%q stderr=%q", forbidden, problem.Message, problem.Hint, err.Error(), stderr.String())
		}
	}
}

func TestMemoryGraphQueryMalformedResponseRedactsBody(t *testing.T) {
	const sentinel = "sentinel-private-node-detail"
	f, stdout, stderr, reg := memoryGraphTestFactory(t)
	stub := &httpmock.Stub{
		Method:  http.MethodPost,
		URL:     graphQueryPath,
		Headers: http.Header{"Content-Type": []string{"text/plain"}},
		RawBody: []byte(`{"code":0,"data":{"nodes":[{"detail":"` + sentinel + `"}]}`),
	}
	reg.Register(stub)

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--as", "user", "--format", "json",
	}, f, stdout)
	problem, ok := errs.ProblemOf(err)
	if !ok {
		t.Fatalf("expected typed error, got %T: %v", err, err)
	}
	if problem.Category != errs.CategoryInternal || problem.Subtype != errs.SubtypeInvalidResponse || problem.Code != 0 || problem.LogID != "" || problem.Retryable {
		t.Fatalf("problem = category:%s subtype:%s code:%d log_id:%q retryable:%v", problem.Category, problem.Subtype, problem.Code, problem.LogID, problem.Retryable)
	}
	if strings.Contains(problem.Message, sentinel) || strings.Contains(problem.Hint, sentinel) {
		t.Fatalf("serialized problem leaks malformed response: message=%q hint=%q", problem.Message, problem.Hint)
	}
	if cause := errors.Unwrap(err); cause == nil || !strings.Contains(cause.Error(), sentinel) {
		t.Fatalf("parse diagnostics not retained in cause: %v", cause)
	}
	if !output.WriteTypedErrorEnvelope(stderr, err, "user") {
		t.Fatal("mounted command error was not serializable")
	}
	if strings.Contains(stderr.String(), sentinel) {
		t.Fatalf("mounted command stderr leaks malformed response: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if len(stub.CapturedBodies) != 1 {
		t.Fatalf("API calls = %d, want 1", len(stub.CapturedBodies))
	}
}

func TestMemoryGraphQueryHTTPErrorRedactsBody(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		category  errs.Category
		subtype   errs.Subtype
		retryable bool
		wantCalls int
	}{
		{name: "client error", status: http.StatusBadRequest, category: errs.CategoryAPI, subtype: errs.SubtypeUnknown, wantCalls: 1},
		{name: "server error", status: http.StatusBadGateway, category: errs.CategoryNetwork, subtype: errs.SubtypeNetworkServer, retryable: true, wantCalls: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const sentinel = "sentinel-private-http-body"
			f, stdout, stderr, reg := memoryGraphTestFactory(t)
			stub := &httpmock.Stub{
				Method:   http.MethodPost,
				URL:      graphQueryPath,
				Status:   tt.status,
				Headers:  http.Header{"Content-Type": []string{"text/plain"}, "X-Tt-Logid": []string{"log-graph-redaction"}},
				RawBody:  []byte(sentinel),
				Reusable: tt.wantCalls > 1,
			}
			reg.Register(stub)

			err := runMemoryShortcut(t, MemoryGraphQuery, []string{
				"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--as", "user", "--format", "json",
			}, f, stdout)
			problem, ok := errs.ProblemOf(err)
			if !ok {
				t.Fatalf("expected typed error, got %T: %v", err, err)
			}
			if problem.Category != tt.category || problem.Subtype != tt.subtype || problem.Code != tt.status || problem.LogID != "log-graph-redaction" || problem.Retryable != tt.retryable {
				t.Fatalf("problem = category:%s subtype:%s code:%d log_id:%q retryable:%v", problem.Category, problem.Subtype, problem.Code, problem.LogID, problem.Retryable)
			}
			if strings.Contains(problem.Message, sentinel) || strings.Contains(problem.Hint, sentinel) {
				t.Fatalf("serialized problem leaks HTTP body: message=%q hint=%q", problem.Message, problem.Hint)
			}
			if !output.WriteTypedErrorEnvelope(stderr, err, "user") {
				t.Fatal("mounted command error was not serializable")
			}
			if strings.Contains(stderr.String(), sentinel) {
				t.Fatalf("mounted command stderr leaks HTTP body: %q", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if len(stub.CapturedBodies) != tt.wantCalls {
				t.Fatalf("API calls = %d, want %d", len(stub.CapturedBodies), tt.wantCalls)
			}
		})
	}
}

func TestMemoryGraphQueryTransportErrorRedactsMountedStderr(t *testing.T) {
	const sentinel = "sentinel-private-graph-transport"
	cause := errors.New(sentinel)
	f, stdout, stderr, _ := memoryGraphTestFactory(t)
	config := memoryTestConfig(t)
	httpClient := &http.Client{Transport: graphErrorTransport{err: cause}}
	sdk := lark.NewClient(
		config.AppID,
		credential.RuntimeAppSecret(config.AppSecret),
		lark.WithEnableTokenCache(false),
		lark.WithLogLevel(larkcore.LogLevelError),
		lark.WithHttpClient(httpClient),
		lark.WithOpenBaseUrl(core.ResolveOpenBaseURL(config.Brand)),
	)
	f.LarkClient = func() (*lark.Client, error) { return sdk, nil }

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--as", "user", "--format", "json",
	}, f, stdout)
	problem, ok := errs.ProblemOf(err)
	if !ok {
		t.Fatalf("expected typed error, got %T: %v", err, err)
	}
	if problem.Category != errs.CategoryNetwork || problem.Subtype != errs.SubtypeNetworkTransport {
		t.Fatalf("problem = %s/%s, want %s/%s", problem.Category, problem.Subtype, errs.CategoryNetwork, errs.SubtypeNetworkTransport)
	}
	if strings.Contains(problem.Message, sentinel) || strings.Contains(problem.Hint, sentinel) {
		t.Fatalf("serialized problem leaks transport diagnostics: message=%q hint=%q", problem.Message, problem.Hint)
	}
	if !errors.Is(err, cause) {
		t.Fatal("transport cause was not preserved through graph annotation")
	}
	if !output.WriteTypedErrorEnvelope(stderr, err, "user") {
		t.Fatal("mounted command error was not serializable")
	}
	if strings.Contains(stderr.String(), sentinel) {
		t.Fatalf("mounted command stderr leaks transport diagnostics: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestMemoryGraphQueryDialFailureMakesThreeSingleTransportAttempts(t *testing.T) {
	const sentinelText = "sentinel-private-graph-dial"
	sentinelErr := errors.New(sentinelText)
	transport := &graphDialFailureTransport{cause: sentinelErr}
	httpClient := &http.Client{Transport: client.NewSingleAttemptTransport(transport)}

	config := memoryTestConfig(t)
	f, stdout, stderr, _ := cmdutil.TestFactory(t, config)
	sdk := lark.NewClient(
		config.AppID,
		credential.RuntimeAppSecret(config.AppSecret),
		lark.WithEnableTokenCache(false),
		lark.WithLogLevel(larkcore.LogLevelError),
		lark.WithHttpClient(httpClient),
		lark.WithOpenBaseUrl(core.ResolveOpenBaseURL(config.Brand)),
	)
	f.HttpClient = func() (*http.Client, error) { return httpClient, nil }
	f.LarkClient = func() (*lark.Client, error) { return sdk, nil }

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--as", "user", "--format", "json",
	}, f, stdout)
	var networkErr *errs.NetworkError
	if !errors.As(err, &networkErr) {
		t.Fatalf("error = %T %v, want *errs.NetworkError", err, err)
	}
	if transport.calls != 3 {
		t.Fatalf("RoundTrip calls = %d, want exactly 3", transport.calls)
	}
	if !errors.Is(err, sentinelErr) {
		t.Fatal("Graph dial error did not preserve the transport cause chain")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !output.WriteTypedErrorEnvelope(stderr, err, "user") {
		t.Fatal("mounted command error was not serializable")
	}
	if strings.Contains(networkErr.Message, sentinelText) || strings.Contains(networkErr.Hint, sentinelText) || strings.Contains(stderr.String(), sentinelText) {
		t.Fatalf("serialized Graph dial error leaks sentinel: message=%q hint=%q stderr=%q", networkErr.Message, networkErr.Hint, stderr.String())
	}
	if !strings.Contains(networkErr.Hint, "attempts=3") {
		t.Fatalf("hint = %q, want attempts=3", networkErr.Hint)
	}
}

func TestMemoryGraphQueryRetriesCode2200AndStreamsFinalSuccess(t *testing.T) {
	f, stdout, _, reg := memoryGraphTestFactory(t)
	failures := make([]*httpmock.Stub, 0, 2)
	for attempt := 1; attempt <= 2; attempt++ {
		stub := &httpmock.Stub{
			Method: http.MethodPost,
			URL:    graphQueryPath,
			BodyFilter: func(body []byte) bool {
				return graphRequestMatchesWindow(body, 100, 101)
			},
			Body: map[string]interface{}{"code": 2200, "msg": "retry graph window"},
		}
		failures = append(failures, stub)
		reg.Register(stub)
	}
	success := graphQueryStubForWindow(
		100,
		101,
		graphNode("node-after-retry", "source-after-retry", 100),
		graphEdge("edge-after-retry", "node-after-retry", "node-target", 100),
	)
	reg.Register(success)

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "101", "--as", "user", "--format", "ndjson",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for attempt, stub := range failures {
		if len(stub.CapturedBodies) != 1 {
			t.Fatalf("failure attempt %d calls = %d, want 1", attempt+1, len(stub.CapturedBodies))
		}
	}
	if len(success.CapturedBodies) != 1 {
		t.Fatalf("success calls = %d, want 1", len(success.CapturedBodies))
	}
	lines := graphOutputLines(t, stdout)
	if len(lines) != 1 || !strings.Contains(lines[0], "node-after-retry") {
		t.Fatalf("stdout = %q, want one successful post-retry envelope", stdout.String())
	}
}

func TestAnnotateGraphWindowErrorPreservesTypedErrors(t *testing.T) {
	window := graphTimeWindow{Index: 2, StartTimeSec: 86500, EndTimeSec: 172900}
	const attempts = 3
	wantSuffix := "failed window: window_index=2 start_time_sec=86500 end_time_sec=172900 attempts=3"

	t.Run("api error", func(t *testing.T) {
		cause := errors.New("api cause")
		typed := errs.NewAPIError(errs.SubtypeRateLimit, "rate limited").
			WithHint("existing api hint").
			WithLogID("log-api").
			WithCode(99991400).
			WithRetryable().
			WithCause(cause)
		typed.Troubleshooter = "https://open.feishu.cn/document/troubleshooter/api"
		want := *typed
		want.Message = "graph query request failed"
		want.Hint = wantSuffix

		got := annotateGraphWindowError(typed, window, attempts)
		if got != typed {
			t.Fatalf("returned error pointer = %p, want exact %p", got, typed)
		}
		if !reflect.DeepEqual(*typed, want) {
			t.Fatalf("API error changed beyond Graph-local message and hint\n got: %#v\nwant: %#v", *typed, want)
		}
		if !errors.Is(got, cause) {
			t.Fatal("API error cause was not preserved")
		}
	})

	t.Run("authentication error redacts user open id", func(t *testing.T) {
		const userOpenID = "ou_graph_secret_identity"
		cause := errors.New("authentication cause")
		typed := errs.NewAuthenticationError(errs.SubtypeTokenMissing, "token missing").
			WithHint("existing authentication hint").
			WithLogID("log-authentication").
			WithCode(99991661).
			WithRetryable().
			WithUserOpenID(userOpenID).
			WithCause(cause)
		typed.Troubleshooter = "https://open.feishu.cn/document/troubleshooter/authentication"
		want := *typed
		want.Message = "graph query request failed"
		want.Hint = wantSuffix
		want.UserOpenID = ""

		got := annotateGraphWindowError(typed, window, attempts)
		if got != typed {
			t.Fatalf("returned error pointer = %p, want exact %p", got, typed)
		}
		if !reflect.DeepEqual(*typed, want) {
			t.Fatalf("authentication error changed beyond Graph-local message, hint, and UserOpenID redaction\n got: %#v\nwant: %#v", *typed, want)
		}
		if !errors.Is(got, cause) {
			t.Fatal("authentication error cause was not preserved")
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal annotated authentication error: %v", err)
		}
		for _, forbidden := range []string{userOpenID, "user_open_id"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("marshaled authentication error leaks %q: %s", forbidden, encoded)
			}
		}
	})

	t.Run("permission error extensions", func(t *testing.T) {
		cause := errors.New("permission cause")
		typed := errs.NewPermissionError(errs.SubtypeMissingScope, "missing scope").
			WithHint("existing permission hint").
			WithLogID("log-permission").
			WithCode(99991679).
			WithRetryable().
			WithMissingScopes("memory:hub").
			WithRequestedScopes("memory:hub", "memory:graph").
			WithGrantedScopes("memory:graph").
			WithIdentity("user").
			WithConsoleURL("https://open.feishu.cn/app/cli-test/auth").
			WithCause(cause)
		typed.Troubleshooter = "https://open.feishu.cn/document/troubleshooter/permission"
		want := *typed
		want.MissingScopes = append([]string(nil), typed.MissingScopes...)
		want.RequestedScopes = append([]string(nil), typed.RequestedScopes...)
		want.GrantedScopes = append([]string(nil), typed.GrantedScopes...)
		want.Message = "graph query request failed"
		want.Hint = wantSuffix

		got := annotateGraphWindowError(typed, window, attempts)
		if got != typed {
			t.Fatalf("returned error pointer = %p, want exact %p", got, typed)
		}
		if !reflect.DeepEqual(*typed, want) {
			t.Fatalf("permission error changed beyond Graph-local message and hint\n got: %#v\nwant: %#v", *typed, want)
		}
		if !errors.Is(got, cause) {
			t.Fatal("permission error cause was not preserved")
		}
	})
}

func TestMemoryGraphQueryStopsAfterOutputFailure(t *testing.T) {
	for _, format := range []string{"json", "pretty"} {
		t.Run(format, func(t *testing.T) {
			f, _, _, reg := memoryGraphTestFactory(t)
			first := graphQueryStubForWindow(100, 86500, graphNode("node-first", "source-first", 100), graphEdge("edge-first", "node-first", "node-next", 101))
			second := graphQueryStubForWindow(86500, 86501, graphNode("node-second", "source-second", 86500), graphEdge("edge-second", "node-second", "node-last", 86501))
			reg.Register(first)
			reg.Register(second)

			const sentinelText = "distinctive graph output write failure"
			writeErr := errors.New(sentinelText)
			writer := &graphFailWriter{err: writeErr}
			f.IOStreams.Out = writer
			err := runMemoryShortcut(t, MemoryGraphQuery, []string{
				"+graph-query", "--start-time-sec", "100", "--end-time-sec", "86501", "--as", "user", "--format", format,
			}, f, nil)
			if !errors.Is(err, writeErr) {
				t.Fatalf("error = %T %v, want preserved write cause", err, err)
			}
			var internalErr *errs.InternalError
			if !errors.As(err, &internalErr) {
				t.Fatalf("error = %T, want *errs.InternalError", err)
			}
			if !strings.Contains(internalErr.Hint, "window_index=1") {
				t.Fatalf("hint = %q, want first-window context", internalErr.Hint)
			}
			if strings.Contains(internalErr.Message, sentinelText) || strings.Contains(internalErr.Hint, sentinelText) {
				t.Fatalf("serialized problem leaks writer failure: message=%q hint=%q", internalErr.Message, internalErr.Hint)
			}
			writes := writer.Writes()
			if len(writes) != 1 {
				t.Fatalf("output write attempts = %d, want exactly the first window", len(writes))
			}
			got := string(writes[0])
			firstWindow, laterWindow := `"window_index":1`, `"window_index":2`
			if format == "pretty" {
				firstWindow, laterWindow = "Window 1 [100, 86500)", "Window 2 [86500, 86501)"
			}
			if !strings.Contains(got, firstWindow) || strings.Contains(got, laterWindow) {
				t.Fatalf("recorded output = %q, want only the first window", got)
			}
		})
	}
}

func TestMemoryGraphQueryCancellationDuringSafetyScanStopsRendering(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONTENT_SAFETY_MODE", "warn")
	previous := extcs.GetProvider()
	t.Cleanup(func() { extcs.Register(previous) })

	ctx, cancel := context.WithCancel(context.Background())
	provider := &memoryGraphCancelingSafetyProvider{cancel: cancel}
	extcs.Register(provider)
	f, stdout, _, reg := memoryGraphTestFactory(t)
	first := graphQueryStubForWindow(100, 86500, graphNode("node-canceled", "source-first", 100), graphEdge("edge-first", "node-canceled", "node-next", 101))
	second := graphQueryStubForWindow(86500, 86501, graphNode("node-never-requested", "source-second", 86500), graphEdge("edge-second", "node-never-requested", "node-last", 86501))
	reg.Register(first)
	reg.Register(second)

	err := runMemoryShortcutContext(t, ctx, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "86501", "--as", "user", "--format", "json",
	}, f, stdout)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %T %v, want context.Canceled preserved through annotation", err, err)
	}
	if provider.scans != 1 {
		t.Fatalf("content-safety scans = %d, want 1 after first API response", provider.scans)
	}
	if len(first.CapturedBodies) != 1 {
		t.Fatalf("first API calls = %d, want 1", len(first.CapturedBodies))
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no envelope after cancellation during scan", stdout.String())
	}
	if strings.Contains(stdout.String(), "node-never-requested") {
		t.Fatalf("stdout rendered a window after cancellation: %q", stdout.String())
	}
}

func TestMemoryGraphQueryContentSafetyBlockStopsRendering(t *testing.T) {
	original := extcs.GetProvider()
	sentinel := &memoryGraphSafetyProvider{}
	extcs.Register(sentinel)
	t.Cleanup(func() { extcs.Register(original) })

	t.Run("block", testMemoryGraphQueryContentSafetyBlockStopsRendering)
	if got := extcs.GetProvider(); got != sentinel {
		t.Fatalf("content-safety provider after subtest = %T %p, want exact %T %p", got, got, sentinel, sentinel)
	}
}

func testMemoryGraphQueryContentSafetyBlockStopsRendering(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONTENT_SAFETY_MODE", "block")
	previous := extcs.GetProvider()
	extcs.Register(&memoryGraphSafetyProvider{alert: &extcs.Alert{Provider: "test", MatchedRules: []string{"graph-detail"}}})
	t.Cleanup(func() { extcs.Register(previous) })

	f, stdout, _, reg := memoryGraphTestFactory(t)
	first := graphQueryStubForWindow(100, 86500, graphNode("node-blocked", "source-blocked", 100), graphEdge("edge-blocked", "node-blocked", "node-next", 101))
	second := graphQueryStubForWindow(86500, 86501, graphNode("node-never-requested", "source-second", 86500), graphEdge("edge-second", "node-never-requested", "node-last", 86501))
	reg.Register(first)
	reg.Register(second)

	err := runMemoryShortcut(t, MemoryGraphQuery, []string{
		"+graph-query", "--start-time-sec", "100", "--end-time-sec", "86501", "--as", "user", "--format", "json",
	}, f, stdout)
	var safetyErr *errs.ContentSafetyError
	if !errors.As(err, &safetyErr) {
		t.Fatalf("error = %T %v, want *errs.ContentSafetyError", err, err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on first-window safety block", stdout.String())
	}
	if strings.Contains(stdout.String(), "node-never-requested") {
		t.Fatalf("stdout rendered a window after content-safety block: %q", stdout.String())
	}
}

func TestMemoryGraphQueryPrettyRendering(t *testing.T) {
	out := graphWindowResult{
		WindowIndex:  1,
		StartTimeSec: 100,
		EndTimeSec:   86500,
		Data: map[string]interface{}{
			"nodes": []interface{}{map[string]interface{}{
				"node_id": "node-1", "node_type": json.Number("1"), "source_id": "source-1", "event_time_sec": json.Number("100"),
				"detail": map[string]interface{}{"format": "markdown", "schema_version": "v1", "content": "node detail"},
			}},
			"edges": []interface{}{map[string]interface{}{
				"edge_id": "edge-1", "relation_type": "follows", "from_node_id": "node-1", "to_node_id": "node-2", "event_time_sec": json.Number("101"),
				"detail": map[string]interface{}{"format": "json", "schema_version": "v1", "content": "{\"key\":\"value\"}"},
			}},
		},
	}
	var got bytes.Buffer
	if err := renderGraphWindowPretty(&got, out); err != nil {
		t.Fatalf("render pretty: %v", err)
	}
	want := "Window 1 [100, 86500)\n" +
		"Nodes (1)\n" +
		"- node_id: node-1\n" +
		"  node_type: 1\n" +
		"  source_id: source-1\n" +
		"  event_time_sec: 100\n" +
		"  detail (markdown, v1):\n" +
		"    node detail\n" +
		"Edges (1)\n" +
		"- edge_id: edge-1\n" +
		"  relation_type: follows\n" +
		"  from_node_id: node-1\n" +
		"  to_node_id: node-2\n" +
		"  event_time_sec: 101\n" +
		"  detail (json, v1):\n" +
		"    {\"key\":\"value\"}\n"
	if got.String() != want {
		t.Fatalf("pretty output mismatch\n--- got ---\n%s--- want ---\n%s", got.String(), want)
	}
}

func TestMemoryGraphQueryPrettyHandlesMalformedOptionalFields(t *testing.T) {
	out := graphWindowResult{
		WindowIndex:  2,
		StartTimeSec: 86500,
		EndTimeSec:   86501,
		Data: map[string]interface{}{
			"nodes": []interface{}{map[string]interface{}{
				"node_id": "node-safe", "node_type": json.Number("2"), "source_id": "source-safe",
				"root_id": []interface{}{"bad"}, "graph_date": map[string]interface{}{"bad": true}, "event_time_sec": []interface{}{json.Number("1")},
				"detail": map[string]interface{}{"format": []interface{}{}, "schema_version": json.Number("1"), "content": map[string]interface{}{}},
			}},
			"edges": []interface{}{},
		},
	}
	var got bytes.Buffer
	if err := renderGraphWindowPretty(&got, out); err != nil {
		t.Fatalf("render malformed optional fields: %v", err)
	}
	for _, want := range []string{"Window 2 [86500, 86501)", "Nodes (1)", "- node_id: node-safe", "Edges (0)"} {
		if !strings.Contains(got.String(), want) {
			t.Fatalf("pretty output missing %q: %s", want, got.String())
		}
	}
	for _, malformed := range []string{"root_id:", "graph_date:", "event_time_sec:", "detail ("} {
		if strings.Contains(got.String(), malformed) {
			t.Fatalf("pretty output rendered malformed optional field %q: %s", malformed, got.String())
		}
	}

	got.Reset()
	out.Data = map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}}
	if err := renderGraphWindowPretty(&got, out); err != nil {
		t.Fatalf("render empty lists: %v", err)
	}
	if want := "Window 2 [86500, 86501)\nNodes (0)\nEdges (0)\n"; got.String() != want {
		t.Fatalf("empty-list output = %q, want %q", got.String(), want)
	}
}

func TestMemoryGraphQueryGraphScalarUsesDecodedRepresentationsOnly(t *testing.T) {
	tests := []struct {
		name   string
		value  interface{}
		want   string
		wantOK bool
	}{
		{name: "string", value: "node-type", want: "node-type", wantOK: true},
		{name: "integer json number", value: json.Number("101"), want: "101", wantOK: true},
		{name: "decimal json number", value: json.Number("1.5"), want: "1.5", wantOK: true},
		{name: "float64 cannot come from decoder", value: float64(1), wantOK: false},
		{name: "int cannot come from decoder", value: int(1), wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := graphScalar(tc.value)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("graphScalar(%T(%v)) = %q, %v; want %q, %v", tc.value, tc.value, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

type graphWindowOutput struct {
	WindowIndex  int                    `json:"window_index"`
	StartTimeSec int64                  `json:"start_time_sec"`
	EndTimeSec   int64                  `json:"end_time_sec"`
	Data         map[string]interface{} `json:"data"`
}

type graphFailWriter struct {
	err    error
	writes [][]byte
}

type graphErrorTransport struct {
	err error
}

func (t graphErrorTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, t.err
}

type graphDialFailureTransport struct {
	calls int
	cause error
}

func (t *graphDialFailureTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	t.calls++
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: t.cause}
}

// graphFailWriter is written only by the output coordinator and read after the
// command returns, so its per-test recording needs no synchronization.
func (w *graphFailWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, append([]byte(nil), p...))
	return 0, w.err
}

func (w *graphFailWriter) Writes() [][]byte {
	return w.writes
}

type memoryGraphSafetyProvider struct {
	alert *extcs.Alert
}

func (p *memoryGraphSafetyProvider) Name() string { return "memory-graph-test" }

func (p *memoryGraphSafetyProvider) Scan(_ context.Context, _ extcs.ScanRequest) (*extcs.Alert, error) {
	return p.alert, nil
}

type memoryGraphCancelingSafetyProvider struct {
	cancel context.CancelFunc
	scans  int
}

func (p *memoryGraphCancelingSafetyProvider) Name() string { return "memory-graph-cancel-test" }

func (p *memoryGraphCancelingSafetyProvider) Scan(_ context.Context, _ extcs.ScanRequest) (*extcs.Alert, error) {
	p.scans++
	p.cancel()
	return nil, nil
}

func memoryGraphTestFactory(t *testing.T) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer, *httpmock.Registry) {
	t.Helper()
	config := memoryTestConfig(t)
	f, stdout, stderr, _ := cmdutil.TestFactory(t, config)

	reg := &httpmock.Registry{}
	httpClient := httpmock.NewClient(reg)
	sdk := lark.NewClient(
		config.AppID,
		credential.RuntimeAppSecret(config.AppSecret),
		lark.WithEnableTokenCache(false),
		lark.WithLogLevel(larkcore.LogLevelError),
		lark.WithHttpClient(httpClient),
		lark.WithOpenBaseUrl(core.ResolveOpenBaseURL(config.Brand)),
	)
	f.HttpClient = func() (*http.Client, error) { return httpClient, nil }
	f.LarkClient = func() (*lark.Client, error) { return sdk, nil }
	return f, stdout, stderr, reg
}

func graphQueryStub(node, edge map[string]interface{}) *httpmock.Stub {
	return &httpmock.Stub{
		Method: http.MethodPost,
		URL:    graphQueryPath,
		Body: map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"nodes": []interface{}{node},
					"edges": []interface{}{edge},
				},
			},
		},
	}
}

func graphRequestMatchesWindow(body []byte, start, end int64) bool {
	var request struct {
		TimeRange struct {
			StartTimeSec int64 `json:"start_time_sec"`
			EndTimeSec   int64 `json:"end_time_sec"`
		} `json:"time_range"`
	}
	return json.Unmarshal(body, &request) == nil &&
		request.TimeRange.StartTimeSec == start &&
		request.TimeRange.EndTimeSec == end
}

func graphQueryStubForWindow(start, end int64, node, edge map[string]interface{}) *httpmock.Stub {
	stub := graphQueryStub(node, edge)
	stub.BodyFilter = func(body []byte) bool {
		return graphRequestMatchesWindow(body, start, end)
	}
	return stub
}

func graphNode(nodeID, sourceID string, eventTimeSec int64) map[string]interface{} {
	return map[string]interface{}{
		"node_id":        nodeID,
		"node_type":      1,
		"source_id":      sourceID,
		"root_id":        "root-1",
		"graph_date":     "2026-08-26",
		"event_time_sec": eventTimeSec,
		"detail": map[string]interface{}{
			"format":         "markdown",
			"schema_version": "v1",
			"content":        "complete node detail",
		},
	}
}

func graphEdge(edgeID, fromNodeID, toNodeID string, eventTimeSec int64) map[string]interface{} {
	return map[string]interface{}{
		"edge_id":        edgeID,
		"relation_type":  "follows",
		"from_node_id":   fromNodeID,
		"to_node_id":     toNodeID,
		"event_time_sec": eventTimeSec,
		"detail": map[string]interface{}{
			"format":         "json",
			"schema_version": "v1",
			"content":        "{\"key\":\"value\"}",
		},
	}
}

func assertGraphRequestWindow(t *testing.T, body []byte, start, end int64) {
	t.Helper()
	var request struct {
		UserID    string `json:"user_id"`
		TimeRange struct {
			StartTimeSec int64 `json:"start_time_sec"`
			EndTimeSec   int64 `json:"end_time_sec"`
		} `json:"time_range"`
		Params struct {
			DetailFormat string `json:"detail_format"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	if request.UserID != "ou_graph_test_user" || request.TimeRange.StartTimeSec != start || request.TimeRange.EndTimeSec != end || request.Params.DetailFormat != "markdown" {
		t.Fatalf("request = user:%q range:[%d,%d) detail:%q, want user:ou_graph_test_user range:[%d,%d) detail:markdown", request.UserID, request.TimeRange.StartTimeSec, request.TimeRange.EndTimeSec, request.Params.DetailFormat, start, end)
	}
}

func graphOutputLines(t *testing.T, stdout *bytes.Buffer) []string {
	t.Helper()
	trimmed := strings.TrimSuffix(stdout.String(), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func decodeGraphWindow(t *testing.T, data interface{}) graphWindowOutput {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal graph window: %v", err)
	}
	var window graphWindowOutput
	if err := json.Unmarshal(raw, &window); err != nil {
		t.Fatalf("decode graph window: %v\n%s", err, raw)
	}
	return window
}

func firstGraphNodeID(t *testing.T, data map[string]interface{}) string {
	t.Helper()
	nodes, ok := data["nodes"].([]interface{})
	if !ok || len(nodes) != 1 {
		t.Fatalf("nodes = %#v, want one node", data["nodes"])
	}
	node, ok := nodes[0].(map[string]interface{})
	if !ok {
		t.Fatalf("node = %#v, want object", nodes[0])
	}
	nodeID, _ := node["node_id"].(string)
	return nodeID
}
