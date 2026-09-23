// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const (
	graphSearchService                     = memoryService
	graphSearchCommand                     = "+graph-search"
	graphSearchKnowledgeQAURL              = "https://lgadymoe.fn.bytedance.net/knowledge_qa/search"
	graphSearchIdentityURL                 = "https://lgadymoe.fn.bytedance.net/knowledge_qa/out_id_to_in_id"
	graphSearchUserInfoPath                = "/open-apis/authen/v1/user_info"
	graphSearchOpenIDType            int64 = 3
	graphSearchTenantID              int64 = 1
	graphSearchAppID                 int64 = 1234
	graphSearchDefaultConcurrency          = 8
	graphSearchTimezoneOffsetSec           = 8 * 60 * 60
	graphSearchDefaultTTEnv                = defaultMemoryTTEnv
	graphSearchGraphQueryModeAuto          = "auto"
	graphSearchGraphQueryModeOn            = "on"
	graphSearchGraphQueryModeOff           = "off"
	graphSearchGraphQueryDefaultDays       = 7
)

type graphSearchInput struct {
	Query                  string
	Concurrency            int
	DetailFormat           string
	Format                 string
	MemoryTTEnv            string
	GraphQueryMode         string
	GraphQueryModeSet      bool
	GraphQueryLookbackDays int
	GraphQueryLookbackSet  bool
}

type graphSearchSpec struct {
	Query                  string
	Concurrency            int
	DetailFormat           string
	InternalUserID         int64
	GraphUserID            string
	TraceID                string
	MemoryTTEnv            string
	GraphQueryMode         string
	GraphQueryLookbackDays int
	RequestLimiter         chan struct{}
	WikiResolver           *graphSearchWikiResolver
}

// MemoryGraphSearch turns a natural-language query into stable Memory Graph roots
// through the intranet-only Knowledge QA endpoint, then performs the initial
// OneHop request. Agents explicitly control every later hop through
// memory +graph-one-hop.
var MemoryGraphSearch = common.Shortcut{
	Service:     graphSearchService,
	Command:     graphSearchCommand,
	Description: "Search intranet knowledge and traverse related Memory Graph nodes",
	Risk:        "read",
	Scopes:      []string{memoryScope},
	ConditionalUserScopes: []string{
		"wiki:node:retrieve",
	},
	AuthTypes: []string{"user"},
	Flags: []common.Flag{
		{Name: "query", Desc: "natural-language intranet knowledge query", Required: true},
		{Name: "concurrency", Type: "int", Default: strconv.Itoa(graphSearchDefaultConcurrency), Desc: "maximum concurrent Graph Search network requests across all stages"},
		{Name: "detail-format", Default: "markdown", Desc: "Graph detail format", Enum: []string{"markdown", "json"}},
		{Name: "graph-query-mode", Default: graphSearchGraphQueryModeOn, Desc: "time-window GraphQuery mode; defaults on so every query attempts Graph memory recall", Enum: []string{graphSearchGraphQueryModeAuto, graphSearchGraphQueryModeOn, graphSearchGraphQueryModeOff}},
		{Name: "graph-query-lookback-days", Type: "int", Default: strconv.Itoa(graphSearchGraphQueryDefaultDays), Desc: "fallback rolling GraphQuery lookback when enabled (1-7); exact query time ranges take precedence"},
	},
	Tips: []string{
		"ByteDance intranet only. The current UAT is verified and converted to the internal UID automatically.",
		"Graph Search business requests default to x-tt-env=ppe_memory_hub; LARKSUITE_CLI_MEMORY_TT_ENV overrides it for this process.",
		"Minutes are returned as skipped candidates because Knowledge QA does not expose a stable MEETING root.",
		"GraphQuery nodes with verified NodeType and RootID are returned as next_roots for Agent selection; they are never auto-expanded.",
		"Graph Search always performs the initial OneHop only. Inspect node/edge Detail and next_roots before issuing memory +graph-one-hop.",
		"Before Knowledge QA, Graph Search verifies the UAT with user_info and derives its internal UID through the intranet ID conversion service.",
	},
	Preflight: validateGraphSearchRawFlags,
	Validate: func(_ context.Context, rctx *common.RuntimeContext) error {
		if _, err := graphSearchSpecFromRuntime(rctx, uuid.NewString()); err != nil {
			return err
		}
		// Dry-run only renders a redacted request plan and performs no business
		// I/O, so it must remain usable before login. Real execution validates
		// and refreshes the UAT before Knowledge QA or Graph can be called.
		if !rctx.Bool("dry-run") {
			if _, err := rctx.AccessToken(); err != nil {
				return annotateGraphSearchAuthPreflightError(err)
			}
		}
		return nil
	},
	DryRun: func(_ context.Context, rctx *common.RuntimeContext) *common.DryRunAPI {
		spec, err := graphSearchSpecFromRuntime(rctx, "dry-run-trace")
		if err != nil {
			return common.NewDryRunAPI().Set("error", err.Error())
		}
		return buildGraphSearchDryRun(spec)
	},
	Execute: func(_ context.Context, rctx *common.RuntimeContext) error {
		spec, err := graphSearchSpecFromRuntime(rctx, uuid.NewString())
		if err != nil {
			return err
		}
		spec, err = resolveGraphSearchIdentity(rctx.Ctx(), rctx, spec)
		if err != nil {
			return err
		}
		return executeGraphSearch(rctx, spec)
	},
}

func annotateGraphSearchAuthPreflightError(err error) error {
	if problem, ok := errs.ProblemOf(err); ok {
		problem.Message = "Graph Search user access token preflight failed"
		problem.Hint = "run `lark-memory-cli auth login --scope \"memory:hub wiki:node:retrieve\"` and retry"
		return err
	}
	return errs.NewAuthenticationError(errs.SubtypeTokenInvalid,
		"Graph Search user access token preflight failed").
		WithHint("run `lark-memory-cli auth login --scope \"memory:hub wiki:node:retrieve\"` and retry").
		WithCause(err)
}

func acquireGraphSearchRequestPermit(ctx context.Context, spec graphSearchSpec) (func(), error) {
	if spec.RequestLimiter == nil {
		return func() {}, nil
	}
	select {
	case spec.RequestLimiter <- struct{}{}:
		return func() { <-spec.RequestLimiter }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func validateGraphSearchRawFlags(_ context.Context, cmd *cobra.Command) error {
	input := graphSearchInputFromCommand(cmd)
	if _, err := parseGraphSearchSpec(input, "preflight-trace"); err != nil {
		return err
	}
	for _, name := range []string{"query", "detail-format", "graph-query-mode"} {
		raw, _ := cmd.Flags().GetString(name)
		normalized := strings.TrimSpace(raw)
		if raw == normalized {
			continue
		}
		if err := cmd.Flags().Set(name, normalized); err != nil {
			return errs.NewInternalError(errs.SubtypeUnknown, "failed to normalize --%s", name).WithCause(err)
		}
	}
	return nil
}

func graphSearchInputFromCommand(cmd *cobra.Command) graphSearchInput {
	query, _ := cmd.Flags().GetString("query")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	detailFormat, _ := cmd.Flags().GetString("detail-format")
	format, _ := cmd.Flags().GetString("format")
	graphQueryMode, _ := cmd.Flags().GetString("graph-query-mode")
	graphQueryLookbackDays, _ := cmd.Flags().GetInt("graph-query-lookback-days")
	return graphSearchInput{
		Query:                  query,
		Concurrency:            concurrency,
		DetailFormat:           detailFormat,
		Format:                 format,
		MemoryTTEnv:            os.Getenv(envMemoryTTEnv),
		GraphQueryMode:         graphQueryMode,
		GraphQueryModeSet:      cmd.Flags().Changed("graph-query-mode"),
		GraphQueryLookbackDays: graphQueryLookbackDays,
		GraphQueryLookbackSet:  cmd.Flags().Changed("graph-query-lookback-days"),
	}
}

func graphSearchSpecFromRuntime(rctx *common.RuntimeContext, generatedTraceID string) (graphSearchSpec, error) {
	return parseGraphSearchSpec(graphSearchInput{
		Query:                  rctx.Str("query"),
		Concurrency:            rctx.Int("concurrency"),
		DetailFormat:           rctx.Str("detail-format"),
		Format:                 rctx.Format,
		MemoryTTEnv:            os.Getenv(envMemoryTTEnv),
		GraphQueryMode:         rctx.Str("graph-query-mode"),
		GraphQueryModeSet:      rctx.Changed("graph-query-mode"),
		GraphQueryLookbackDays: rctx.Int("graph-query-lookback-days"),
		GraphQueryLookbackSet:  rctx.Changed("graph-query-lookback-days"),
	}, generatedTraceID)
}

func parseGraphSearchSpec(input graphSearchInput, generatedTraceID string) (graphSearchSpec, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return graphSearchSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument, "--query is required").WithParam("--query")
	}
	if input.Concurrency <= 0 {
		return graphSearchSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"invalid --concurrency: must be positive").WithParam("--concurrency")
	}
	detailFormat := strings.TrimSpace(input.DetailFormat)
	if detailFormat != "markdown" && detailFormat != "json" {
		return graphSearchSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"invalid --detail-format: must be markdown or json").WithParam("--detail-format")
	}
	if input.Format != "json" && input.Format != "pretty" {
		return graphSearchSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"--format must be json or pretty for memory +graph-search").WithParam("--format")
	}
	memoryTTEnv := strings.TrimSpace(input.MemoryTTEnv)
	if memoryTTEnv == "" {
		memoryTTEnv = graphSearchDefaultTTEnv
	}
	if len(memoryTTEnv) > 256 || strings.ContainsAny(memoryTTEnv, "\r\n") {
		return graphSearchSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"%s must be a valid single-line header value", envMemoryTTEnv)
	}
	graphQueryMode := strings.TrimSpace(input.GraphQueryMode)
	if graphQueryMode == "" && !input.GraphQueryModeSet {
		graphQueryMode = graphSearchGraphQueryModeOn
	}
	if graphQueryMode != graphSearchGraphQueryModeAuto && graphQueryMode != graphSearchGraphQueryModeOn && graphQueryMode != graphSearchGraphQueryModeOff {
		return graphSearchSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"invalid --graph-query-mode: must be auto, on, or off").WithParam("--graph-query-mode")
	}
	graphQueryLookbackDays := input.GraphQueryLookbackDays
	if graphQueryLookbackDays == 0 && !input.GraphQueryLookbackSet {
		graphQueryLookbackDays = graphSearchGraphQueryDefaultDays
	}
	if graphQueryLookbackDays < 1 || graphQueryLookbackDays > 7 {
		return graphSearchSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"invalid --graph-query-lookback-days: must be between 1 and 7").WithParam("--graph-query-lookback-days")
	}

	traceID := strings.TrimSpace(generatedTraceID)
	if traceID == "" {
		return graphSearchSpec{}, errs.NewInternalError(errs.SubtypeUnknown, "failed to generate Graph Search trace ID")
	}

	return graphSearchSpec{
		Query:                  query,
		Concurrency:            input.Concurrency,
		DetailFormat:           detailFormat,
		TraceID:                traceID,
		MemoryTTEnv:            memoryTTEnv,
		GraphQueryMode:         graphQueryMode,
		GraphQueryLookbackDays: graphQueryLookbackDays,
	}, nil
}

func buildGraphSearchDryRun(spec graphSearchSpec) *common.DryRunAPI {
	const (
		openIDPlaceholder = "<from user_info.open_id>"
		uidPlaceholder    = "<from out_id_to_in_id_map>"
	)
	return common.NewDryRunAPI().
		GET(graphSearchUserInfoPath).
		Desc("[1] Verify the current UAT and obtain its open_id").
		POST(graphSearchIdentityURL).
		Desc("[2] Convert the UAT-derived open_id to the internal UID on the selected x-tt-env").
		Body(buildGraphSearchIdentityBody(openIDPlaceholder)).
		POST(graphSearchKnowledgeQAURL).
		Desc("[3] Search ByteDance intranet knowledge with the converted UID; dynamic Wiki and OneHop calls depend on this response").
		Body(buildGraphSearchKnowledgeBody(spec.Query, uidPlaceholder)).
		Set("identity_conversion_headers", map[string]string{"X-Tt-Env": spec.MemoryTTEnv}).
		Set("knowledge_qa_headers", buildGraphSearchKnowledgeHeaders(uidPlaceholder, spec.MemoryTTEnv)).
		Set("dynamic_steps", []string{
			"when enabled, query the current user's time-window Graph in parallel with Knowledge QA using the same x-tt-env",
			"resolve Wiki node_token values with GET /open-apis/wiki/v2/spaces/get_node",
			"group stable roots by Asia/Shanghai graph_date",
			"call /open-apis/search/v2/memory_hub/one_hop exactly once for the initial roots",
			"expose verified GraphQuery nodes as next_roots without auto-expanding them",
		}).
		Set("concurrency", spec.Concurrency).
		Set("detail_format", spec.DetailFormat).
		Set("one_hop_tt_env", spec.MemoryTTEnv).
		Set("graph_query", buildGraphSearchGraphQueryDryRun(spec))
}
