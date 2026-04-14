// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package base

import (
	"context"

	"github.com/larksuite/cli/shortcuts/common"
)

var BaseRecordShareLinkCreate = common.Shortcut{
	Service:     "base",
	Command:     "+record-share-link-create",
	Description: "Generate a share link for a single record",
	Risk:        "read",
	Scopes:      []string{"base:record:read"},
	AuthTypes:   authTypes(),
	Flags: []common.Flag{
		baseTokenFlag(true),
		tableRefFlag(true),
		recordRefFlag(true),
	},
	Tips: []string{
		`Example: --base-token xxx --table-id tblxxx --record-id recxxx`,
	},
	DryRun: dryRunRecordShare,
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		return executeRecordShare(runtime)
	},
}
