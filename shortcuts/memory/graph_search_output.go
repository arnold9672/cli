// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/output"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

type graphSearchOutput struct {
	Query             string                        `json:"query"`
	SearchCandidates  []graphSearchCandidate        `json:"search_candidates"`
	Roots             []graphSearchRoot             `json:"roots"`
	NextRoots         []graphSearchRoot             `json:"next_roots"`
	Continuation      graphSearchContinuation       `json:"continuation"`
	Nodes             []map[string]interface{}      `json:"nodes"`
	Edges             []map[string]interface{}      `json:"edges"`
	SkippedCandidates []graphSearchSkippedCandidate `json:"skipped_candidates"`
	FailedBatches     []graphSearchFailedBatch      `json:"failed_batches"`
	Hops              []graphSearchHopSummary       `json:"hops"`
	Evidence          graphSearchEvidenceIndex      `json:"evidence"`
	GraphQuery        graphSearchGraphQueryResult   `json:"graph_query"`
}

type graphSearchContinuation struct {
	Mode                  string `json:"mode"`
	RequiresModelDecision bool   `json:"requires_model_decision"`
	NextRootCount         int    `json:"next_root_count"`
	MaxTotalHops          int    `json:"max_total_hops"`
	DecisionBasis         string `json:"decision_basis"`
}

func executeGraphSearch(rctx *common.RuntimeContext, spec graphSearchSpec) error {
	started := time.Now()
	spec.RequestLimiter = make(chan struct{}, spec.Concurrency)
	spec.WikiResolver = newGraphSearchWikiResolver()
	graphQueryPlan := planGraphSearchGraphQuery(spec, started)
	type knowledgeResult struct {
		Candidates []graphSearchCandidate
		LogID      string
		Err        error
	}
	type graphQueryResult struct {
		Result graphSearchGraphQueryResult
		Err    error
	}
	workCtx, cancelWork := context.WithCancel(rctx.Ctx())
	defer cancelWork()
	knowledgeResults := make(chan knowledgeResult, 1)
	graphQueryResults := make(chan graphQueryResult, 1)
	go func() {
		candidates, logID, err := callGraphSearchKnowledge(workCtx, rctx, spec)
		knowledgeResults <- knowledgeResult{Candidates: candidates, LogID: logID, Err: err}
	}()
	go func() {
		result, err := executeGraphSearchGraphQuery(workCtx, rctx, spec, graphQueryPlan)
		graphQueryResults <- graphQueryResult{Result: result, Err: err}
	}()
	knowledge := <-knowledgeResults
	if knowledge.Err != nil {
		cancelWork()
		<-graphQueryResults
		return knowledge.Err
	}
	resolved, skipped, err := resolveGraphSearchCandidates(workCtx, rctx, spec, knowledge.Candidates)
	if err != nil {
		cancelWork()
		<-graphQueryResults
		return errs.NewNetworkError(errs.SubtypeNetworkTransport, "Graph Search candidate resolution canceled").WithCause(err)
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Rank < skipped[j].Rank })
	roots, tasks := mergeGraphSearchRoots(resolved)

	traversal := graphSearchTraversalResult{
		Nodes:         []map[string]interface{}{},
		Edges:         []map[string]interface{}{},
		FailedBatches: []graphSearchFailedBatch{},
		Hops:          []graphSearchHopSummary{},
		Complete:      true,
		StopReason:    "no_query_roots",
	}
	if len(tasks) > 0 {
		traversal, err = executeGraphSearchTraversal(rctx, spec, tasks)
		if err != nil {
			cancelWork()
			<-graphQueryResults
			return err
		}
	}
	graphQueryAsync := <-graphQueryResults
	if graphQueryAsync.Err != nil {
		return graphQueryAsync.Err
	}
	graphQuery := graphQueryAsync.Result
	nextTasks := append([]graphSearchTraversalRoot(nil), traversal.NextRoots...)
	nextTasks = append(nextTasks, graphSearchGraphQueryRoots(graphQuery.Nodes)...)
	nextTasks = filterGraphSearchCandidateRoots(nextTasks, traversal.ExpandedRoots)
	nextRoots, _ := mergeGraphSearchRoots(nextTasks)
	stopReason := traversal.StopReason
	if len(nextRoots) > 0 {
		stopReason = "agent_decision_required"
	}
	mergedNodes := mergeGraphSearchObjectSets(traversal.Nodes, graphQuery.Nodes, "node_id")
	mergedEdges := mergeGraphSearchObjectSets(traversal.Edges, graphQuery.Edges, "edge_id")

	out := graphSearchOutput{
		Query:            spec.Query,
		SearchCandidates: nonNilGraphSearchCandidates(knowledge.Candidates),
		Roots:            nonNilGraphSearchRoots(roots),
		NextRoots:        nonNilGraphSearchRoots(nextRoots),
		Continuation: graphSearchContinuation{
			Mode:                  "agent_controlled",
			RequiresModelDecision: len(nextRoots) > 0,
			NextRootCount:         len(nextRoots),
			MaxTotalHops:          graphOneHopMaxHop,
			DecisionBasis:         "judge Query relevance, new evidence, unresolved aspects, and expansion value from node and edge Detail before every additional hop",
		},
		Nodes:             nonNilGraphSearchObjects(mergedNodes),
		Edges:             nonNilGraphSearchObjects(mergedEdges),
		SkippedCandidates: nonNilGraphSearchSkipped(skipped),
		FailedBatches:     nonNilGraphSearchFailedBatches(traversal.FailedBatches),
		Hops:              nonNilGraphSearchHops(traversal.Hops),
		GraphQuery:        graphQuery,
	}
	out.Evidence = buildGraphSearchEvidenceIndex(out.SearchCandidates, out.Nodes, out.Edges)
	rootCount := len(out.Roots)
	nodeCount := len(out.Nodes)
	edgeCount := len(out.Edges)
	searchCandidateCount := len(out.SearchCandidates)
	skippedCandidateCount := len(out.SkippedCandidates)
	failedBatchCount := len(out.FailedBatches)
	hopsExecuted := traversal.HopsExecuted
	complete := traversal.Complete && graphQuery.Complete
	oneHopCalled := traversal.OneHopCalled
	graphQueryCalled := graphQuery.Called
	graphQueryWindowCount := graphQuery.WindowCount
	graphQueryFailedWindowCount := len(graphQuery.FailedWindows)
	tookMS := time.Since(started).Milliseconds()
	detailBytes := graphOneHopDetailBytes(out.Nodes, out.Edges)
	meta := &output.Meta{
		RootCount:                   &rootCount,
		NodeCount:                   &nodeCount,
		EdgeCount:                   &edgeCount,
		DetailBytes:                 &detailBytes,
		TookMS:                      &tookMS,
		LogID:                       knowledge.LogID,
		TraceID:                     spec.TraceID,
		Complete:                    &complete,
		OneHopCalled:                &oneHopCalled,
		HopsExecuted:                &hopsExecuted,
		SearchCandidateCount:        &searchCandidateCount,
		SkippedCandidateCount:       &skippedCandidateCount,
		FailedBatchCount:            &failedBatchCount,
		GraphQueryCalled:            &graphQueryCalled,
		GraphQueryWindowCount:       &graphQueryWindowCount,
		GraphQueryFailedWindowCount: &graphQueryFailedWindowCount,
		StopReason:                  stopReason,
	}

	var renderErr error
	rctx.OutFormat(out, meta, func(w io.Writer) {
		renderErr = renderGraphSearchPretty(w, out, meta)
	})
	return renderErr
}

func renderGraphSearchPretty(w io.Writer, out graphSearchOutput, meta *output.Meta) error {
	if _, err := fmt.Fprintf(w,
		"GraphSearch complete=%t hops=%d roots=%d next_roots=%d nodes=%d edges=%d failed_batches=%d trace_id=%s\n",
		graphSearchMetaBool(meta.Complete), graphSearchMetaInt(meta.HopsExecuted), graphSearchMetaInt(meta.RootCount),
		len(out.NextRoots), graphSearchMetaInt(meta.NodeCount), graphSearchMetaInt(meta.EdgeCount), graphSearchMetaInt(meta.FailedBatchCount), meta.TraceID,
	); err != nil {
		return err
	}
	if len(out.SkippedCandidates) > 0 {
		if _, err := fmt.Fprintf(w, "Skipped candidates: %d\n", len(out.SkippedCandidates)); err != nil {
			return err
		}
		for _, skipped := range out.SkippedCandidates {
			if _, err := fmt.Fprintf(w, "  rank=%d source_type=%d reason=%s title=%s\n",
				skipped.Rank, skipped.SourceType, skipped.Reason, skipped.Title); err != nil {
				return err
			}
		}
	}
	if len(out.FailedBatches) > 0 {
		if _, err := fmt.Fprintf(w, "Warning: %d OneHop batch(es) failed; returned graph is partial.\n", len(out.FailedBatches)); err != nil {
			return err
		}
		for _, failed := range out.FailedBatches {
			if _, err := fmt.Fprintf(w, "  hop=%d graph_date=%s attempts=%d code=%d log_id=%s message=%s\n",
				failed.Hop, failed.GraphDate, failed.Attempts, failed.ErrorCode, failed.LogID, failed.Message); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(w,
		"GraphQuery called=%t reason=%s windows=%d failed_windows=%d unique_nodes=%d unique_edges=%d\n",
		out.GraphQuery.Called, out.GraphQuery.TriggerReason, out.GraphQuery.WindowCount,
		len(out.GraphQuery.FailedWindows), out.GraphQuery.UniqueNodeCount, out.GraphQuery.UniqueEdgeCount,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w,
		"Evidence candidate_content=%d node_detail=%d edge_detail=%d self_loop_edges=%d edge_groups=%d\n",
		out.Evidence.CandidateContentCount, out.Evidence.NodeDetailCount, out.Evidence.EdgeDetailCount,
		out.Evidence.SelfLoopEdgeCount, len(out.Evidence.EdgeGroups),
	); err != nil {
		return err
	}
	return renderGraphDataPretty(w, map[string]interface{}{
		"nodes": graphSearchMapInterfaces(out.Nodes),
		"edges": graphSearchMapInterfaces(out.Edges),
	})
}

func mergeGraphSearchObjectSets(primary, supplemental []map[string]interface{}, idKey string) []map[string]interface{} {
	merged := make(map[string]map[string]interface{}, len(primary)+len(supplemental))
	for _, object := range primary {
		mergeGraphSearchObject(merged, object, idKey)
	}
	for _, object := range supplemental {
		mergeGraphSearchObject(merged, object, idKey)
	}
	return sortedGraphSearchObjects(merged, idKey)
}

func graphSearchMetaBool(value *bool) bool {
	return value != nil && *value
}

func graphSearchMetaInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func graphSearchMapInterfaces(values []map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func nonNilGraphSearchCandidates(values []graphSearchCandidate) []graphSearchCandidate {
	if values == nil {
		return []graphSearchCandidate{}
	}
	return values
}

func nonNilGraphSearchRoots(values []graphSearchRoot) []graphSearchRoot {
	if values == nil {
		return []graphSearchRoot{}
	}
	return values
}

func nonNilGraphSearchObjects(values []map[string]interface{}) []map[string]interface{} {
	if values == nil {
		return []map[string]interface{}{}
	}
	return values
}

func nonNilGraphSearchSkipped(values []graphSearchSkippedCandidate) []graphSearchSkippedCandidate {
	if values == nil {
		return []graphSearchSkippedCandidate{}
	}
	return values
}

func nonNilGraphSearchFailedBatches(values []graphSearchFailedBatch) []graphSearchFailedBatch {
	if values == nil {
		return []graphSearchFailedBatch{}
	}
	return values
}

func nonNilGraphSearchHops(values []graphSearchHopSummary) []graphSearchHopSummary {
	if values == nil {
		return []graphSearchHopSummary{}
	}
	return values
}
