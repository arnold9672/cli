// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"reflect"
	"strings"
)

// Only Markdown is body evidence. Other raw wire fields are not compatibility inputs.
func graphDetailMarkdown(value interface{}) string {
	detail, _ := value.(map[string]interface{})
	markdown, _ := detail["markdown"].(string)
	return markdown
}

func graphDetailContentPresent(value interface{}) bool {
	return strings.TrimSpace(graphDetailMarkdown(value)) != ""
}

// Count UTF-8 Markdown bytes, excluding provenance and other raw wire metadata.
func graphDetailContentBytes(value interface{}) int {
	return len([]byte(graphDetailMarkdown(value)))
}

func graphDetailSourceCount(value interface{}) int {
	detail, _ := value.(map[string]interface{})
	sources, _ := detail["source"].([]interface{})
	count := 0
	for _, item := range sources {
		source, _ := item.(map[string]interface{})
		id, _ := source["entity_id"].(string)
		kind, _ := source["entity_type"].(string)
		if strings.TrimSpace(id) != "" || strings.TrimSpace(kind) != "" {
			count++
		}
	}
	return count
}

// Merge complementary Detail fields rather than choosing one entire object.
// Existing nonempty values win conflicts; source lists are a stable union. Neither
// input Detail is mutated, and unknown fields survive for forward compatibility.
func mergeGraphDetail(existing, candidate interface{}) (interface{}, bool) {
	left, leftOK := existing.(map[string]interface{})
	right, rightOK := candidate.(map[string]interface{})
	if !leftOK || !rightOK {
		if !graphDetailValuePresent(existing) && candidate != nil && !reflect.DeepEqual(existing, candidate) {
			return cloneGraphDetailValue(candidate), true
		}
		return existing, false
	}
	merged := cloneGraphDetailValue(left).(map[string]interface{})
	for key, value := range right {
		if key == "source" {
			if sources, ok := value.([]interface{}); ok {
				if previous, ok := merged[key].([]interface{}); ok {
					for _, source := range sources {
						duplicate := false
						for _, kept := range previous {
							if reflect.DeepEqual(kept, source) {
								duplicate = true
								break
							}
						}
						if !duplicate {
							previous = append(previous, cloneGraphDetailValue(source))
						}
					}
					merged[key] = previous
					continue
				}
			}
		}
		if !graphDetailValuePresent(merged[key]) {
			merged[key] = cloneGraphDetailValue(value)
		}
	}
	return merged, !reflect.DeepEqual(left, merged)
}

func graphDetailValuePresent(value interface{}) bool {
	switch value := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case map[string]interface{}:
		return len(value) > 0
	case []interface{}:
		return len(value) > 0
	default:
		return true
	}
}

func cloneGraphDetailValue(value interface{}) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(value))
		for key, field := range value {
			out[key] = cloneGraphDetailValue(field)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(value))
		for i, field := range value {
			out[i] = cloneGraphDetailValue(field)
		}
		return out
	default:
		return value
	}
}
