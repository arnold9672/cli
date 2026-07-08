// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/core"
	"code.byted.org/lark_search/larksuite-cli/internal/httpmock"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

func TestShortcuts(t *testing.T) {
	got := Shortcuts()
	if len(got) != 2 {
		t.Fatalf("len(Shortcuts()) = %d, want 2", len(got))
	}
	if got[0].Command != "+list" || got[1].Command != "+get" {
		t.Fatalf("commands = %q, %q; want +list, +get", got[0].Command, got[1].Command)
	}
}

func TestMemoryDryRunShapes(t *testing.T) {
	cases := []struct {
		name string
		s    common.Shortcut
		args []string
		want []string
	}{
		{
			name: "list",
			s:    MemoryList,
			args: []string{"+list", "--dry-run", "--as", "user"},
			want: []string{"GET", "/open-apis/search/v2/memory_hub/list_memory"},
		},
		{
			name: "get default full",
			s:    MemoryGet,
			args: []string{"+get", "--memory-key", "personal_memory_snapshot", "--dry-run", "--as", "user"},
			want: []string{"POST", "/open-apis/search/v2/memory_hub/get_memory", "personal_memory_snapshot", "payload_mode", "full"},
		},
		{
			name: "get variant summary",
			s:    MemoryGet,
			args: []string{"+get", "--memory-key", "personal_memory_snapshot", "--variant-key", "v2", "--payload-mode", "summary", "--dry-run", "--as", "user"},
			want: []string{"variant_key", "v2", "payload_mode", "summary"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
			if err := runMemoryShortcut(t, tc.s, tc.args, f, stdout); err != nil {
				t.Fatalf("dry-run: %v", err)
			}
			got := stdout.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("dry-run output missing %q: %s", want, got)
				}
			}
		})
	}
}

func TestMemoryGetValidation(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
	err := runMemoryShortcut(t, MemoryGet, []string{"+get", "--memory-key", "   ", "--as", "user"}, f, stdout)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	var validation *errs.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got %T: %v", err, err)
	}
	if validation.Param != "--memory-key" {
		t.Fatalf("param = %q, want --memory-key", validation.Param)
	}
}

func TestMemoryListExecuteUnwrapsData(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, memoryTestConfig(t))
	stub := &httpmock.Stub{
		Method: "GET",
		URL:    "/open-apis/search/v2/memory_hub/list_memory",
		Body: map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"memories": []interface{}{
						map[string]interface{}{
							"memory_key":          "personal_memory_snapshot",
							"name":                "Personal Memory Snapshot",
							"default_variant_key": "default",
							"status":              "ready",
						},
					},
				},
			},
		},
	}
	reg.Register(stub)

	if err := runMemoryShortcut(t, MemoryList, []string{"+list", "--as", "user", "--format", "json"}, f, stdout); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := stub.CapturedHeaders.Get("x-tt-env"); got != memoryTTEnv {
		t.Fatalf("x-tt-env = %q, want %q", got, memoryTTEnv)
	}
	got := stdout.String()
	if !strings.Contains(got, `"memories"`) || !strings.Contains(got, "personal_memory_snapshot") {
		t.Fatalf("stdout missing unwrapped memories: %s", got)
	}
	if strings.Contains(got, `"data":{"data"`) {
		t.Fatalf("stdout should unwrap nested data layer: %s", got)
	}
}

func TestMemoryGetExecuteSendsFullPayloadMode(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, memoryTestConfig(t))
	stub := &httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/search/v2/memory_hub/get_memory",
		Body: map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"memory_key":   "personal_memory_snapshot",
					"variant_key":  "default",
					"status":       "ready",
					"payload_type": "personal_memory_snapshot",
					"payload":      "{\"summary\":\"hello\"}",
				},
			},
		},
	}
	reg.Register(stub)

	if err := runMemoryShortcut(t, MemoryGet, []string{"+get", "--memory-key", "personal_memory_snapshot", "--as", "user", "--format", "json"}, f, stdout); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !bytes.Contains(stub.CapturedBody, []byte(`"payload_mode":"full"`)) {
		t.Fatalf("request body should include default payload_mode=full, got %s", string(stub.CapturedBody))
	}
	if got := stub.CapturedHeaders.Get("x-tt-env"); got != memoryTTEnv {
		t.Fatalf("x-tt-env = %q, want %q", got, memoryTTEnv)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"memory_key"`)) {
		t.Fatalf("stdout should include memory data, got %s", stdout.String())
	}
}

func TestMemoryAPIFailureTyped(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, memoryTestConfig(t))
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/open-apis/search/v2/memory_hub/list_memory",
		Body:   map[string]interface{}{"code": 123456, "msg": "memory rejected"},
	})

	err := runMemoryShortcut(t, MemoryList, []string{"+list", "--as", "user"}, f, stdout)
	if err == nil {
		t.Fatalf("expected API error")
	}
	p, ok := errs.ProblemOf(err)
	if !ok {
		t.Fatalf("expected typed problem, got %T: %v", err, err)
	}
	if p.Category != errs.CategoryAPI || p.Code != 123456 {
		t.Fatalf("problem = %s code=%d, want api code=123456", p.Category, p.Code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should stay empty on API failure, got %q", stdout.String())
	}
}

func memoryTestConfig(t *testing.T) *core.CliConfig {
	t.Helper()
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	return &core.CliConfig{
		AppID:               "cli_dummy_app",
		AppSecret:           "cli_dummy_secret",
		Brand:               core.BrandFeishu,
		DefaultAs:           core.AsUser,
		UserOpenId:          "ou_test",
		SupportedIdentities: 1,
	}
}

func runMemoryShortcut(t *testing.T, s common.Shortcut, args []string, f *cmdutil.Factory, stdout *bytes.Buffer) error {
	t.Helper()
	parent := &cobra.Command{Use: "memory"}
	s.Mount(parent, f)
	parent.SetArgs(args)
	parent.SilenceErrors = true
	parent.SilenceUsage = true
	if stdout != nil {
		stdout.Reset()
	}
	return parent.ExecuteContext(context.Background())
}
