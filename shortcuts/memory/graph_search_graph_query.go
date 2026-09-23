// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/client"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const (
	graphSearchRetrievalOneHop     = "one_hop"
	graphSearchRetrievalGraphQuery = "graph_query"
)

var graphSearchTimeIntentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:最近|近|过去)\s*\d+\s*(?:天|日|周|个月|月)`),
	regexp.MustCompile(`\d{4}[-/.年]\d{1,2}(?:[-/.月]\d{1,2}日?)?`),
	regexp.MustCompile(`(?i)\b(?:last|past)\s+\d+\s+(?:day|days|week|weeks|month|months)\b`),
}

var (
	graphSearchExplicitDatePattern    = regexp.MustCompile(`(\d{4})[-/.年](\d{1,2})[-/.月](\d{1,2})日?`)
	graphSearchChineseDurationPattern = regexp.MustCompile(`(?:最近|近|过去)\s*(\d+)\s*(天|日|周|个月|月)`)
	graphSearchEnglishDurationPattern = regexp.MustCompile(`(?i)\b(?:last|past)\s+(\d+)\s+(day|days|week|weeks|month|months)\b`)
)

type graphSearchRetrievalSource struct {
	Kind         string `json:"kind"`
	Hop          int    `json:"hop,omitempty"`
	GraphDate    string `json:"graph_date,omitempty"`
	WindowIndex  int    `json:"window_index,omitempty"`
	StartTimeSec int64  `json:"start_time_sec,omitempty"`
	EndTimeSec   int64  `json:"end_time_sec,omitempty"`
}

type graphSearchGraphQueryPlan struct {
	Enabled       bool
	Mode          string
	TriggerReason string
	LookbackDays  int
	RangeSource   string
	StartTimeSec  int64
	EndTimeSec    int64
	Windows       []graphTimeWindow
}

type graphSearchGraphQueryFailedWindow struct {
	WindowIndex  int    `json:"window_index"`
	StartTimeSec int64  `json:"start_time_sec"`
	EndTimeSec   int64  `json:"end_time_sec"`
	Attempts     int    `json:"attempts"`
	ErrorType    string `json:"error_type,omitempty"`
	ErrorSubtype string `json:"error_subtype,omitempty"`
	ErrorCode    int    `json:"error_code,omitempty"`
	LogID        string `json:"log_id,omitempty"`
	Message      string `json:"message"`
}

type graphSearchGraphQueryResult struct {
	Mode              string                              `json:"mode"`
	Called            bool                                `json:"called"`
	TriggerReason     string                              `json:"trigger_reason"`
	LookbackDays      int                                 `json:"lookback_days"`
	RangeSource       string                              `json:"range_source"`
	StartTimeSec      int64                               `json:"start_time_sec,omitempty"`
	EndTimeSec        int64                               `json:"end_time_sec,omitempty"`
	WindowCount       int                                 `json:"window_count"`
	SuccessfulWindows int                                 `json:"successful_window_count"`
	FailedWindows     []graphSearchGraphQueryFailedWindow `json:"failed_windows"`
	ReturnedNodeCount int                                 `json:"returned_node_count"`
	ReturnedEdgeCount int                                 `json:"returned_edge_count"`
	FilteredNodeCount int                                 `json:"filtered_node_count"`
	FilteredEdgeCount int                                 `json:"filtered_edge_count"`
	UniqueNodeCount   int                                 `json:"unique_node_count"`
	UniqueEdgeCount   int                                 `json:"unique_edge_count"`
	Complete          bool                                `json:"complete"`
	TookMS            int64                               `json:"took_ms"`
	Nodes             []map[string]interface{}            `json:"-"`
	Edges             []map[string]interface{}            `json:"-"`
}

func buildGraphSearchGraphQueryDryRun(spec graphSearchSpec) map[string]interface{} {
	plan := planGraphSearchGraphQuery(spec, time.Now())
	return map[string]interface{}{
		"mode":           spec.GraphQueryMode,
		"enabled":        plan.Enabled,
		"trigger_reason": plan.TriggerReason,
		"lookback_days":  plan.LookbackDays,
		"range_source":   plan.RangeSource,
		"start_time_sec": plan.StartTimeSec,
		"end_time_sec":   plan.EndTimeSec,
		"window_count":   len(plan.Windows),
		"tt_env":         spec.MemoryTTEnv,
		"note":           "GraphQuery runs in parallel; verified roots are exposed as next_roots for Agent selection and are never auto-expanded",
	}
}

func planGraphSearchGraphQuery(spec graphSearchSpec, now time.Time) graphSearchGraphQueryPlan {
	enabled, reason := graphSearchGraphQueryDecision(spec.GraphQueryMode, spec.Query)
	plan := graphSearchGraphQueryPlan{
		Enabled:       enabled,
		Mode:          spec.GraphQueryMode,
		TriggerReason: reason,
		LookbackDays:  spec.GraphQueryLookbackDays,
		RangeSource:   "disabled",
	}
	if !enabled {
		return plan
	}
	start, end, source, supported := inferGraphSearchTimeRange(spec.Query, now, spec.GraphQueryLookbackDays)
	plan.RangeSource = source
	if !supported {
		plan.Enabled = false
		plan.TriggerReason = "time_range_unsupported"
		return plan
	}
	plan.StartTimeSec = start.Unix()
	plan.EndTimeSec = end.Unix()
	plan.LookbackDays = graphSearchRangeDays(plan.StartTimeSec, plan.EndTimeSec)
	plan.Windows = splitGraphQueryWindows(plan.StartTimeSec, plan.EndTimeSec)
	return plan
}

func inferGraphSearchTimeRange(query string, now time.Time, fallbackDays int) (time.Time, time.Time, string, bool) {
	now = now.In(graphSearchLocation)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, graphSearchLocation)
	dates := graphSearchExplicitDatePattern.FindAllStringSubmatch(query, 2)
	if len(dates) > 0 {
		start, ok := parseGraphSearchExplicitDate(dates[0])
		if !ok {
			return time.Time{}, time.Time{}, "explicit_date_invalid", false
		}
		end := start.AddDate(0, 0, 1)
		source := "query_explicit_date"
		if len(dates) == 2 {
			last, ok := parseGraphSearchExplicitDate(dates[1])
			if !ok || last.Before(start) {
				return time.Time{}, time.Time{}, "explicit_date_range_invalid", false
			}
			end = last.AddDate(0, 0, 1)
			source = "query_explicit_date_range"
		}
		return validateGraphSearchInferredRange(start, end, source)
	}

	normalized := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(normalized, "上周") || strings.Contains(normalized, "last week") {
		thisMonday := graphSearchWeekStart(today)
		return validateGraphSearchInferredRange(thisMonday.AddDate(0, 0, -7), thisMonday, "query_last_week")
	}
	if strings.Contains(normalized, "本周") || strings.Contains(normalized, "这周") || strings.Contains(normalized, "this week") {
		return validateGraphSearchInferredRange(graphSearchWeekStart(today), now, "query_this_week")
	}
	if strings.Contains(normalized, "昨天") || strings.Contains(normalized, "昨日") || strings.Contains(normalized, "yesterday") {
		return validateGraphSearchInferredRange(today.AddDate(0, 0, -1), today, "query_yesterday")
	}
	if strings.Contains(normalized, "今天") || strings.Contains(normalized, "今日") || strings.Contains(normalized, "today") {
		return validateGraphSearchInferredRange(today, now, "query_today")
	}
	if days, matched := graphSearchDurationDays(normalized); matched {
		if days < 1 || days > 7 {
			return time.Time{}, time.Time{}, "query_duration_exceeds_limit", false
		}
		return validateGraphSearchInferredRange(now.Add(-time.Duration(days)*24*time.Hour), now, "query_rolling_duration")
	}
	return validateGraphSearchInferredRange(now.Add(-time.Duration(fallbackDays)*24*time.Hour), now, "configured_lookback")
}

func parseGraphSearchExplicitDate(match []string) (time.Time, bool) {
	if len(match) != 4 {
		return time.Time{}, false
	}
	year, yearErr := strconv.Atoi(match[1])
	month, monthErr := strconv.Atoi(match[2])
	day, dayErr := strconv.Atoi(match[3])
	if yearErr != nil || monthErr != nil || dayErr != nil {
		return time.Time{}, false
	}
	parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, graphSearchLocation)
	return parsed, parsed.Year() == year && int(parsed.Month()) == month && parsed.Day() == day
}

func graphSearchWeekStart(day time.Time) time.Time {
	daysSinceMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -daysSinceMonday)
}

func graphSearchDurationDays(query string) (int, bool) {
	if match := graphSearchChineseDurationPattern.FindStringSubmatch(query); len(match) == 3 {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, true
		}
		switch match[2] {
		case "周":
			value *= 7
		case "个月", "月":
			value *= 30
		}
		return value, true
	}
	if match := graphSearchEnglishDurationPattern.FindStringSubmatch(query); len(match) == 3 {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, true
		}
		unit := strings.ToLower(match[2])
		if strings.HasPrefix(unit, "week") {
			value *= 7
		} else if strings.HasPrefix(unit, "month") {
			value *= 30
		}
		return value, true
	}
	return 0, false
}

func validateGraphSearchInferredRange(start, end time.Time, source string) (time.Time, time.Time, string, bool) {
	span := end.Sub(start)
	if span <= 0 || span > time.Duration(graphQueryMaxSpanSec)*time.Second {
		return time.Time{}, time.Time{}, source, false
	}
	return start, end, source, true
}

func graphSearchRangeDays(start, end int64) int {
	span := end - start
	if span <= 0 {
		return 0
	}
	return int((span + graphQueryWindowSec - 1) / graphQueryWindowSec)
}

func graphSearchGraphQueryDecision(mode, query string) (bool, string) {
	switch mode {
	case graphSearchGraphQueryModeOn:
		return true, "forced_on"
	case graphSearchGraphQueryModeOff:
		return false, "forced_off"
	default:
		if graphSearchHasTimeIntent(query) {
			return true, "auto_time_intent"
		}
		return false, "auto_no_time_intent"
	}
}

func graphSearchHasTimeIntent(query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(query))
	for _, marker := range []string{
		"最近", "近期", "近来", "过去", "一段时间", "今天", "今日", "昨天", "昨日", "本周", "这周", "上周",
		"recent", "recently", "latest", "today", "yesterday", "this week", "last week",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	for _, pattern := range graphSearchTimeIntentPatterns {
		if pattern.MatchString(normalized) {
			return true
		}
	}
	return false
}

func executeGraphSearchGraphQuery(ctx context.Context, rctx *common.RuntimeContext, spec graphSearchSpec, plan graphSearchGraphQueryPlan) (graphSearchGraphQueryResult, error) {
	result := graphSearchGraphQueryResult{
		Mode:          plan.Mode,
		Called:        plan.Enabled,
		TriggerReason: plan.TriggerReason,
		LookbackDays:  plan.LookbackDays,
		RangeSource:   plan.RangeSource,
		StartTimeSec:  plan.StartTimeSec,
		EndTimeSec:    plan.EndTimeSec,
		WindowCount:   len(plan.Windows),
		Complete:      true,
		FailedWindows: []graphSearchGraphQueryFailedWindow{},
		Nodes:         []map[string]interface{}{},
		Edges:         []map[string]interface{}{},
	}
	if !plan.Enabled {
		return result, nil
	}
	started := time.Now()
	querySpec := graphQuerySpec{
		UserID:       spec.GraphUserID,
		DetailFormat: spec.DetailFormat,
		Windows:      plan.Windows,
	}
	executions := make([]graphWindowExecution, len(plan.Windows))
	runGraphSearchIndexed(ctx, spec.Concurrency, len(plan.Windows), func(position int) {
		window := plan.Windows[position]
		executions[position] = executeGraphWindow(ctx, position, window, func(callCtx context.Context, window graphTimeWindow) (map[string]interface{}, error) {
			release, err := acquireGraphSearchRequestPermit(callCtx, spec)
			if err != nil {
				return nil, err
			}
			defer release()
			return callMemoryAPITypedWithContextAndTTEnv(
				rctx,
				client.WithSingleTransportAttempt(callCtx),
				http.MethodPost,
				graphQueryPath,
				buildGraphQueryBody(querySpec, window),
				spec.MemoryTTEnv,
			)
		}, graphQueryRetryInterval)
	})
	if err := ctx.Err(); err != nil {
		return result, errs.NewNetworkError(errs.SubtypeNetworkTransport, "supplemental GraphQuery canceled").WithCause(err)
	}

	nodesByID := make(map[string]map[string]interface{})
	edgesByID := make(map[string]map[string]interface{})
	for _, execution := range executions {
		if execution.Err != nil {
			result.Complete = false
			result.FailedWindows = append(result.FailedWindows, newGraphSearchGraphQueryFailedWindow(execution, execution.Err))
			continue
		}
		data := unwrapMemoryData(execution.Data)
		nodes, nodesOK := data["nodes"].([]interface{})
		edges, edgesOK := data["edges"].([]interface{})
		if !nodesOK || !edgesOK {
			result.Complete = false
			responseErr := errs.NewInternalError(errs.SubtypeInvalidResponse, "supplemental GraphQuery response nodes and edges must be lists")
			result.FailedWindows = append(result.FailedWindows, newGraphSearchGraphQueryFailedWindow(execution, responseErr))
			continue
		}
		windowNodes := make(map[string]map[string]interface{})
		windowEdges := make(map[string]map[string]interface{})
		filteredNodes, filteredEdges, err := mergeGraphSearchGraphQueryWindow(windowNodes, windowEdges, execution.Window, nodes, edges)
		if err != nil {
			result.Complete = false
			result.FailedWindows = append(result.FailedWindows, newGraphSearchGraphQueryFailedWindow(execution, err))
			continue
		}
		for _, node := range windowNodes {
			mergeGraphSearchObject(nodesByID, node, "node_id")
		}
		for _, edge := range windowEdges {
			mergeGraphSearchObject(edgesByID, edge, "edge_id")
		}
		result.SuccessfulWindows++
		result.ReturnedNodeCount += len(nodes)
		result.ReturnedEdgeCount += len(edges)
		result.FilteredNodeCount += filteredNodes
		result.FilteredEdgeCount += filteredEdges
	}
	result.Nodes = sortedGraphSearchObjects(nodesByID, "node_id")
	result.Edges = sortedGraphSearchObjects(edgesByID, "edge_id")
	result.UniqueNodeCount = len(result.Nodes)
	result.UniqueEdgeCount = len(result.Edges)
	result.TookMS = time.Since(started).Milliseconds()
	return result, nil
}

func mergeGraphSearchGraphQueryWindow(nodesByID, edgesByID map[string]map[string]interface{}, window graphTimeWindow, nodes, edges []interface{}) (int, int, error) {
	source := graphSearchRetrievalSource{
		Kind:         graphSearchRetrievalGraphQuery,
		WindowIndex:  window.Index,
		StartTimeSec: window.StartTimeSec,
		EndTimeSec:   window.EndTimeSec,
	}
	filteredNodeIDs := make(map[string]struct{})
	filteredNodeCount := 0
	for _, rawNode := range nodes {
		node, ok := rawNode.(map[string]interface{})
		if !ok {
			return 0, 0, errs.NewInternalError(errs.SubtypeInvalidResponse, "supplemental GraphQuery node must be an object")
		}
		nodeID, ok := graphOneHopNonEmptyString(node["node_id"])
		if !ok {
			return 0, 0, errs.NewInternalError(errs.SubtypeInvalidResponse, "supplemental GraphQuery node has an invalid node_id")
		}
		nodeType, ok := graphOneHopInt64(node["node_type"])
		if !ok {
			return 0, 0, errs.NewInternalError(errs.SubtypeInvalidResponse, "supplemental GraphQuery node has an invalid node_type")
		}
		if nodeType < graphSearchIMDayNodeType || nodeType > 3 {
			filteredNodeIDs[nodeID] = struct{}{}
			filteredNodeCount++
			continue
		}
		if strings.TrimSpace(common.GetString(node, "graph_date")) == "" {
			eventTime, _ := graphOneHopInt64(node["event_time_sec"])
			graphDate, valid := graphSearchDate(eventTime)
			if !valid {
				graphDate, _ = graphSearchDate(window.StartTimeSec)
			}
			node["graph_date"] = graphDate
		}
		node["retrieved_via"] = []graphSearchRetrievalSource{source}
		mergeGraphSearchObject(nodesByID, node, "node_id")
	}
	filteredEdgeCount := 0
	for _, rawEdge := range edges {
		edge, ok := rawEdge.(map[string]interface{})
		if !ok {
			return 0, 0, errs.NewInternalError(errs.SubtypeInvalidResponse, "supplemental GraphQuery edge must be an object")
		}
		if _, err := graphOneHopRequiredString(edge, "edge_id", "supplemental GraphQuery edge"); err != nil {
			return 0, 0, err
		}
		fromNodeID, err := graphOneHopRequiredString(edge, "from_node_id", "supplemental GraphQuery edge")
		if err != nil {
			return 0, 0, err
		}
		toNodeID, err := graphOneHopRequiredString(edge, "to_node_id", "supplemental GraphQuery edge")
		if err != nil {
			return 0, 0, err
		}
		_, fromFiltered := filteredNodeIDs[fromNodeID]
		_, toFiltered := filteredNodeIDs[toNodeID]
		if fromFiltered || toFiltered || graphOneHopIsUserNodeID(fromNodeID, filteredNodeIDs) || graphOneHopIsUserNodeID(toNodeID, filteredNodeIDs) {
			filteredEdgeCount++
			continue
		}
		edge["retrieved_via"] = []graphSearchRetrievalSource{source}
		mergeGraphSearchObject(edgesByID, edge, "edge_id")
	}
	return filteredNodeCount, filteredEdgeCount, nil
}

func graphSearchGraphQueryRoots(nodes []map[string]interface{}) []graphSearchTraversalRoot {
	roots := make([]graphSearchTraversalRoot, 0)
	for _, node := range nodes {
		if expandable, present := node["expandable"].(bool); present && !expandable {
			continue
		}
		nodeType, ok := graphOneHopInt64(node["node_type"])
		if !ok || nodeType < graphSearchIMDayNodeType || nodeType > 3 {
			continue
		}
		rootID, ok := graphOneHopNonEmptyString(node["root_id"])
		if !ok || !graphSearchSafeRootID(rootID) {
			continue
		}
		graphDate := strings.TrimSpace(common.GetString(node, "graph_date"))
		eventTime, _ := graphOneHopInt64(node["event_time_sec"])
		anchorTimeSource := "graph_query.nodes.event_time_sec"
		if eventTime <= 0 {
			for _, source := range graphSearchRetrievalSources(node["retrieved_via"]) {
				if source.Kind == graphSearchRetrievalGraphQuery && source.StartTimeSec > 0 {
					eventTime = source.StartTimeSec
					anchorTimeSource = "graph_query.window.start_time_sec"
					break
				}
			}
		}
		if graphDate == "" {
			graphDate, ok = graphSearchDate(eventTime)
			if !ok {
				continue
			}
		}
		if _, _, ok := graphSearchDateWindow(graphDate); !ok {
			continue
		}
		roots = append(roots, graphSearchTraversalRoot{
			NodeType:  nodeType,
			RootID:    rootID,
			GraphDate: graphDate,
			SearchOrigins: []graphSearchOrigin{{
				CandidateSource:  "graph_query.nodes",
				Title:            strings.TrimSpace(common.GetString(node, "title")),
				AnchorTimeSec:    eventTime,
				GraphDate:        graphDate,
				NodeTypeSource:   "graph_query.nodes.node_type",
				RootIDSource:     "graph_query.nodes.root_id",
				AnchorTimeSource: anchorTimeSource,
			}},
		})
	}
	return filterGraphSearchCandidateRoots(roots, nil)
}

func newGraphSearchGraphQueryFailedWindow(execution graphWindowExecution, err error) graphSearchGraphQueryFailedWindow {
	failure := graphSearchGraphQueryFailedWindow{
		WindowIndex:  execution.Window.Index,
		StartTimeSec: execution.Window.StartTimeSec,
		EndTimeSec:   execution.Window.EndTimeSec,
		Attempts:     execution.Attempts,
		Message:      common.TruncateStr(err.Error(), 1000),
	}
	if problem, ok := errs.ProblemOf(err); ok {
		failure.ErrorType = string(problem.Category)
		failure.ErrorSubtype = string(problem.Subtype)
		failure.ErrorCode = problem.Code
		failure.LogID = problem.LogID
	}
	return failure
}

func graphSearchRetrievalSources(value interface{}) []graphSearchRetrievalSource {
	values, _ := value.([]graphSearchRetrievalSource)
	return values
}

func mergeGraphSearchRetrievalSources(values, candidates []graphSearchRetrievalSource) []graphSearchRetrievalSource {
	out := append([]graphSearchRetrievalSource(nil), values...)
	for _, candidate := range candidates {
		found := false
		for _, value := range out {
			if value == candidate {
				found = true
				break
			}
		}
		if !found {
			out = append(out, candidate)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Hop != out[j].Hop {
			return out[i].Hop < out[j].Hop
		}
		if out[i].WindowIndex != out[j].WindowIndex {
			return out[i].WindowIndex < out[j].WindowIndex
		}
		return out[i].GraphDate < out[j].GraphDate
	})
	return out
}

func graphSearchRetrievalClass(value interface{}) string {
	hasOneHop := false
	hasGraphQuery := false
	for _, source := range graphSearchRetrievalSources(value) {
		switch source.Kind {
		case graphSearchRetrievalOneHop:
			hasOneHop = true
		case graphSearchRetrievalGraphQuery:
			hasGraphQuery = true
		}
	}
	switch {
	case hasOneHop && hasGraphQuery:
		return "corroborated"
	case hasGraphQuery:
		return "graph_query_only"
	case hasOneHop:
		return "one_hop_only"
	default:
		return "unknown"
	}
}
