// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package events wires domain EventKey definitions into the global registry. Blank-import to populate.
package events

import (
	"code.byted.org/lark_search/larksuite-cli/events/im"
	"code.byted.org/lark_search/larksuite-cli/events/minutes"
	"code.byted.org/lark_search/larksuite-cli/events/task"
	"code.byted.org/lark_search/larksuite-cli/events/vc"
	"code.byted.org/lark_search/larksuite-cli/events/whiteboard"
	"code.byted.org/lark_search/larksuite-cli/internal/event"
)

// Mail is intentionally omitted in this phase.
func init() {
	all := [][]event.KeyDefinition{
		im.Keys(),
		minutes.Keys(),
		task.Keys(),
		vc.Keys(),
		whiteboard.Keys(),
	}
	for _, keys := range all {
		for _, k := range keys {
			event.RegisterKey(k)
		}
	}
}
