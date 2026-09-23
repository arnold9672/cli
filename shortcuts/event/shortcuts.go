// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package event

import "code.byted.org/lark_search/larksuite-cli/shortcuts/common"

// Shortcuts returns all event shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		EventSubscribe,
	}
}
