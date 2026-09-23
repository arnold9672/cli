// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

type graphSearchExpansion struct {
	Hop            int                      `json:"hop"`
	Kind           string                   `json:"kind"`
	Root           graphSearchTraversalRoot `json:"root"`
	UpstreamNodeID string                   `json:"upstream_node_id"`
	EdgeID         string                   `json:"edge_id,omitempty"`
}

type graphSearchFailedBatch struct {
	Hop          int               `json:"hop"`
	GraphDate    string            `json:"graph_date"`
	Roots        []graphOneHopRoot `json:"roots"`
	Attempts     int               `json:"attempts"`
	ErrorType    string            `json:"error_type,omitempty"`
	ErrorSubtype string            `json:"error_subtype,omitempty"`
	ErrorCode    int               `json:"error_code,omitempty"`
	LogID        string            `json:"log_id,omitempty"`
	Message      string            `json:"message"`
}

type graphSearchHopSummary struct {
	Hop                int      `json:"hop"`
	TaskCount          int      `json:"task_count"`
	BatchCount         int      `json:"batch_count"`
	SuccessfulBatches  int      `json:"successful_batch_count"`
	FailedBatches      int      `json:"failed_batch_count"`
	ReturnedNodeCount  int      `json:"returned_node_count"`
	ReturnedEdgeCount  int      `json:"returned_edge_count"`
	NewNodeCount       int      `json:"new_node_count"`
	NewEdgeCount       int      `json:"new_edge_count"`
	NewNodeDetailCount int      `json:"new_node_detail_count"`
	NewEdgeDetailCount int      `json:"new_edge_detail_count"`
	TookMS             int64    `json:"took_ms"`
	LogIDs             []string `json:"log_ids,omitempty"`
}

type graphSearchTraversalResult struct {
	Nodes             []map[string]interface{}
	Edges             []map[string]interface{}
	NextRoots         []graphSearchTraversalRoot
	ExpandedRoots     []graphSearchTraversalRoot
	FailedBatches     []graphSearchFailedBatch
	Hops              []graphSearchHopSummary
	Complete          bool
	OneHopCalled      bool
	HopsExecuted      int
	SuccessfulBatches int
	StopReason        string
}

type graphSearchBatch struct {
	Hop          int
	GraphDate    string
	StartTimeSec int64
	EndTimeSec   int64
	Tasks        []graphSearchTraversalRoot
}

type graphSearchBatchExecution struct {
	Batch    graphSearchBatch
	Result   *graphOneHopCallResult
	Err      error
	Attempts int
}

func executeGraphSearchTraversal(rctx *common.RuntimeContext, spec graphSearchSpec, initial []graphSearchTraversalRoot) (graphSearchTraversalResult, error) {
	result := graphSearchTraversalResult{Complete: true}
	frontier := append([]graphSearchTraversalRoot(nil), initial...)
	visited := make(map[string]struct{})
	nodesByID := make(map[string]map[string]interface{})
	edgesByID := make(map[string]map[string]interface{})

	frontier = filterGraphSearchFrontier(frontier, visited)
	if len(frontier) == 0 {
		result.StopReason = "no_query_roots"
		return result, nil
	}

	const hop = 1
	result.ExpandedRoots = append(result.ExpandedRoots, frontier...)
	batches, err := buildGraphSearchBatches(frontier, hop)
	if err != nil {
		return result, err
	}
	result.OneHopCalled = true
	result.HopsExecuted = hop
	hopStarted := time.Now()
	executions := runGraphSearchBatches(rctx, spec, batches)
	if err := rctx.Ctx().Err(); err != nil {
		return result, errs.NewNetworkError(errs.SubtypeNetworkTransport, "Graph Search traversal canceled").WithCause(err)
	}

	summary := graphSearchHopSummary{Hop: hop, TaskCount: len(frontier), BatchCount: len(batches)}
	next := make([]graphSearchTraversalRoot, 0)
	for _, execution := range executions {
		if execution.Err != nil {
			result.Complete = false
			summary.FailedBatches++
			result.FailedBatches = append(result.FailedBatches, newGraphSearchFailedBatch(execution))
			continue
		}
		summary.SuccessfulBatches++
		result.SuccessfulBatches++
		if execution.Result.Meta != nil && execution.Result.Meta.LogID != "" {
			summary.LogIDs = appendUniqueGraphSearchString(summary.LogIDs, execution.Result.Meta.LogID)
		}
		nodes := graphItems(execution.Result.Data, "nodes")
		edges := graphItems(execution.Result.Data, "edges")
		summary.ReturnedNodeCount += len(nodes)
		summary.ReturnedEdgeCount += len(edges)
		taskOrigins := graphSearchBatchOrigins(execution.Batch.Tasks)
		for _, rawNode := range nodes {
			node, ok := rawNode.(map[string]interface{})
			if !ok {
				return result, graphOneHopInvalidResponse("Graph Search OneHop node must be an object")
			}
			enrichGraphSearchObject(node, hop, execution.Batch.GraphDate, taskOrigins)
			newNode, newNodeDetail := mergeGraphSearchObject(nodesByID, node, "node_id")
			if newNode {
				summary.NewNodeCount++
			}
			if newNodeDetail {
				summary.NewNodeDetailCount++
			}
			if task, ok := nextGraphSearchTask(node); ok {
				next = append(next, task)
			}
		}
		for _, rawEdge := range edges {
			edge, ok := rawEdge.(map[string]interface{})
			if !ok {
				return result, graphOneHopInvalidResponse("Graph Search OneHop edge must be an object")
			}
			enrichGraphSearchObject(edge, hop, execution.Batch.GraphDate, taskOrigins)
			newEdge, newEdgeDetail := mergeGraphSearchObject(edgesByID, edge, "edge_id")
			if newEdge {
				summary.NewEdgeCount++
			}
			if newEdgeDetail {
				summary.NewEdgeDetailCount++
			}
		}
	}
	sort.Strings(summary.LogIDs)
	summary.TookMS = time.Since(hopStarted).Milliseconds()
	result.Hops = append(result.Hops, summary)

	switch {
	case summary.NewNodeCount == 0 && summary.NewEdgeCount == 0 &&
		summary.NewNodeDetailCount == 0 && summary.NewEdgeDetailCount == 0:
		result.StopReason = "no_new_graph_evidence"
	case len(next) == 0:
		result.StopReason = "frontier_exhausted"
	default:
		_, frontier = mergeGraphSearchRoots(next)
		result.NextRoots = filterGraphSearchCandidateRoots(frontier, result.ExpandedRoots)
		if len(result.NextRoots) == 0 {
			result.StopReason = "frontier_exhausted"
		} else {
			result.StopReason = "agent_decision_required"
		}
	}

	if result.OneHopCalled && result.SuccessfulBatches == 0 {
		return result, graphSearchAllBatchesFailedError(result.FailedBatches)
	}
	result.Nodes = sortedGraphSearchObjects(nodesByID, "node_id")
	result.Edges = sortedGraphSearchObjects(edgesByID, "edge_id")
	sort.Slice(result.FailedBatches, func(i, j int) bool {
		if result.FailedBatches[i].Hop != result.FailedBatches[j].Hop {
			return result.FailedBatches[i].Hop < result.FailedBatches[j].Hop
		}
		return result.FailedBatches[i].GraphDate < result.FailedBatches[j].GraphDate
	})
	return result, nil
}

func filterGraphSearchCandidateRoots(candidates, expanded []graphSearchTraversalRoot) []graphSearchTraversalRoot {
	_, mergedCandidates := mergeGraphSearchRoots(candidates)
	expandedKeys := make(map[string]struct{}, len(expanded))
	for _, root := range expanded {
		expandedKeys[graphSearchTaskKey(root.NodeType, root.RootID, root.GraphDate)] = struct{}{}
	}
	out := make([]graphSearchTraversalRoot, 0, len(mergedCandidates))
	for _, root := range mergedCandidates {
		if _, ok := expandedKeys[graphSearchTaskKey(root.NodeType, root.RootID, root.GraphDate)]; ok {
			continue
		}
		out = append(out, root)
	}
	return out
}

func filterGraphSearchFrontier(frontier []graphSearchTraversalRoot, visited map[string]struct{}) []graphSearchTraversalRoot {
	_, merged := mergeGraphSearchRoots(frontier)
	out := make([]graphSearchTraversalRoot, 0, len(merged))
	for _, task := range merged {
		key := graphSearchTaskKey(task.NodeType, task.RootID, task.GraphDate)
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}
		out = append(out, task)
	}
	return out
}

func buildGraphSearchBatches(tasks []graphSearchTraversalRoot, hop int) ([]graphSearchBatch, error) {
	byDate := make(map[string][]graphSearchTraversalRoot)
	for _, task := range tasks {
		byDate[task.GraphDate] = append(byDate[task.GraphDate], task)
	}
	dates := make([]string, 0, len(byDate))
	for graphDate := range byDate {
		dates = append(dates, graphDate)
	}
	sort.Strings(dates)
	batches := make([]graphSearchBatch, 0, len(dates))
	for _, graphDate := range dates {
		start, end, ok := graphSearchDateWindow(graphDate)
		if !ok {
			return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
				"Graph Search generated invalid graph_date %q", graphDate)
		}
		dateTasks := byDate[graphDate]
		sortGraphSearchTasks(dateTasks)
		batches = append(batches, graphSearchBatch{
			Hop:          hop,
			GraphDate:    graphDate,
			StartTimeSec: start,
			EndTimeSec:   end,
			Tasks:        dateTasks,
		})
	}
	return batches, nil
}

func runGraphSearchBatches(rctx *common.RuntimeContext, spec graphSearchSpec, batches []graphSearchBatch) []graphSearchBatchExecution {
	results := make([]graphSearchBatchExecution, len(batches))
	runGraphSearchIndexed(rctx.Ctx(), spec.Concurrency, len(batches), func(index int) {
		results[index] = executeGraphSearchBatch(rctx, spec, batches[index])
	})
	return results
}

func executeGraphSearchBatch(rctx *common.RuntimeContext, spec graphSearchSpec, batch graphSearchBatch) graphSearchBatchExecution {
	execution := graphSearchBatchExecution{Batch: batch}
	roots := make([]graphOneHopRoot, 0, len(batch.Tasks))
	for _, task := range batch.Tasks {
		roots = append(roots, graphOneHopRoot{NodeType: task.NodeType, RootID: task.RootID})
	}
	oneHopSpec := graphOneHopSpec{
		UserID:       spec.GraphUserID,
		Roots:        roots,
		StartTimeSec: batch.StartTimeSec,
		EndTimeSec:   batch.EndTimeSec,
		NodeTypes:    []int64{graphSearchIMDayNodeType, graphSearchDocDayNodeType, 3},
		DetailFormat: spec.DetailFormat,
		Hop:          batch.Hop,
		TraceID:      spec.TraceID,
		Scene:        graphOneHopDefaultScene,
	}
	for attempt := 1; attempt <= graphQueryMaxAttempts; attempt++ {
		execution.Attempts = attempt
		if err := rctx.Ctx().Err(); err != nil {
			execution.Err = err
			return execution
		}
		release, permitErr := acquireGraphSearchRequestPermit(rctx.Ctx(), spec)
		if permitErr != nil {
			execution.Err = permitErr
			return execution
		}
		result, err := callGraphOneHopWithTTEnv(rctx, oneHopSpec, spec.MemoryTTEnv)
		release()
		if err == nil {
			execution.Result = result
			return execution
		}
		if attempt == graphQueryMaxAttempts || !isGraphQueryRetryable(err) {
			execution.Err = err
			return execution
		}
		if waitErr := waitGraphQueryRetry(rctx.Ctx(), graphQueryRetryInterval); waitErr != nil {
			execution.Err = waitErr
			return execution
		}
	}
	return execution
}

func graphSearchBatchOrigins(tasks []graphSearchTraversalRoot) map[string][]graphSearchOrigin {
	origins := make(map[string][]graphSearchOrigin, len(tasks))
	for _, task := range tasks {
		key := graphSearchRootKey(task.NodeType, task.RootID)
		origins[key] = mergeGraphSearchOrigins(origins[key], task.SearchOrigins)
	}
	return origins
}

func enrichGraphSearchObject(object map[string]interface{}, hop int, graphDate string, taskOrigins map[string][]graphSearchOrigin) {
	oneHopExpansions := graphOneHopExpansions(object["expanded_from"])
	expansions := make([]graphSearchExpansion, 0, len(oneHopExpansions))
	origins := make([]graphSearchOrigin, 0)
	for _, expansion := range oneHopExpansions {
		rootOrigins := taskOrigins[graphSearchRootKey(expansion.Root.NodeType, expansion.Root.RootID)]
		expansions = appendGraphSearchExpansion(expansions, graphSearchExpansion{
			Hop:  hop,
			Kind: expansion.Kind,
			Root: graphSearchTraversalRoot{
				NodeType:  expansion.Root.NodeType,
				RootID:    expansion.Root.RootID,
				GraphDate: graphDate,
			},
			UpstreamNodeID: expansion.UpstreamNodeID,
			EdgeID:         expansion.EdgeID,
		})
		origins = mergeGraphSearchOrigins(origins, rootOrigins)
	}
	object["expanded_from"] = expansions
	object["search_origins"] = origins
	object["first_seen_hop"] = hop
	object["retrieved_via"] = []graphSearchRetrievalSource{{
		Kind:      graphSearchRetrievalOneHop,
		Hop:       hop,
		GraphDate: graphDate,
	}}
}

func appendGraphSearchExpansion(values []graphSearchExpansion, candidate graphSearchExpansion) []graphSearchExpansion {
	for _, value := range values {
		if value.Hop == candidate.Hop && value.Kind == candidate.Kind &&
			value.Root.NodeType == candidate.Root.NodeType && value.Root.RootID == candidate.Root.RootID &&
			value.Root.GraphDate == candidate.Root.GraphDate && value.UpstreamNodeID == candidate.UpstreamNodeID &&
			value.EdgeID == candidate.EdgeID {
			return values
		}
	}
	return append(values, candidate)
}

func mergeGraphSearchObject(objects map[string]map[string]interface{}, candidate map[string]interface{}, idKey string) (bool, bool) {
	id, _ := graphOneHopNonEmptyString(candidate[idKey])
	if id == "" {
		return false, false
	}
	existing := objects[id]
	if existing == nil {
		objects[id] = candidate
		return true, graphSearchDetailPresent(candidate["detail"])
	}
	newDetail := false
	if !graphSearchDetailPresent(existing["detail"]) && graphSearchDetailPresent(candidate["detail"]) {
		existing["detail"] = candidate["detail"]
		newDetail = true
	}
	existing["expanded_from"] = mergeGraphSearchExpansions(
		graphSearchExpansions(existing["expanded_from"]),
		graphSearchExpansions(candidate["expanded_from"]),
	)
	existing["search_origins"] = mergeGraphSearchOrigins(
		graphSearchOrigins(existing["search_origins"]),
		graphSearchOrigins(candidate["search_origins"]),
	)
	existing["retrieved_via"] = mergeGraphSearchRetrievalSources(
		graphSearchRetrievalSources(existing["retrieved_via"]),
		graphSearchRetrievalSources(candidate["retrieved_via"]),
	)
	existingHop, _ := graphOneHopInt64(existing["first_seen_hop"])
	candidateHop, _ := graphOneHopInt64(candidate["first_seen_hop"])
	if existingHop == 0 || candidateHop < existingHop {
		existing["first_seen_hop"] = candidateHop
	}
	return false, newDetail
}

func graphSearchExpansions(value interface{}) []graphSearchExpansion {
	values, _ := value.([]graphSearchExpansion)
	return values
}

func graphSearchOrigins(value interface{}) []graphSearchOrigin {
	values, _ := value.([]graphSearchOrigin)
	return values
}

func mergeGraphSearchExpansions(values, candidates []graphSearchExpansion) []graphSearchExpansion {
	out := append([]graphSearchExpansion(nil), values...)
	for _, candidate := range candidates {
		out = appendGraphSearchExpansion(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		left := strconv.Itoa(out[i].Hop) + "\x00" + graphSearchTaskKey(out[i].Root.NodeType, out[i].Root.RootID, out[i].Root.GraphDate) + "\x00" + out[i].EdgeID
		right := strconv.Itoa(out[j].Hop) + "\x00" + graphSearchTaskKey(out[j].Root.NodeType, out[j].Root.RootID, out[j].Root.GraphDate) + "\x00" + out[j].EdgeID
		return left < right
	})
	return out
}

func nextGraphSearchTask(node map[string]interface{}) (graphSearchTraversalRoot, bool) {
	expandable, _ := node["expandable"].(bool)
	if !expandable {
		return graphSearchTraversalRoot{}, false
	}
	nodeType, ok := graphOneHopInt64(node["node_type"])
	if !ok || nodeType < graphSearchIMDayNodeType || nodeType > 3 {
		return graphSearchTraversalRoot{}, false
	}
	rootID, ok := graphOneHopNonEmptyString(node["root_id"])
	if !ok || len(rootID) > 1024 {
		return graphSearchTraversalRoot{}, false
	}
	graphDate := strings.TrimSpace(common.GetString(node, "graph_date"))
	if graphDate == "" {
		eventTime, _ := graphOneHopInt64(node["event_time_sec"])
		graphDate, ok = graphSearchDate(eventTime)
		if !ok {
			return graphSearchTraversalRoot{}, false
		}
	}
	if _, _, ok := graphSearchDateWindow(graphDate); !ok {
		return graphSearchTraversalRoot{}, false
	}
	origins := graphSearchOrigins(node["search_origins"])
	if len(origins) == 0 {
		return graphSearchTraversalRoot{}, false
	}
	return graphSearchTraversalRoot{NodeType: nodeType, RootID: rootID, GraphDate: graphDate, SearchOrigins: origins}, true
}

func newGraphSearchFailedBatch(execution graphSearchBatchExecution) graphSearchFailedBatch {
	failure := graphSearchFailedBatch{
		Hop:       execution.Batch.Hop,
		GraphDate: execution.Batch.GraphDate,
		Attempts:  execution.Attempts,
		Message:   common.TruncateStr(execution.Err.Error(), 1000),
	}
	for _, task := range execution.Batch.Tasks {
		failure.Roots = append(failure.Roots, graphOneHopRoot{NodeType: task.NodeType, RootID: task.RootID})
	}
	if problem, ok := errs.ProblemOf(execution.Err); ok {
		failure.ErrorType = string(problem.Category)
		failure.ErrorSubtype = string(problem.Subtype)
		failure.ErrorCode = problem.Code
		failure.LogID = problem.LogID
	}
	return failure
}

func graphSearchAllBatchesFailedError(failures []graphSearchFailedBatch) error {
	err := errs.NewAPIError(errs.SubtypeUnknown, "all OneHop batches failed").
		WithHint("inspect the returned log_id, or retry after the OneHop deployment/ACL issue is resolved")
	if len(failures) > 0 {
		if failures[0].ErrorCode != 0 {
			err.WithCode(failures[0].ErrorCode)
		}
		if failures[0].LogID != "" {
			err.WithLogID(failures[0].LogID)
		}
	}
	return err
}

func sortedGraphSearchObjects(objects map[string]map[string]interface{}, idKey string) []map[string]interface{} {
	keys := make([]string, 0, len(objects))
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		out = append(out, objects[key])
	}
	return out
}
