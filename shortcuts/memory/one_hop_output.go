// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/output"
)

type graphOneHopExpansion struct {
	Kind           string          `json:"kind"`
	Root           graphOneHopRoot `json:"root"`
	UpstreamNodeID string          `json:"upstream_node_id"`
	EdgeID         string          `json:"edge_id,omitempty"`
}

func normalizeGraphOneHopResponse(data map[string]interface{}, spec graphOneHopSpec) (map[string]interface{}, *output.Meta, error) {
	data = unwrapMemoryData(data)
	nodes, err := graphOneHopObjectList(data, "nodes")
	if err != nil {
		return nil, nil, err
	}
	edges, err := graphOneHopObjectList(data, "edges")
	if err != nil {
		return nil, nil, err
	}

	filteredNodeIDs := make(map[string]struct{})
	filteredUserNodeCount := 0
	keptNodes := make([]map[string]interface{}, 0, len(nodes))
	for _, node := range nodes {
		nodeID, err := graphOneHopRequiredString(node, "node_id", "node")
		if err != nil {
			return nil, nil, err
		}
		nodeType, ok := graphOneHopInt64(node["node_type"])
		if !ok {
			return nil, nil, graphOneHopInvalidResponse("node has an invalid node_type")
		}
		if nodeType == graphOneHopUserNodeType {
			filteredNodeIDs[nodeID] = struct{}{}
			filteredUserNodeCount++
			continue
		}
		_, hasRootID := graphOneHopNonEmptyString(node["root_id"])
		node["expandable"] = hasRootID
		keptNodes = append(keptNodes, node)
	}

	filteredUserEdgeCount := 0
	keptEdges := make([]map[string]interface{}, 0, len(edges))
	for _, edge := range edges {
		if _, err := graphOneHopRequiredString(edge, "edge_id", "edge"); err != nil {
			return nil, nil, err
		}
		fromNodeID, err := graphOneHopRequiredString(edge, "from_node_id", "edge")
		if err != nil {
			return nil, nil, err
		}
		toNodeID, err := graphOneHopRequiredString(edge, "to_node_id", "edge")
		if err != nil {
			return nil, nil, err
		}
		if graphOneHopIsUserNodeID(fromNodeID, filteredNodeIDs) || graphOneHopIsUserNodeID(toNodeID, filteredNodeIDs) {
			filteredUserEdgeCount++
			continue
		}
		keptEdges = append(keptEdges, edge)
	}

	annotateGraphOneHopProvenance(keptNodes, keptEdges, spec.Roots)
	detailBytes := graphOneHopDetailBytes(keptNodes, keptEdges)
	rootCount := len(spec.Roots)
	nodeCount := len(keptNodes)
	edgeCount := len(keptEdges)
	hop := spec.Hop
	meta := &output.Meta{
		RootCount:             &rootCount,
		NodeCount:             &nodeCount,
		EdgeCount:             &edgeCount,
		FilteredUserNodeCount: &filteredUserNodeCount,
		FilteredUserEdgeCount: &filteredUserEdgeCount,
		DetailBytes:           &detailBytes,
		TraceID:               spec.TraceID,
		Hop:                   &hop,
	}
	return map[string]interface{}{
		"nodes": graphOneHopInterfaces(keptNodes),
		"edges": graphOneHopInterfaces(keptEdges),
	}, meta, nil
}

func graphOneHopObjectList(data map[string]interface{}, key string) ([]map[string]interface{}, error) {
	if data == nil {
		return nil, graphOneHopInvalidResponse("OneHop response data is missing")
	}
	raw, ok := data[key].([]interface{})
	if !ok {
		return nil, graphOneHopInvalidResponse("OneHop response %s must be a list", key)
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		object, ok := item.(map[string]interface{})
		if !ok {
			return nil, graphOneHopInvalidResponse("OneHop response %s contains a non-object item", key)
		}
		out = append(out, object)
	}
	return out, nil
}

func graphOneHopRequiredString(object map[string]interface{}, key, objectType string) (string, error) {
	value, ok := graphOneHopNonEmptyString(object[key])
	if !ok {
		return "", graphOneHopInvalidResponse("%s has an invalid %s", objectType, key)
	}
	return value, nil
}

func graphOneHopNonEmptyString(value interface{}) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func graphOneHopInt64(value interface{}) (int64, bool) {
	switch value := value.(type) {
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		if uint64(value) > math.MaxInt64 {
			return 0, false
		}
		return int64(value), true
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint64:
		if value > math.MaxInt64 {
			return 0, false
		}
		return int64(value), true
	case float64:
		if math.Trunc(value) != value {
			return 0, false
		}
		parsed, err := strconv.ParseInt(strconv.FormatFloat(value, 'f', -1, 64), 10, 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func graphOneHopIsUserNodeID(nodeID string, filtered map[string]struct{}) bool {
	if _, ok := filtered[nodeID]; ok {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(nodeID)), "user:")
}

func annotateGraphOneHopProvenance(nodes, edges []map[string]interface{}, roots []graphOneHopRoot) {
	nodesByID := make(map[string]map[string]interface{}, len(nodes))
	rootOriginsByNodeID := make(map[string][]graphOneHopExpansion)
	for _, node := range nodes {
		nodeID, _ := graphOneHopNonEmptyString(node["node_id"])
		nodesByID[nodeID] = node
		nodeType, typeOK := graphOneHopInt64(node["node_type"])
		rootID, rootOK := graphOneHopNonEmptyString(node["root_id"])
		if typeOK && rootOK {
			for _, root := range roots {
				if root.NodeType != nodeType || root.RootID != rootID {
					continue
				}
				rootOriginsByNodeID[nodeID] = appendGraphOneHopExpansion(rootOriginsByNodeID[nodeID], graphOneHopExpansion{
					Kind:           "timeline",
					Root:           root,
					UpstreamNodeID: nodeID,
				})
			}
		}
	}

	for nodeID, origins := range rootOriginsByNodeID {
		nodesByID[nodeID]["expanded_from"] = origins
	}
	for _, edge := range edges {
		edgeID, _ := graphOneHopNonEmptyString(edge["edge_id"])
		fromNodeID, _ := graphOneHopNonEmptyString(edge["from_node_id"])
		toNodeID, _ := graphOneHopNonEmptyString(edge["to_node_id"])
		edgeOrigins := make([]graphOneHopExpansion, 0, 2)
		for _, rootOrigin := range rootOriginsByNodeID[fromNodeID] {
			relationOrigin := rootOrigin
			relationOrigin.Kind = "relation"
			relationOrigin.EdgeID = edgeID
			edgeOrigins = appendGraphOneHopExpansion(edgeOrigins, relationOrigin)
			if node := nodesByID[toNodeID]; node != nil {
				node["expanded_from"] = appendGraphOneHopExpansion(graphOneHopExpansions(node["expanded_from"]), relationOrigin)
			}
		}
		for _, rootOrigin := range rootOriginsByNodeID[toNodeID] {
			relationOrigin := rootOrigin
			relationOrigin.Kind = "relation"
			relationOrigin.EdgeID = edgeID
			edgeOrigins = appendGraphOneHopExpansion(edgeOrigins, relationOrigin)
			if node := nodesByID[fromNodeID]; node != nil {
				node["expanded_from"] = appendGraphOneHopExpansion(graphOneHopExpansions(node["expanded_from"]), relationOrigin)
			}
		}
		edge["expanded_from"] = edgeOrigins
	}
	for _, node := range nodes {
		if _, ok := node["expanded_from"]; !ok {
			node["expanded_from"] = []graphOneHopExpansion{}
		}
	}
}

func graphOneHopExpansions(value interface{}) []graphOneHopExpansion {
	values, _ := value.([]graphOneHopExpansion)
	return values
}

func appendGraphOneHopExpansion(values []graphOneHopExpansion, candidate graphOneHopExpansion) []graphOneHopExpansion {
	for _, existing := range values {
		if existing == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func graphOneHopDetailBytes(nodes, edges []map[string]interface{}) int {
	total := 0
	for _, items := range [][]map[string]interface{}{nodes, edges} {
		for _, item := range items {
			detail, _ := item["detail"].(map[string]interface{})
			content, _ := detail["content"].(string)
			total += len([]byte(content))
		}
	}
	return total
}

func graphOneHopInterfaces(items []map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func graphOneHopInvalidResponse(format string, args ...interface{}) error {
	return errs.NewInternalError(errs.SubtypeInvalidResponse, format, args...)
}

func renderGraphOneHopPretty(w io.Writer, out map[string]interface{}, meta *output.Meta) error {
	if _, err := fmt.Fprintf(w, "OneHop hop=%d trace_id=%s roots=%d nodes=%d edges=%d\n",
		graphOneHopMetaInt(meta.Hop), meta.TraceID, graphOneHopMetaInt(meta.RootCount), graphOneHopMetaInt(meta.NodeCount), graphOneHopMetaInt(meta.EdgeCount)); err != nil {
		return err
	}
	return renderGraphDataPretty(w, out)
}

func graphOneHopMetaInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
