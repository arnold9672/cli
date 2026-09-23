// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// graphSearchEvidenceIndex is a compact, non-duplicating index over the raw
// candidate, node, and edge payloads. It makes both node and edge Detail
// discoverable to agents without copying large Detail bodies a second time.
type graphSearchEvidenceIndex struct {
	CandidateSource         string                     `json:"candidate_source"`
	PermissionFiltered      bool                       `json:"permission_filtered"`
	CandidateCount          int                        `json:"candidate_count"`
	CandidateContentCount   int                        `json:"candidate_content_count"`
	NodeDetailCount         int                        `json:"node_detail_count"`
	MissingNodeDetailCount  int                        `json:"missing_node_detail_count"`
	EdgeDetailCount         int                        `json:"edge_detail_count"`
	MissingEdgeDetailCount  int                        `json:"missing_edge_detail_count"`
	SelfLoopEdgeCount       int                        `json:"self_loop_edge_count"`
	OneHopOnlyNodeCount     int                        `json:"one_hop_only_node_count"`
	GraphQueryOnlyNodeCount int                        `json:"graph_query_only_node_count"`
	CorroboratedNodeCount   int                        `json:"corroborated_node_count"`
	OneHopOnlyEdgeCount     int                        `json:"one_hop_only_edge_count"`
	GraphQueryOnlyEdgeCount int                        `json:"graph_query_only_edge_count"`
	CorroboratedEdgeCount   int                        `json:"corroborated_edge_count"`
	NodeDetails             []graphSearchNodeDetailRef `json:"node_details"`
	EdgeDetails             []graphSearchEdgeDetailRef `json:"edge_details"`
	EdgeGroups              []graphSearchEdgeGroup     `json:"edge_groups"`
}

type graphSearchNodeDetailRef struct {
	NodeID            string `json:"node_id"`
	NodeType          int64  `json:"node_type,omitempty"`
	FirstSeenHop      int    `json:"first_seen_hop,omitempty"`
	DetailPath        string `json:"detail_path"`
	DetailPresent     bool   `json:"detail_present"`
	DetailFormat      string `json:"detail_format,omitempty"`
	DetailBytes       int    `json:"detail_bytes"`
	SearchOriginCount int    `json:"search_origin_count"`
	RetrievalClass    string `json:"retrieval_class"`
}

type graphSearchEdgeDetailRef struct {
	EdgeID            string `json:"edge_id"`
	RelationType      string `json:"relation_type,omitempty"`
	FromNodeID        string `json:"from_node_id,omitempty"`
	ToNodeID          string `json:"to_node_id,omitempty"`
	FirstSeenHop      int    `json:"first_seen_hop,omitempty"`
	DetailPath        string `json:"detail_path"`
	DetailPresent     bool   `json:"detail_present"`
	DetailFormat      string `json:"detail_format,omitempty"`
	DetailBytes       int    `json:"detail_bytes"`
	SelfLoop          bool   `json:"self_loop"`
	FromNodePresent   bool   `json:"from_node_present"`
	ToNodePresent     bool   `json:"to_node_present"`
	SearchOriginCount int    `json:"search_origin_count"`
	RetrievalClass    string `json:"retrieval_class"`
}

// graphSearchEdgeGroup collapses repeated structural relations, such as many
// im_reply edges inside one IM_DAY node, while retaining every edge ID so an
// agent can inspect each underlying edge Detail before using it as evidence.
type graphSearchEdgeGroup struct {
	RelationType string   `json:"relation_type,omitempty"`
	FromNodeID   string   `json:"from_node_id,omitempty"`
	ToNodeID     string   `json:"to_node_id,omitempty"`
	FirstSeenHop int      `json:"first_seen_hop,omitempty"`
	EdgeCount    int      `json:"edge_count"`
	DetailCount  int      `json:"detail_count"`
	SelfLoop     bool     `json:"self_loop"`
	EdgeIDs      []string `json:"edge_ids"`
}

func buildGraphSearchEvidenceIndex(candidates []graphSearchCandidate, nodes, edges []map[string]interface{}) graphSearchEvidenceIndex {
	index := graphSearchEvidenceIndex{
		CandidateSource:    "knowledge_qa.passages",
		PermissionFiltered: true,
		CandidateCount:     len(candidates),
		NodeDetails:        make([]graphSearchNodeDetailRef, 0, len(nodes)),
		EdgeDetails:        make([]graphSearchEdgeDetailRef, 0, len(edges)),
		EdgeGroups:         []graphSearchEdgeGroup{},
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Content) != "" {
			index.CandidateContentCount++
		}
		if !candidate.PermissionFiltered {
			index.PermissionFiltered = false
		}
	}

	nodeIDs := make(map[string]struct{}, len(nodes))
	for nodeIndex, node := range nodes {
		nodeID, _ := graphOneHopNonEmptyString(node["node_id"])
		if nodeID != "" {
			nodeIDs[nodeID] = struct{}{}
		}
		present := graphSearchDetailPresent(node["detail"])
		if present {
			index.NodeDetailCount++
		} else {
			index.MissingNodeDetailCount++
		}
		nodeType, _ := graphOneHopInt64(node["node_type"])
		firstSeenHop, _ := graphOneHopInt64(node["first_seen_hop"])
		retrievalClass := graphSearchRetrievalClass(node["retrieved_via"])
		switch retrievalClass {
		case "one_hop_only":
			index.OneHopOnlyNodeCount++
		case "graph_query_only":
			index.GraphQueryOnlyNodeCount++
		case "corroborated":
			index.CorroboratedNodeCount++
		}
		index.NodeDetails = append(index.NodeDetails, graphSearchNodeDetailRef{
			NodeID:            nodeID,
			NodeType:          nodeType,
			FirstSeenHop:      int(firstSeenHop),
			DetailPath:        fmt.Sprintf("nodes[%d].detail", nodeIndex),
			DetailPresent:     present,
			DetailFormat:      graphSearchDetailFormat(node["detail"]),
			DetailBytes:       graphSearchDetailSize(node["detail"]),
			SearchOriginCount: len(graphSearchOrigins(node["search_origins"])),
			RetrievalClass:    retrievalClass,
		})
	}

	groups := make(map[string]*graphSearchEdgeGroup)
	for edgeIndex, edge := range edges {
		edgeID, _ := graphOneHopNonEmptyString(edge["edge_id"])
		relationType := strings.TrimSpace(firstGraphSearchString(edge, "relation_type", "type"))
		fromNodeID := strings.TrimSpace(firstGraphSearchString(edge, "from_node_id", "source_node_id"))
		toNodeID := strings.TrimSpace(firstGraphSearchString(edge, "to_node_id", "target_node_id"))
		firstSeenHop, _ := graphOneHopInt64(edge["first_seen_hop"])
		present := graphSearchDetailPresent(edge["detail"])
		selfLoop := fromNodeID != "" && fromNodeID == toNodeID
		_, fromPresent := nodeIDs[fromNodeID]
		_, toPresent := nodeIDs[toNodeID]
		retrievalClass := graphSearchRetrievalClass(edge["retrieved_via"])
		switch retrievalClass {
		case "one_hop_only":
			index.OneHopOnlyEdgeCount++
		case "graph_query_only":
			index.GraphQueryOnlyEdgeCount++
		case "corroborated":
			index.CorroboratedEdgeCount++
		}
		if present {
			index.EdgeDetailCount++
		} else {
			index.MissingEdgeDetailCount++
		}
		if selfLoop {
			index.SelfLoopEdgeCount++
		}
		index.EdgeDetails = append(index.EdgeDetails, graphSearchEdgeDetailRef{
			EdgeID:            edgeID,
			RelationType:      relationType,
			FromNodeID:        fromNodeID,
			ToNodeID:          toNodeID,
			FirstSeenHop:      int(firstSeenHop),
			DetailPath:        fmt.Sprintf("edges[%d].detail", edgeIndex),
			DetailPresent:     present,
			DetailFormat:      graphSearchDetailFormat(edge["detail"]),
			DetailBytes:       graphSearchDetailSize(edge["detail"]),
			SelfLoop:          selfLoop,
			FromNodePresent:   fromPresent,
			ToNodePresent:     toPresent,
			SearchOriginCount: len(graphSearchOrigins(edge["search_origins"])),
			RetrievalClass:    retrievalClass,
		})

		groupKey := strings.Join([]string{relationType, fromNodeID, toNodeID}, "\x00")
		group := groups[groupKey]
		if group == nil {
			group = &graphSearchEdgeGroup{
				RelationType: relationType,
				FromNodeID:   fromNodeID,
				ToNodeID:     toNodeID,
				FirstSeenHop: int(firstSeenHop),
				SelfLoop:     selfLoop,
			}
			groups[groupKey] = group
		}
		group.EdgeCount++
		if present {
			group.DetailCount++
		}
		if group.FirstSeenHop == 0 || (firstSeenHop > 0 && int(firstSeenHop) < group.FirstSeenHop) {
			group.FirstSeenHop = int(firstSeenHop)
		}
		if edgeID != "" {
			group.EdgeIDs = appendUniqueGraphSearchString(group.EdgeIDs, edgeID)
		}
	}

	index.EdgeGroups = make([]graphSearchEdgeGroup, 0, len(groups))
	for _, group := range groups {
		sort.Strings(group.EdgeIDs)
		index.EdgeGroups = append(index.EdgeGroups, *group)
	}
	sort.Slice(index.EdgeGroups, func(i, j int) bool {
		left := index.EdgeGroups[i]
		right := index.EdgeGroups[j]
		if left.RelationType != right.RelationType {
			return left.RelationType < right.RelationType
		}
		if left.FromNodeID != right.FromNodeID {
			return left.FromNodeID < right.FromNodeID
		}
		return left.ToNodeID < right.ToNodeID
	})
	return index
}

func graphSearchDetailPresent(detail interface{}) bool {
	switch value := detail.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case map[string]interface{}:
		if len(value) == 0 {
			return false
		}
		if content, exists := value["content"]; exists {
			return graphSearchDetailPresent(content)
		}
		return true
	case []interface{}:
		return len(value) > 0
	default:
		return true
	}
}

func graphSearchDetailFormat(detail interface{}) string {
	value, _ := detail.(map[string]interface{})
	return strings.TrimSpace(firstGraphSearchString(value, "format"))
}

func graphSearchDetailSize(detail interface{}) int {
	if detail == nil {
		return 0
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return 0
	}
	return len(encoded)
}

func firstGraphSearchString(value map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		text, _ := graphOneHopNonEmptyString(value[key])
		if text != "" {
			return text
		}
	}
	return ""
}
