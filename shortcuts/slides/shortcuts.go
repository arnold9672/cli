// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package slides

import "code.byted.org/lark_search/larksuite-cli/shortcuts/common"

// Shortcuts returns all slides shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		SlidesCreate,
		SlidesMediaUpload,
		SlidesReplaceSlide,
		SlidesReplacePages,
		SlidesScreenshot,
		SlidesXMLGet,
	}
}
