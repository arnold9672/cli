// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/client"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const graphQueryPath = memoryAPIBasePath + "/graph_query"

// MemoryGraphQuery plans graph queries in one request per 24-hour window.
var MemoryGraphQuery = common.Shortcut{
	Service:     memoryService,
	Command:     "+graph-query",
	Description: "Query Memory Graph data with per-day streaming output",
	Risk:        "read",
	Scopes:      []string{memoryScope},
	AuthTypes:   []string{"user"},
	Flags: []common.Flag{
		{Name: "start-time-sec", Desc: "inclusive Unix-second start", Required: true},
		{Name: "end-time-sec", Desc: "exclusive Unix-second end", Required: true},
		{Name: "detail-format", Default: "markdown", Desc: "Graph detail format", Enum: []string{"markdown", "json"}},
	},
	Preflight: validateGraphQueryRawFlags,
	Validate: func(ctx context.Context, rctx *common.RuntimeContext) error {
		spec, err := validateGraphQueryInput(
			rctx.Str("detail-format"),
			rctx.Str("start-time-sec"),
			rctx.Str("end-time-sec"),
			rctx.JqExpr,
			rctx.Format,
		)
		if err != nil {
			return err
		}
		_, err = bindCurrentGraphUser(spec, rctx)
		return err
	},
	DryRun: func(ctx context.Context, rctx *common.RuntimeContext) *common.DryRunAPI {
		spec, err := parseGraphQuerySpec(
			rctx.Str("detail-format"),
			rctx.Str("start-time-sec"),
			rctx.Str("end-time-sec"),
		)
		if err != nil {
			// Validate runs before DryRun. Keep this defensive branch non-panicking
			// for direct hook callers if that framework invariant ever changes.
			return common.NewDryRunAPI()
		}
		spec, err = bindCurrentGraphUser(spec, rctx)
		if err != nil {
			return common.NewDryRunAPI()
		}

		dryRun := common.NewDryRunAPI()
		for _, window := range spec.Windows {
			dryRun.POST(graphQueryPath).
				Desc(fmt.Sprintf("Query Memory Graph data for window %d", window.Index)).
				Body(buildGraphQueryBody(spec, window))
		}
		return dryRun
	},
	Execute: func(ctx context.Context, rctx *common.RuntimeContext) error {
		spec, err := parseGraphQuerySpec(
			rctx.Str("detail-format"),
			rctx.Str("start-time-sec"),
			rctx.Str("end-time-sec"),
		)
		if err != nil {
			return err
		}
		spec, err = bindCurrentGraphUser(spec, rctx)
		if err != nil {
			return err
		}
		return executeGraphQuery(rctx, spec)
	},
}

func validateGraphQueryRawFlags(_ context.Context, cmd *cobra.Command) error {
	detailFormat, _ := cmd.Flags().GetString("detail-format")
	startTimeSec, _ := cmd.Flags().GetString("start-time-sec")
	endTimeSec, _ := cmd.Flags().GetString("end-time-sec")
	jqExpr, _ := cmd.Flags().GetString("jq")
	format, _ := cmd.Flags().GetString("format")
	spec, err := validateGraphQueryInput(detailFormat, startTimeSec, endTimeSec, jqExpr, format)
	if err != nil {
		return err
	}
	if detailFormat != spec.DetailFormat {
		if err := cmd.Flags().Set("detail-format", spec.DetailFormat); err != nil {
			return errs.NewInternalError(errs.SubtypeUnknown, "failed to normalize --detail-format").
				WithCause(err)
		}
	}
	return nil
}

func validateGraphQueryInput(detailFormat, startTimeSec, endTimeSec, jqExpr, format string) (graphQuerySpec, error) {
	spec, err := parseGraphQuerySpec(detailFormat, startTimeSec, endTimeSec)
	if err != nil {
		return graphQuerySpec{}, err
	}
	if jqExpr != "" {
		return graphQuerySpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument, "--jq is not supported for memory +graph-query").
			WithParam("--jq")
	}
	switch format {
	case "json", "ndjson", "pretty":
		return spec, nil
	default:
		return graphQuerySpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument, "--format must be json, ndjson, or pretty for memory +graph-query").
			WithParam("--format")
	}
}

func buildGraphQueryBody(spec graphQuerySpec, window graphTimeWindow) map[string]interface{} {
	return map[string]interface{}{
		"user_id": spec.UserID,
		"time_range": map[string]interface{}{
			"start_time_sec": window.StartTimeSec,
			"end_time_sec":   window.EndTimeSec,
		},
		"params": map[string]interface{}{"detail_format": spec.DetailFormat},
	}
}

func executeGraphQuery(rctx *common.RuntimeContext, spec graphQuerySpec) error {
	call := func(callCtx context.Context, window graphTimeWindow) (map[string]interface{}, error) {
		return callMemoryAPITypedWithContext(
			rctx,
			client.WithSingleTransportAttempt(callCtx),
			http.MethodPost,
			graphQueryPath,
			buildGraphQueryBody(spec, window),
		)
	}
	emit := func(out graphWindowResult) error {
		var renderErr error
		if err := rctx.OutStream(out, func(w io.Writer) {
			renderErr = renderGraphWindowPretty(w, out)
		}); err != nil {
			return err
		}
		return renderErr
	}
	return runGraphQueryWindows(rctx.Ctx(), spec.Windows, call, emit, graphQueryRetryInterval)
}

func annotateGraphWindowError(err error, window graphTimeWindow, attempts int) error {
	suffix := fmt.Sprintf(
		"failed window: window_index=%d start_time_sec=%d end_time_sec=%d attempts=%d",
		window.Index,
		window.StartTimeSec,
		window.EndTimeSec,
		attempts,
	)
	if problem, ok := errs.ProblemOf(err); ok {
		problem.Message = "graph query request failed"
		problem.Hint = suffix
		var authErr *errs.AuthenticationError
		if errors.As(err, &authErr) {
			authErr.UserOpenID = ""
		}
		return err
	}
	return errs.NewInternalError(errs.SubtypeUnknown, "graph query window failed").
		WithHint("%s", suffix).
		WithCause(err)
}
