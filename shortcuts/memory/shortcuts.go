// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"code.byted.org/lark_search/larksuite-cli/internal/deprecation"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const (
	memoryService       = "memory"
	memoryScope         = "memory:hub"
	memoryAPIBasePath   = "/open-apis/search/v2/memory_hub"
	defaultMemoryTTEnv  = "ppe_memory_hub"
	envMemoryTTEnv      = "LARKSUITE_CLI_MEMORY_TT_ENV"
	payloadModeMetadata = "metadata"
	payloadModeSummary  = "summary"
	payloadModeFull     = "full"
)

// Shortcuts returns all Memory Hub shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		MemoryList,
		MemoryGet,
		MemoryGraphRange,
		MemoryGraphOneHop,
		MemoryGraphSearch,
		MemoryWritingStyle,
		MemoryOneHop,
	}
}

func deprecatedShortcutAlias(current common.Shortcut, legacyService, legacyCommand, replacement string) common.Shortcut {
	alias := current
	alias.Service = legacyService
	alias.Command = legacyCommand
	alias.Hidden = true
	notice := &deprecation.Notice{
		Command:     legacyService + " " + legacyCommand,
		Replacement: replacement,
		Skill:       "lark-memory",
	}
	previousOnInvoke := alias.OnInvoke
	alias.OnInvoke = func() {
		if previousOnInvoke != nil {
			previousOnInvoke()
		}
		deprecation.SetPending(notice)
	}
	return alias
}
