// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package contact

import "code.byted.org/lark_search/larksuite-cli/shortcuts/common"

// Shortcuts returns all contact shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		ContactSearchUser,
		ContactGetUser,
	}
}
