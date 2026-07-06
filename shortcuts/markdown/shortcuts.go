// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package markdown

import "code.byted.org/lark_search/larksuite-cli/shortcuts/common"

// Shortcuts returns all markdown shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		MarkdownCreate,
		MarkdownDiff,
		MarkdownFetch,
		MarkdownPatch,
		MarkdownOverwrite,
	}
}
