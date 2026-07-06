// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import "code.byted.org/lark_search/larksuite-cli/shortcuts/common"

const (
	memoryService       = "memory"
	memoryScope         = "search:message"
	memoryAPIBasePath   = "/open-apis/search/v2/memory_hub"
	memoryTTEnv         = "ppe_memory_hub"
	payloadModeMetadata = "metadata"
	payloadModeSummary  = "summary"
	payloadModeFull     = "full"
)

// Shortcuts returns all Memory Hub shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		MemoryList,
		MemoryGet,
	}
}
