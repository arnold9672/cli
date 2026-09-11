// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/spf13/cobra"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/client"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const (
	graphOneHopPath         = memoryAPIBasePath + "/one_hop"
	graphOneHopDefaultDays  = 7
	graphOneHopDaySec       = int64(24 * 60 * 60)
	graphOneHopUserNodeType = int64(4)
	graphOneHopCalendarType = int64(5)
	graphOneHopDefaultScene = "graphcli"
	graphOneHopMinHop       = 1
	graphOneHopMaxHop       = 5
)

type graphOneHopRoot struct {
	NodeType int64  `json:"node_type"`
	RootID   string `json:"root_id"`
}

type graphOneHopSpec struct {
	UserID        string
	Roots         []graphOneHopRoot
	StartTimeSec  int64
	EndTimeSec    int64
	NodeTypes     []int64
	RelationTypes []string
	DetailFormat  string
	Hop           int
	TraceID       string
	Scene         string
}

type graphOneHopInput struct {
	Roots         []string
	LookbackDays  int
	NodeTypes     []int
	RelationTypes []string
	DetailFormat  string
	Hop           int
	HopSet        bool
	TraceID       string
	Scene         string
	Format        string
}

// MemoryOneHop returns the timeline nodes and one-hop neighbours for one or
// more stable Memory Graph roots. The API itself performs exactly one graph
// expansion; callers decide whether to issue another command for the next hop.
var MemoryOneHop = common.Shortcut{
	Service:     memoryService,
	Command:     "+one-hop",
	Description: "Query one-hop Memory Graph data from stable roots",
	Risk:        "read",
	Scopes:      []string{memoryScope},
	AuthTypes:   []string{"user"},
	Flags: []common.Flag{
		{Name: "root", Type: "string_array", Desc: "query root as <node_type>:<root_id>; repeat for multiple roots", Required: true},
		{Name: "lookback-days", Type: "int", Default: strconv.Itoa(graphOneHopDefaultDays), Desc: "rolling lookback window in days"},
		{Name: "node-type", Type: "int_array", Desc: "target node type filter; repeat or use CSV; USER (4) is forbidden"},
		{Name: "relation-type", Type: "string_array", Desc: "relation type filter; repeat for multiple relation types"},
		{Name: "detail-format", Default: "markdown", Desc: "Graph detail format", Enum: []string{"markdown", "json"}},
		{Name: "hop", Type: "int", Desc: "current Agent traversal hop (1-5); the API still expands exactly one hop", Required: true},
		{Name: "trace-id", Desc: "trace ID shared by commands in the same Agent traversal; generated when omitted"},
		{Name: "scene", Default: graphOneHopDefaultScene, Desc: "fixed Graph caller scene", Enum: []string{graphOneHopDefaultScene}},
	},
	Preflight: validateGraphOneHopRawFlags,
	Validate: func(_ context.Context, rctx *common.RuntimeContext) error {
		if _, err := graphOneHopSpecFromRuntime(rctx, time.Now(), uuid.NewString()); err != nil {
			return err
		}
		_, err := currentGraphUserID(rctx)
		return err
	},
	DryRun: func(_ context.Context, rctx *common.RuntimeContext) *common.DryRunAPI {
		spec, err := graphOneHopSpecFromRuntime(rctx, time.Now(), uuid.NewString())
		if err != nil {
			return common.NewDryRunAPI()
		}
		spec.UserID, err = currentGraphUserID(rctx)
		if err != nil {
			return common.NewDryRunAPI()
		}
		return common.NewDryRunAPI().
			POST(graphOneHopPath).
			Desc("Query one-hop Memory Graph data").
			Body(buildGraphOneHopBody(spec))
	},
	Execute: func(_ context.Context, rctx *common.RuntimeContext) error {
		spec, err := graphOneHopSpecFromRuntime(rctx, time.Now(), uuid.NewString())
		if err != nil {
			return err
		}
		spec.UserID, err = currentGraphUserID(rctx)
		if err != nil {
			return err
		}
		return executeGraphOneHop(rctx, spec)
	},
}

func validateGraphOneHopRawFlags(_ context.Context, cmd *cobra.Command) error {
	input := graphOneHopInputFromCommand(cmd)
	if _, err := parseGraphOneHopSpec(input, time.Now(), "preflight-trace"); err != nil {
		return err
	}
	for _, name := range []string{"detail-format", "scene"} {
		raw, _ := cmd.Flags().GetString(name)
		normalized := strings.TrimSpace(raw)
		if raw == normalized {
			continue
		}
		if err := cmd.Flags().Set(name, normalized); err != nil {
			return errs.NewInternalError(errs.SubtypeUnknown, "failed to normalize --%s", name).
				WithCause(err)
		}
	}
	return nil
}

func graphOneHopInputFromCommand(cmd *cobra.Command) graphOneHopInput {
	roots, _ := cmd.Flags().GetStringArray("root")
	lookbackDays, _ := cmd.Flags().GetInt("lookback-days")
	nodeTypes, _ := cmd.Flags().GetIntSlice("node-type")
	relationTypes, _ := cmd.Flags().GetStringArray("relation-type")
	detailFormat, _ := cmd.Flags().GetString("detail-format")
	hop, _ := cmd.Flags().GetInt("hop")
	traceID, _ := cmd.Flags().GetString("trace-id")
	scene, _ := cmd.Flags().GetString("scene")
	format, _ := cmd.Flags().GetString("format")
	return graphOneHopInput{
		Roots:         roots,
		LookbackDays:  lookbackDays,
		NodeTypes:     nodeTypes,
		RelationTypes: relationTypes,
		DetailFormat:  detailFormat,
		Hop:           hop,
		HopSet:        cmd.Flags().Changed("hop"),
		TraceID:       traceID,
		Scene:         scene,
		Format:        format,
	}
}

func graphOneHopSpecFromRuntime(rctx *common.RuntimeContext, now time.Time, generatedTraceID string) (graphOneHopSpec, error) {
	return parseGraphOneHopSpec(graphOneHopInput{
		Roots:         rctx.StrArray("root"),
		LookbackDays:  rctx.Int("lookback-days"),
		NodeTypes:     rctx.IntArray("node-type"),
		RelationTypes: rctx.StrArray("relation-type"),
		DetailFormat:  rctx.Str("detail-format"),
		Hop:           rctx.Int("hop"),
		HopSet:        rctx.Changed("hop"),
		TraceID:       rctx.Str("trace-id"),
		Scene:         rctx.Str("scene"),
		Format:        rctx.Format,
	}, now, generatedTraceID)
}

func parseGraphOneHopSpec(input graphOneHopInput, now time.Time, generatedTraceID string) (graphOneHopSpec, error) {
	detailFormat := strings.TrimSpace(input.DetailFormat)
	if detailFormat != "markdown" && detailFormat != "json" {
		return graphOneHopSpec{}, common.ValidationErrorf("invalid --detail-format: must be markdown or json").
			WithParam("--detail-format")
	}
	scene := strings.TrimSpace(input.Scene)
	if scene != graphOneHopDefaultScene {
		return graphOneHopSpec{}, common.ValidationErrorf("invalid --scene: must be graphcli").
			WithParam("--scene")
	}
	if input.Format != "json" && input.Format != "pretty" {
		return graphOneHopSpec{}, common.ValidationErrorf("--format must be json or pretty for memory +one-hop").
			WithParam("--format")
	}
	if len(input.Roots) == 0 {
		return graphOneHopSpec{}, common.ValidationErrorf("--root is required").WithParam("--root")
	}
	roots := make([]graphOneHopRoot, 0, len(input.Roots))
	for _, raw := range input.Roots {
		root, err := parseGraphOneHopRoot(raw)
		if err != nil {
			return graphOneHopSpec{}, err
		}
		roots = append(roots, root)
	}
	if input.LookbackDays <= 0 {
		return graphOneHopSpec{}, common.ValidationErrorf("invalid --lookback-days: must be positive").
			WithParam("--lookback-days")
	}
	if int64(input.LookbackDays) > math.MaxInt64/graphOneHopDaySec {
		return graphOneHopSpec{}, common.ValidationErrorf("invalid --lookback-days: value is too large").
			WithParam("--lookback-days")
	}
	lookbackSec := int64(input.LookbackDays) * graphOneHopDaySec
	endTimeSec := now.Unix()
	if endTimeSec < lookbackSec {
		return graphOneHopSpec{}, common.ValidationErrorf("invalid --lookback-days: time range starts before Unix epoch").
			WithParam("--lookback-days")
	}
	if !input.HopSet {
		return graphOneHopSpec{}, common.ValidationErrorf("--hop is required").WithParam("--hop")
	}
	if input.Hop < graphOneHopMinHop || input.Hop > graphOneHopMaxHop {
		return graphOneHopSpec{}, common.ValidationErrorf("invalid --hop: must be between %d and %d", graphOneHopMinHop, graphOneHopMaxHop).
			WithParam("--hop")
	}

	nodeTypes, err := normalizeGraphOneHopNodeTypes(input.NodeTypes)
	if err != nil {
		return graphOneHopSpec{}, err
	}
	relationTypes, err := normalizeGraphOneHopRelationTypes(input.RelationTypes)
	if err != nil {
		return graphOneHopSpec{}, err
	}
	traceID := strings.TrimSpace(input.TraceID)
	if traceID == "" {
		traceID = generatedTraceID
	}
	if traceID == "" {
		return graphOneHopSpec{}, errs.NewInternalError(errs.SubtypeUnknown, "failed to generate OneHop trace ID")
	}

	return graphOneHopSpec{
		Roots:         roots,
		StartTimeSec:  endTimeSec - lookbackSec,
		EndTimeSec:    endTimeSec,
		NodeTypes:     nodeTypes,
		RelationTypes: relationTypes,
		DetailFormat:  detailFormat,
		Hop:           input.Hop,
		TraceID:       traceID,
		Scene:         scene,
	}, nil
}

func parseGraphOneHopRoot(raw string) (graphOneHopRoot, error) {
	typePart, rootID, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok || strings.TrimSpace(typePart) == "" || strings.TrimSpace(rootID) == "" {
		return graphOneHopRoot{}, common.ValidationErrorf("invalid --root: expected <node_type>:<root_id>").
			WithParam("--root")
	}
	nodeType, err := strconv.ParseInt(strings.TrimSpace(typePart), 10, 64)
	if err != nil || nodeType <= 0 {
		return graphOneHopRoot{}, common.ValidationErrorf("invalid --root: node_type must be a positive int64").
			WithParam("--root")
	}
	if nodeType == graphOneHopUserNodeType {
		return graphOneHopRoot{}, common.ValidationErrorf("invalid --root: USER (node_type 4) cannot be a OneHop root").
			WithParam("--root")
	}
	if nodeType == graphOneHopCalendarType {
		return graphOneHopRoot{}, common.ValidationErrorf("invalid --root: CALENDAR (node_type 5) is not supported yet").
			WithParam("--root")
	}
	return graphOneHopRoot{NodeType: nodeType, RootID: strings.TrimSpace(rootID)}, nil
}

func normalizeGraphOneHopNodeTypes(values []int) ([]int64, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		nodeType := int64(value)
		if nodeType <= 0 {
			return nil, common.ValidationErrorf("invalid --node-type: must be a positive integer").
				WithParam("--node-type")
		}
		if nodeType == graphOneHopUserNodeType {
			return nil, common.ValidationErrorf("invalid --node-type: USER (4) is not returnable").
				WithParam("--node-type")
		}
		if nodeType == graphOneHopCalendarType {
			return nil, common.ValidationErrorf("invalid --node-type: CALENDAR (5) is not supported yet").
				WithParam("--node-type")
		}
		if _, ok := seen[nodeType]; ok {
			continue
		}
		seen[nodeType] = struct{}{}
		out = append(out, nodeType)
	}
	return out, nil
}

func normalizeGraphOneHopRelationTypes(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, common.ValidationErrorf("invalid --relation-type: value cannot be empty").
				WithParam("--relation-type")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func buildGraphOneHopBody(spec graphOneHopSpec) map[string]interface{} {
	roots := make([]map[string]interface{}, 0, len(spec.Roots))
	for _, root := range spec.Roots {
		roots = append(roots, map[string]interface{}{
			"node_type": root.NodeType,
			"root_id":   root.RootID,
		})
	}
	filters := make(map[string]interface{}, 2)
	if len(spec.NodeTypes) > 0 {
		filters["node_types"] = spec.NodeTypes
	}
	if len(spec.RelationTypes) > 0 {
		filters["relation_types"] = spec.RelationTypes
	}
	body := map[string]interface{}{
		"user_id": spec.UserID,
		"roots":   roots,
		"time_range": map[string]interface{}{
			"start_time_sec": spec.StartTimeSec,
			"end_time_sec":   spec.EndTimeSec,
		},
		"params": map[string]interface{}{
			"detailFormat": spec.DetailFormat,
		},
	}
	if len(filters) > 0 {
		body["filters"] = filters
	}
	return body
}

func executeGraphOneHop(rctx *common.RuntimeContext, spec graphOneHopSpec) error {
	req := &larkcore.ApiReq{
		HttpMethod: http.MethodPost,
		ApiPath:    graphOneHopPath,
		Body:       buildGraphOneHopBody(spec),
	}
	started := time.Now()
	resp, err := rctx.DoAPIWithHeadersContext(
		client.WithSingleTransportAttempt(rctx.Ctx()),
		req,
		memoryExtraHeaders(),
	)
	if err != nil {
		return annotateGraphOneHopError(err)
	}
	data, err := rctx.ClassifyAPIResponse(resp)
	if err != nil {
		return annotateGraphOneHopError(err)
	}
	out, meta, err := normalizeGraphOneHopResponse(data, spec)
	if err != nil {
		return err
	}
	tookMS := time.Since(started).Milliseconds()
	meta.TookMS = &tookMS
	if resp != nil {
		meta.LogID = strings.TrimSpace(resp.Header.Get("x-tt-logid"))
	}
	var renderErr error
	rctx.OutFormat(out, meta, func(w io.Writer) {
		renderErr = renderGraphOneHopPretty(w, out, meta)
	})
	return renderErr
}

func annotateGraphOneHopError(err error) error {
	if err == nil {
		return nil
	}
	if problem, ok := errs.ProblemOf(err); ok {
		problem.Message = "one-hop request failed"
		var authErr *errs.AuthenticationError
		if errors.As(err, &authErr) {
			authErr.UserOpenID = ""
		}
		return err
	}
	return errs.NewInternalError(errs.SubtypeUnknown, "one-hop request failed").WithCause(err)
}
