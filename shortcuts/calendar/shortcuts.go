// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package calendar

import "code.byted.org/lark_search/larksuite-cli/shortcuts/common"

// Shortcuts returns all calendar shortcuts.
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		CalendarAgenda,
		CalendarCreate,
		CalendarUpdate,
		CalendarFreebusy,
		CalendarRoomFind,
		CalendarRsvp,
		CalendarSuggestion,
		CalendarMeeting,
		CalendarSearchEvent,
	}
}
