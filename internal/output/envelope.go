// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package output

// Envelope is the standard success response wrapper.
type Envelope struct {
	OK                 bool                   `json:"ok"`
	Identity           string                 `json:"identity,omitempty"`
	Data               interface{}            `json:"data,omitempty"`
	Meta               *Meta                  `json:"meta,omitempty"`
	ContentSafetyAlert interface{}            `json:"_content_safety_alert,omitempty"`
	Notice             map[string]interface{} `json:"_notice,omitempty"`
}

// Meta carries optional metadata in envelope responses.
type Meta struct {
	Count                 int    `json:"count,omitempty"`
	Rollback              string `json:"rollback,omitempty"`
	RootCount             *int   `json:"root_count,omitempty"`
	NodeCount             *int   `json:"node_count,omitempty"`
	EdgeCount             *int   `json:"edge_count,omitempty"`
	FilteredUserNodeCount *int   `json:"filtered_user_node_count,omitempty"`
	FilteredUserEdgeCount *int   `json:"filtered_user_edge_count,omitempty"`
	DetailBytes           *int   `json:"detail_bytes,omitempty"`
	TookMS                *int64 `json:"took_ms,omitempty"`
	LogID                 string `json:"log_id,omitempty"`
	TraceID               string `json:"trace_id,omitempty"`
	Hop                   *int   `json:"hop,omitempty"`
}

// PendingNotice, if set, returns system-level notices to inject as the
// "_notice" field in JSON output envelopes. Set by cmd/root.go.
// Returns nil when there is nothing to report.
var PendingNotice func() map[string]interface{}

// GetNotice returns the current pending notice for struct-based callers.
// Returns nil when there is nothing to report.
func GetNotice() map[string]interface{} {
	if PendingNotice == nil {
		return nil
	}
	return PendingNotice()
}
