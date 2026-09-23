// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"io"
	"strings"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/output"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

// MemoryGet gets one Memory Hub entry by memory_key.
var MemoryGet = common.Shortcut{
	Service:     memoryService,
	Command:     "+get",
	Description: "Get one Memory Hub entry by memory key",
	Risk:        "read",
	Scopes:      []string{memoryScope},
	AuthTypes:   []string{"user"},
	HasFormat:   true,
	Tips: []string{
		"Example: lark-cli memory +get --memory-key personal_memory_snapshot --as user",
		"Tip: --payload-mode defaults to full for this fork.",
	},
	Flags: []common.Flag{
		{Name: "memory-key", Desc: "Memory Hub memory_key", Required: true},
		{Name: "variant-key", Desc: "optional Memory Hub variant_key"},
		{Name: "payload-mode", Default: payloadModeFull, Desc: "payload mode", Enum: []string{payloadModeMetadata, payloadModeSummary, payloadModeFull}},
	},
	Validate: func(ctx context.Context, rctx *common.RuntimeContext) error {
		if strings.TrimSpace(rctx.Str("memory-key")) == "" {
			return errs.NewValidationError(errs.SubtypeInvalidArgument, "--memory-key is required").WithParam("--memory-key")
		}
		return nil
	},
	DryRun: func(ctx context.Context, rctx *common.RuntimeContext) *common.DryRunAPI {
		return common.NewDryRunAPI().
			POST(memoryAPIBasePath + "/get_memory").
			Desc("Get one Memory Hub entry").
			Body(buildGetMemoryBody(rctx))
	},
	Execute: func(ctx context.Context, rctx *common.RuntimeContext) error {
		data, err := callMemoryAPITyped(rctx, "POST", memoryAPIBasePath+"/get_memory", buildGetMemoryBody(rctx))
		if err != nil {
			return err
		}
		out := unwrapMemoryData(data)
		rctx.OutFormat(out, nil, func(w io.Writer) {
			output.PrintTable(w, []map[string]interface{}{{
				"memory_key":   out["memory_key"],
				"variant_key":  out["variant_key"],
				"status":       out["status"],
				"payload_type": out["payload_type"],
				"detail_entry": out["detail_entry"],
			}})
		})
		return nil
	},
}

func buildGetMemoryBody(rctx *common.RuntimeContext) map[string]interface{} {
	body := map[string]interface{}{
		"memory_key":   strings.TrimSpace(rctx.Str("memory-key")),
		"payload_mode": strings.TrimSpace(rctx.Str("payload-mode")),
	}
	if variantKey := strings.TrimSpace(rctx.Str("variant-key")); variantKey != "" {
		body["variant_key"] = variantKey
	}
	return body
}
