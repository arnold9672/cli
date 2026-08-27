// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package common

import (
	"context"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/core"
)

func TestShortcutPreflightRunsAfterOnInvokeBeforeRequiredFlagValidation(t *testing.T) {
	f, _, _, _ := cmdutil.TestFactory(t, nil)
	parent := &cobra.Command{Use: "root", SilenceErrors: true, SilenceUsage: true}
	var calls []string
	shortcut := Shortcut{
		Service: "test",
		Command: "+check",
		Flags:   []Flag{{Name: "required", Required: true}},
		OnInvoke: func() {
			calls = append(calls, "on-invoke")
		},
		Preflight: func(context.Context, *cobra.Command) error {
			calls = append(calls, "preflight")
			return errs.NewValidationError(errs.SubtypeInvalidArgument, "--required is required").
				WithParam("--required")
		},
		Execute: func(context.Context, *RuntimeContext) error {
			calls = append(calls, "execute")
			return nil
		},
	}
	shortcut.Mount(parent, f)
	parent.SetArgs([]string{"+check", "--as", "user"})

	err := parent.Execute()
	assertValidationParam(t, err, "--required")
	if want := []string{"on-invoke", "preflight"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("hook calls = %v, want %v", calls, want)
	}
}

func TestShortcutPrintSchemaSkipsPreflightAndPreservesOnInvokeOrder(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, nil)
	parent := &cobra.Command{Use: "root", SilenceErrors: true, SilenceUsage: true}
	var calls []string
	shortcut := Shortcut{
		Service: "test",
		Command: "+schema",
		Flags:   []Flag{{Name: "required", Required: true}},
		OnInvoke: func() {
			calls = append(calls, "on-invoke")
		},
		Preflight: func(context.Context, *cobra.Command) error {
			calls = append(calls, "preflight")
			return errs.NewValidationError(errs.SubtypeInvalidArgument, "preflight must not run")
		},
		PrintFlagSchema: func(string) ([]byte, error) {
			calls = append(calls, "print-schema")
			return []byte(`{"type":"object"}`), nil
		},
		Execute: func(context.Context, *RuntimeContext) error {
			calls = append(calls, "execute")
			return nil
		},
	}
	shortcut.Mount(parent, f)
	parent.SetArgs([]string{"+schema", "--print-schema"})

	if err := parent.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := []string{"on-invoke", "print-schema"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("hook calls = %v, want %v", calls, want)
	}
	if got := stdout.String(); got != "{\"type\":\"object\"}\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestShortcutWithoutPreflightStillExecutes(t *testing.T) {
	config := &core.CliConfig{
		AppID:               "test_app",
		AppSecret:           "test-secret",
		DefaultAs:           core.AsUser,
		SupportedIdentities: 1,
	}
	f, _, _, _ := cmdutil.TestFactory(t, config)
	parent := &cobra.Command{Use: "root", SilenceErrors: true, SilenceUsage: true}
	executed := false
	shortcut := Shortcut{
		Service: "test",
		Command: "+plain",
		Flags:   []Flag{{Name: "required", Required: true}},
		Execute: func(context.Context, *RuntimeContext) error {
			executed = true
			return nil
		},
	}
	shortcut.Mount(parent, f)
	parent.SetArgs([]string{"+plain", "--required", "value", "--as", "user"})

	if err := parent.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !executed {
		t.Fatal("Execute() was not called")
	}
}
