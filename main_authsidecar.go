// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

//go:build authsidecar

package main

import (
	_ "code.byted.org/lark_search/larksuite-cli/extension/credential/sidecar" // activate sidecar credential provider
	_ "code.byted.org/lark_search/larksuite-cli/extension/transport/sidecar"  // activate sidecar transport interceptor
)
