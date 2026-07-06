// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT
//
// lark-cli — Feishu/Lark CLI tool (Go implementation).
package main

import (
	"os"

	"code.byted.org/lark_search/larksuite-cli/cmd"

	_ "code.byted.org/lark_search/larksuite-cli/extension/credential/env" // activate env credential provider
)

func main() {
	os.Exit(cmd.Execute())
}
