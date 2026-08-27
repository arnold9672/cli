// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"

	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/core"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

func memoryTestConfig(t *testing.T) *core.CliConfig {
	t.Helper()
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	return &core.CliConfig{
		AppID:               "cli_dummy_app",
		AppSecret:           "cli_dummy_secret",
		Brand:               core.BrandFeishu,
		DefaultAs:           core.AsUser,
		UserOpenId:          "ou_graph_test_user",
		SupportedIdentities: 1,
	}
}

func runMemoryShortcut(t *testing.T, s common.Shortcut, args []string, f *cmdutil.Factory, stdout *bytes.Buffer) error {
	t.Helper()
	return runMemoryShortcutContext(t, context.Background(), s, args, f, stdout)
}

func runMemoryShortcutContext(t *testing.T, ctx context.Context, s common.Shortcut, args []string, f *cmdutil.Factory, stdout *bytes.Buffer) error {
	t.Helper()
	parent := &cobra.Command{Use: "memory"}
	s.Mount(parent, f)
	parent.SetArgs(args)
	parent.SilenceErrors = true
	parent.SilenceUsage = true
	if stdout != nil {
		stdout.Reset()
	}
	return parent.ExecuteContext(ctx)
}
