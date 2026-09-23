// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const (
	graphSearchSourceWiki    int64 = 2
	graphSearchSourceDoc     int64 = 3
	graphSearchSourceMessage int64 = 6
	graphSearchSourceMinutes int64 = 8

	graphSearchIMDayNodeType  int64 = 1
	graphSearchDocDayNodeType int64 = 2
)

var graphSearchLocation = time.FixedZone("Asia/Shanghai", graphSearchTimezoneOffsetSec)

type graphSearchOrigin struct {
	CandidateSource  string  `json:"candidate_source"`
	Rank             int     `json:"rank"`
	PassageID        string  `json:"passage_id,omitempty"`
	SourceType       int64   `json:"source_type,omitempty"`
	Title            string  `json:"title,omitempty"`
	URL              string  `json:"url,omitempty"`
	Score            float64 `json:"score,omitempty"`
	AnchorTimeSec    int64   `json:"anchor_time_sec"`
	GraphDate        string  `json:"graph_date"`
	NodeTypeSource   string  `json:"node_type_source"`
	RootIDSource     string  `json:"root_id_source"`
	AnchorTimeSource string  `json:"anchor_time_source"`
}

type graphSearchTraversalRoot struct {
	NodeType      int64               `json:"node_type"`
	RootID        string              `json:"root_id"`
	GraphDate     string              `json:"graph_date"`
	SearchOrigins []graphSearchOrigin `json:"search_origins,omitempty"`
}

type graphSearchRoot struct {
	NodeType      int64               `json:"node_type"`
	RootID        string              `json:"root_id"`
	AnchorDates   []string            `json:"anchor_dates"`
	SearchOrigins []graphSearchOrigin `json:"search_origins"`
}

type graphSearchSkippedCandidate struct {
	Rank       int    `json:"rank"`
	PassageID  string `json:"passage_id,omitempty"`
	SourceType int64  `json:"source_type"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty"`
	Reason     string `json:"reason"`
	Message    string `json:"message,omitempty"`
}

type graphSearchCandidateResolution struct {
	Root    *graphSearchTraversalRoot
	Skipped *graphSearchSkippedCandidate
}

type graphSearchWikiResolution struct {
	Node map[string]interface{}
	Err  error
	Done chan struct{}
}

type graphSearchWikiResolver struct {
	mu      sync.Mutex
	entries map[string]*graphSearchWikiResolution
}

func newGraphSearchWikiResolver() *graphSearchWikiResolver {
	return &graphSearchWikiResolver{entries: make(map[string]*graphSearchWikiResolution)}
}

func (r *graphSearchWikiResolver) resolve(ctx context.Context, nodeToken string, call func() (map[string]interface{}, error)) (map[string]interface{}, error) {
	if r == nil {
		return call()
	}
	r.mu.Lock()
	if existing := r.entries[nodeToken]; existing != nil {
		r.mu.Unlock()
		select {
		case <-existing.Done:
			return existing.Node, existing.Err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	entry := &graphSearchWikiResolution{Done: make(chan struct{})}
	r.entries[nodeToken] = entry
	r.mu.Unlock()

	entry.Node, entry.Err = call()
	close(entry.Done)
	return entry.Node, entry.Err
}

func resolveGraphSearchCandidates(ctx context.Context, rctx *common.RuntimeContext, spec graphSearchSpec, candidates []graphSearchCandidate) ([]graphSearchTraversalRoot, []graphSearchSkippedCandidate, error) {
	results := make([]graphSearchCandidateResolution, len(candidates))
	runGraphSearchIndexed(ctx, spec.Concurrency, len(candidates), func(index int) {
		results[index] = resolveGraphSearchCandidate(ctx, rctx, spec, candidates[index])
	})
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	roots := make([]graphSearchTraversalRoot, 0, len(results))
	skipped := make([]graphSearchSkippedCandidate, 0)
	for _, result := range results {
		if result.Root != nil {
			roots = append(roots, *result.Root)
		}
		if result.Skipped != nil {
			skipped = append(skipped, *result.Skipped)
		}
	}
	return roots, skipped, nil
}

func resolveGraphSearchCandidate(ctx context.Context, rctx *common.RuntimeContext, spec graphSearchSpec, candidate graphSearchCandidate) graphSearchCandidateResolution {
	skip := func(reason, message string) graphSearchCandidateResolution {
		return graphSearchCandidateResolution{Skipped: &graphSearchSkippedCandidate{
			Rank:       candidate.Rank,
			PassageID:  candidate.PassageID,
			SourceType: candidate.SourceType,
			Title:      candidate.Title,
			URL:        candidate.URL,
			Reason:     reason,
			Message:    message,
		}}
	}

	var nodeType int64
	var rootID string
	var anchorTimeSec int64
	var rootIDSource string
	var anchorTimeSource string
	switch candidate.SourceType {
	case graphSearchSourceDoc:
		var ok bool
		rootID, ok = graphSearchURLPathToken(candidate.URL,
			"/docx/", "/doc/", "/sheets/", "/base/", "/mindnote/", "/slides/", "/file/")
		if !ok {
			return skip("doc_root_unavailable", "document URL does not contain a supported object token")
		}
		nodeType = graphSearchDocDayNodeType
		rootIDSource = "knowledge_qa.passages.url.object_token"
		if candidate.UpdateTimeSec > 0 {
			anchorTimeSec = candidate.UpdateTimeSec
			anchorTimeSource = "knowledge_qa.passages.extra.update_time"
		} else {
			anchorTimeSec = candidate.CreateTimeSec
			anchorTimeSource = "knowledge_qa.passages.extra.create_time"
		}
	case graphSearchSourceWiki:
		nodeToken, ok := graphSearchURLPathToken(candidate.URL, "/wiki/")
		if !ok {
			return skip("wiki_node_token_unavailable", "Wiki URL does not contain a node_token")
		}
		node, err := spec.WikiResolver.resolve(ctx, nodeToken, func() (map[string]interface{}, error) {
			release, err := acquireGraphSearchRequestPermit(ctx, spec)
			if err != nil {
				return nil, err
			}
			defer release()
			data, err := rctx.CallAPITyped("GET", "/open-apis/wiki/v2/spaces/get_node", map[string]interface{}{"token": nodeToken}, nil)
			if err != nil {
				return nil, err
			}
			return common.GetMap(data, "node"), nil
		})
		if err != nil {
			return skip("wiki_resolution_failed", common.TruncateStr(err.Error(), 500))
		}
		rootID = strings.TrimSpace(common.GetString(node, "obj_token"))
		if !graphSearchSafeRootID(rootID) {
			return skip("wiki_obj_token_unavailable", "Wiki get_node did not return a valid obj_token")
		}
		nodeType = graphSearchDocDayNodeType
		rootIDSource = "wiki.get_node.obj_token"
		if objectEditTime := parseGraphSearchTimestamp(node["obj_edit_time"]); objectEditTime > 0 {
			anchorTimeSec = objectEditTime
			anchorTimeSource = "wiki.get_node.obj_edit_time"
		} else if candidate.UpdateTimeSec > 0 {
			anchorTimeSec = candidate.UpdateTimeSec
			anchorTimeSource = "knowledge_qa.passages.extra.update_time"
		} else {
			anchorTimeSec = candidate.CreateTimeSec
			anchorTimeSource = "knowledge_qa.passages.extra.create_time"
		}
	case graphSearchSourceMessage:
		var ok bool
		rootID, ok = graphSearchMessageChatID(candidate.URL)
		if !ok {
			return skip("message_chat_id_unavailable", "message URL does not contain a positive internal chatId")
		}
		nodeType = graphSearchIMDayNodeType
		rootIDSource = "knowledge_qa.passages.url.chatId"
		if candidate.CreateTimeSec > 0 {
			anchorTimeSec = candidate.CreateTimeSec
			anchorTimeSource = "knowledge_qa.passages.extra.create_time"
		} else {
			anchorTimeSec = candidate.UpdateTimeSec
			anchorTimeSource = "knowledge_qa.passages.extra.update_time"
		}
	case graphSearchSourceMinutes:
		return skip("minutes_root_unsupported", "Minutes are retained as candidates but do not enter OneHop")
	default:
		return skip("unsupported_source_type", "source type is not supported as a Graph Search root")
	}

	graphDate, ok := graphSearchDate(anchorTimeSec)
	if !ok {
		return skip("anchor_time_unavailable", "candidate does not contain a valid graph date anchor")
	}
	origin := graphSearchOrigin{
		CandidateSource:  "knowledge_qa.passages",
		Rank:             candidate.Rank,
		PassageID:        candidate.PassageID,
		SourceType:       candidate.SourceType,
		Title:            candidate.Title,
		URL:              candidate.URL,
		Score:            candidate.Score,
		AnchorTimeSec:    anchorTimeSec,
		GraphDate:        graphDate,
		NodeTypeSource:   "knowledge_qa.passages.source_type",
		RootIDSource:     rootIDSource,
		AnchorTimeSource: anchorTimeSource,
	}
	return graphSearchCandidateResolution{Root: &graphSearchTraversalRoot{
		NodeType:      nodeType,
		RootID:        rootID,
		GraphDate:     graphDate,
		SearchOrigins: []graphSearchOrigin{origin},
	}}
}

func graphSearchDate(timestampSec int64) (string, bool) {
	if timestampSec <= 0 {
		return "", false
	}
	return time.Unix(timestampSec, 0).In(graphSearchLocation).Format("2006-01-02"), true
}

func graphSearchDateWindow(graphDate string) (int64, int64, bool) {
	start, err := time.ParseInLocation("2006-01-02", graphDate, graphSearchLocation)
	if err != nil {
		return 0, 0, false
	}
	return start.Unix(), start.Add(24 * time.Hour).Unix(), true
}

func runGraphSearchIndexed(ctx context.Context, concurrency, count int, fn func(index int)) {
	if count == 0 {
		return
	}
	workers := concurrency
	if workers > count {
		workers = count
	}
	jobs := make(chan int, count)
	for index := 0; index < count; index++ {
		jobs <- index
	}
	close(jobs)

	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				fn(index)
			}
		}()
	}
	waitGroup.Wait()
}

func mergeGraphSearchRoots(tasks []graphSearchTraversalRoot) ([]graphSearchRoot, []graphSearchTraversalRoot) {
	byRoot := make(map[string]*graphSearchRoot)
	byTask := make(map[string]*graphSearchTraversalRoot)
	for _, task := range tasks {
		rootKey := graphSearchRootKey(task.NodeType, task.RootID)
		root := byRoot[rootKey]
		if root == nil {
			root = &graphSearchRoot{NodeType: task.NodeType, RootID: task.RootID}
			byRoot[rootKey] = root
		}
		root.AnchorDates = appendUniqueGraphSearchString(root.AnchorDates, task.GraphDate)
		root.SearchOrigins = mergeGraphSearchOrigins(root.SearchOrigins, task.SearchOrigins)

		taskKey := graphSearchTaskKey(task.NodeType, task.RootID, task.GraphDate)
		mergedTask := byTask[taskKey]
		if mergedTask == nil {
			copy := task
			copy.SearchOrigins = mergeGraphSearchOrigins(nil, task.SearchOrigins)
			byTask[taskKey] = &copy
		} else {
			mergedTask.SearchOrigins = mergeGraphSearchOrigins(mergedTask.SearchOrigins, task.SearchOrigins)
		}
	}

	roots := make([]graphSearchRoot, 0, len(byRoot))
	for _, root := range byRoot {
		sort.Strings(root.AnchorDates)
		root.SearchOrigins = mergeGraphSearchOrigins(nil, root.SearchOrigins)
		roots = append(roots, *root)
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].NodeType != roots[j].NodeType {
			return roots[i].NodeType < roots[j].NodeType
		}
		return roots[i].RootID < roots[j].RootID
	})

	mergedTasks := make([]graphSearchTraversalRoot, 0, len(byTask))
	for _, task := range byTask {
		mergedTasks = append(mergedTasks, *task)
	}
	sortGraphSearchTasks(mergedTasks)
	return roots, mergedTasks
}

func graphSearchRootKey(nodeType int64, rootID string) string {
	return strconv.FormatInt(nodeType, 10) + "\x00" + rootID
}

func graphSearchTaskKey(nodeType int64, rootID, graphDate string) string {
	return graphSearchRootKey(nodeType, rootID) + "\x00" + graphDate
}

func sortGraphSearchTasks(tasks []graphSearchTraversalRoot) {
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].GraphDate != tasks[j].GraphDate {
			return tasks[i].GraphDate < tasks[j].GraphDate
		}
		if tasks[i].NodeType != tasks[j].NodeType {
			return tasks[i].NodeType < tasks[j].NodeType
		}
		return tasks[i].RootID < tasks[j].RootID
	})
}

func appendUniqueGraphSearchString(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func mergeGraphSearchOrigins(values, candidates []graphSearchOrigin) []graphSearchOrigin {
	seen := make(map[string]struct{}, len(values)+len(candidates))
	out := make([]graphSearchOrigin, 0, len(values)+len(candidates))
	for _, origin := range append(append([]graphSearchOrigin(nil), values...), candidates...) {
		key := strings.Join([]string{
			origin.CandidateSource,
			origin.PassageID,
			strconv.FormatInt(origin.SourceType, 10),
			origin.URL,
			origin.GraphDate,
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, origin)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		if out[i].PassageID != out[j].PassageID {
			return out[i].PassageID < out[j].PassageID
		}
		return out[i].GraphDate < out[j].GraphDate
	})
	return out
}
