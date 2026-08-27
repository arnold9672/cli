// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"strconv"
	"strings"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const (
	graphQueryWindowSec  int64 = 24 * 60 * 60
	graphQueryMaxSpanSec int64 = 7 * graphQueryWindowSec
)

type graphTimeWindow struct {
	Index        int   `json:"window_index"`
	StartTimeSec int64 `json:"start_time_sec"`
	EndTimeSec   int64 `json:"end_time_sec"`
}

type graphQuerySpec struct {
	UserID       string
	DetailFormat string
	Windows      []graphTimeWindow
}

func parseGraphQuerySpec(detailFormat, startTimeSec, endTimeSec string) (graphQuerySpec, error) {
	detailFormat = strings.TrimSpace(detailFormat)
	if detailFormat != "markdown" && detailFormat != "json" {
		return graphQuerySpec{}, common.ValidationErrorf("invalid --detail-format: must be markdown or json").
			WithParam("--detail-format")
	}

	start, err := parseGraphQueryTime(startTimeSec, "--start-time-sec")
	if err != nil {
		return graphQuerySpec{}, err
	}
	if start < 0 {
		return graphQuerySpec{}, common.ValidationErrorf("invalid --start-time-sec: must be non-negative").
			WithParam("--start-time-sec")
	}

	end, err := parseGraphQueryTime(endTimeSec, "--end-time-sec")
	if err != nil {
		return graphQuerySpec{}, err
	}
	if end <= start {
		return graphQuerySpec{}, common.ValidationErrorf("invalid --end-time-sec: must be greater than --start-time-sec").
			WithParam("--end-time-sec")
	}
	if end-start > graphQueryMaxSpanSec {
		return graphQuerySpec{}, common.ValidationErrorf("invalid graph query range: span must be at most %d seconds", graphQueryMaxSpanSec).
			WithParam("--end-time-sec")
	}

	return graphQuerySpec{
		DetailFormat: detailFormat,
		Windows:      splitGraphQueryWindows(start, end),
	}, nil
}

func bindCurrentGraphUser(spec graphQuerySpec, rctx *common.RuntimeContext) (graphQuerySpec, error) {
	userID := ""
	if rctx != nil && rctx.Config != nil {
		userID = strings.TrimSpace(rctx.UserOpenId())
	}
	if !strings.HasPrefix(userID, "ou_") {
		return graphQuerySpec{}, errs.NewAuthenticationError(
			errs.SubtypeTokenMissing,
			"current user open_id is unavailable",
		).WithHint("run `lark-memory-cli auth login --scope \"memory:hub\"` and retry")
	}
	spec.UserID = userID
	return spec, nil
}

func parseGraphQueryTime(value, flag string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, common.ValidationErrorf("%s is required", flag).WithParam(flag)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, common.ValidationErrorf("invalid %s: must be an int64", flag).
			WithParam(flag)
	}
	return parsed, nil
}

func splitGraphQueryWindows(start, end int64) []graphTimeWindow {
	windows := make([]graphTimeWindow, 0, 7)
	for start < end {
		size := end - start
		if size > graphQueryWindowSec {
			size = graphQueryWindowSec
		}
		windowEnd := start + size
		windows = append(windows, graphTimeWindow{
			Index:        len(windows) + 1,
			StartTimeSec: start,
			EndTimeSec:   windowEnd,
		})
		start = windowEnd
	}
	return windows
}
