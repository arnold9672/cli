// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"io"

	"code.byted.org/lark_search/larksuite-cli/internal/output"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

// MemoryList lists Memory Hub entries visible to the authenticated user.
var MemoryList = common.Shortcut{
	Service:     memoryService,
	Command:     "+list",
	Description: "List Memory Hub entries visible to the authenticated user",
	Risk:        "read",
	Scopes:      []string{memoryScope},
	AuthTypes:   []string{"user"},
	HasFormat:   true,
	Tips: []string{
		"Example: lark-cli memory +list --as user",
		"Tip: use memory +get --memory-key <memory_key> to inspect one entry.",
	},
	DryRun: func(ctx context.Context, rctx *common.RuntimeContext) *common.DryRunAPI {
		return common.NewDryRunAPI().
			GET(memoryAPIBasePath + "/list_memory").
			Desc("List Memory Hub entries")
	},
	Execute: func(ctx context.Context, rctx *common.RuntimeContext) error {
		data, err := callMemoryAPITyped(rctx, "GET", memoryAPIBasePath+"/list_memory", map[string]interface{}{})
		if err != nil {
			return err
		}
		out := unwrapMemoryData(data)
		rctx.OutFormat(out, nil, func(w io.Writer) {
			output.PrintTable(w, memoryRows(out))
		})
		return nil
	},
}

func memoryRows(data map[string]interface{}) []map[string]interface{} {
	raw, _ := data["memories"].([]interface{})
	rows := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"memory_key":          m["memory_key"],
			"name":                firstString(m, "name", "display_name"),
			"default_variant_key": m["default_variant_key"],
			"status":              m["status"],
			"description":         m["description"],
			"showcase":            m["showcase"],
		})
	}
	return rows
}
