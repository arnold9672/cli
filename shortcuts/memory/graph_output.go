// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type graphWindowResult struct {
	WindowIndex  int                    `json:"window_index"`
	StartTimeSec int64                  `json:"start_time_sec"`
	EndTimeSec   int64                  `json:"end_time_sec"`
	Data         map[string]interface{} `json:"data"`
}

func renderGraphWindowPretty(w io.Writer, out graphWindowResult) error {
	if _, err := fmt.Fprintf(w, "Window %d [%d, %d)\n", out.WindowIndex, out.StartTimeSec, out.EndTimeSec); err != nil {
		return err
	}
	return renderGraphDataPretty(w, out.Data)
}

func renderGraphDataPretty(w io.Writer, data map[string]interface{}) error {
	nodes := graphItems(data, "nodes")
	if _, err := fmt.Fprintf(w, "Nodes (%d)\n", len(nodes)); err != nil {
		return err
	}
	for _, item := range nodes {
		node, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []struct {
			prefix string
			key    string
		}{
			{prefix: "- ", key: "node_id"},
			{prefix: "  ", key: "node_type"},
			{prefix: "  ", key: "source_id"},
			{prefix: "  ", key: "root_id"},
			{prefix: "  ", key: "graph_date"},
			{prefix: "  ", key: "event_time_sec"},
			{prefix: "  ", key: "expandable"},
		} {
			if err := writeGraphField(w, field.prefix, field.key, node[field.key]); err != nil {
				return err
			}
		}
		if err := writeGraphDetail(w, node["detail"]); err != nil {
			return err
		}
		if err := writeGraphJSONField(w, "expanded_from", node["expanded_from"]); err != nil {
			return err
		}
	}

	edges := graphItems(data, "edges")
	if _, err := fmt.Fprintf(w, "Edges (%d)\n", len(edges)); err != nil {
		return err
	}
	for _, item := range edges {
		edge, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []struct {
			prefix string
			key    string
		}{
			{prefix: "- ", key: "edge_id"},
			{prefix: "  ", key: "relation_type"},
			{prefix: "  ", key: "from_node_id"},
			{prefix: "  ", key: "to_node_id"},
			{prefix: "  ", key: "event_time_sec"},
		} {
			if err := writeGraphField(w, field.prefix, field.key, edge[field.key]); err != nil {
				return err
			}
		}
		if err := writeGraphDetail(w, edge["detail"]); err != nil {
			return err
		}
		if err := writeGraphJSONField(w, "expanded_from", edge["expanded_from"]); err != nil {
			return err
		}
	}
	return nil
}

func graphItems(data map[string]interface{}, key string) []interface{} {
	items, _ := data[key].([]interface{})
	return items
}

func writeGraphField(w io.Writer, prefix, key string, value interface{}) error {
	formatted, ok := graphScalar(value)
	if !ok {
		return nil
	}
	_, err := fmt.Fprintf(w, "%s%s: %s\n", prefix, key, formatted)
	return err
}

func graphScalar(value interface{}) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case json.Number:
		return value.String(), true
	case bool:
		return strconv.FormatBool(value), true
	default:
		return "", false
	}
}

func writeGraphJSONField(w io.Writer, key string, value interface{}) error {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "  %s: %s\n", key, encoded)
	return err
}

func writeGraphDetail(w io.Writer, value interface{}) error {
	detail, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	format, formatOK := detail["format"].(string)
	schemaVersion, schemaOK := detail["schema_version"].(string)
	content, contentOK := detail["content"].(string)
	if !formatOK || !schemaOK || !contentOK {
		return nil
	}
	if _, err := fmt.Fprintf(w, "  detail (%s, %s):\n", format, schemaVersion); err != nil {
		return err
	}
	for _, line := range strings.Split(content, "\n") {
		if _, err := fmt.Fprintf(w, "    %s\n", line); err != nil {
			return err
		}
	}
	return nil
}
