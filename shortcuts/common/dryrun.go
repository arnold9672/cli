// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package common

import "code.byted.org/lark_search/larksuite-cli/internal/cmdutil"

// Type aliases so all existing shortcut code continues to use common.DryRunAPI
// without any changes. The real implementation lives in internal/cmdutil.
type DryRunAPI = cmdutil.DryRunAPI
type DryRunAPICall = cmdutil.DryRunAPICall

var NewDryRunAPI = cmdutil.NewDryRunAPI
