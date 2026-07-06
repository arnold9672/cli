// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package okr

import (
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

// Shortcuts returns all okr shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		OKRListCycles,
		OKRCycleDetail,
		OKRListProgress,
		OKRGetProgressRecord,
		OKRCreateProgressRecord,
		OKRUpdateProgressRecord,
		OKRDeleteProgressRecord,
		OKRUploadImage,
		OKRBatchCreate,
		OKRReorder,
		OKRWeight,
		OKRIndicatorUpdate,
	}
}
