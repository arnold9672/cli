// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/internal/cmdutil"
	"code.byted.org/lark_search/larksuite-cli/internal/httpmock"
)

func TestMemoryWritingStyleDryRun(t *testing.T) {
	t.Setenv(envMemoryTTEnv, "")
	f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
	if err := runMemoryShortcut(t, MemoryWritingStyle, []string{
		writingStyleCommand,
		"--dry-run",
		"--as", "user",
		"--format", "json",
	}, f, stdout); err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	got := stdout.String()
	if value := gjson.Get(got, "api.0.method").String(); value != "POST" {
		t.Fatalf("method = %q, want POST; output=%s", value, got)
	}
	if value := gjson.Get(got, "api.0.url").String(); value != memoryAPIBasePath+"/get_memory" {
		t.Fatalf("url = %q; output=%s", value, got)
	}
	if value := gjson.Get(got, "api.0.body.memory_key").String(); value != writingStyleMemoryKey {
		t.Fatalf("memory_key = %q; output=%s", value, got)
	}
	if value := gjson.Get(got, "api.0.body.variant_key").String(); value != writingStyleVariantKey {
		t.Fatalf("variant_key = %q; output=%s", value, got)
	}
	if value := gjson.Get(got, "api.0.body.payload_mode").String(); value != payloadModeFull {
		t.Fatalf("payload_mode = %q; output=%s", value, got)
	}
	if value := gjson.Get(got, "headers.X-Tt-Env").String(); value != writingStyleDefaultEnv {
		t.Fatalf("X-Tt-Env = %q; output=%s", value, got)
	}
	if value := gjson.Get(got, "headers.Destination-Idc").String(); value != writingStyleDefaultIDC {
		t.Fatalf("Destination-Idc = %q; output=%s", value, got)
	}
}

func TestMemoryWritingStyleExecuteParsesPayload(t *testing.T) {
	t.Setenv(envMemoryTTEnv, "")
	f, stdout, _, reg := cmdutil.TestFactory(t, memoryTestConfig(t))
	stub := &httpmock.Stub{
		Method: "POST",
		URL:    memoryAPIBasePath + "/get_memory",
		Body: map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"memory_key":   writingStyleMemoryKey,
					"variant_key":  writingStyleVariantKey,
					"status":       "ready",
					"payload_type": "agentic_memory_viewer",
					"payload":      testWritingStylePayload(t),
					"metadata": map[string]interface{}{
						"namespace": "writing-style-test",
					},
				},
			},
		},
	}
	reg.Register(stub)

	if err := runMemoryShortcut(t, MemoryWritingStyle, []string{
		writingStyleCommand,
		"--as", "user",
		"--format", "json",
	}, f, stdout); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := stub.CapturedHeaders.Get("x-tt-env"); got != writingStyleDefaultEnv {
		t.Fatalf("x-tt-env = %q, want %q", got, writingStyleDefaultEnv)
	}
	if got := stub.CapturedHeaders.Get("destination-idc"); got != writingStyleDefaultIDC {
		t.Fatalf("destination-idc = %q, want %q", got, writingStyleDefaultIDC)
	}
	for _, want := range []string{
		`"memory_key":"personal_memory_snapshot"`,
		`"variant_key":"agentic_v1_writing_pattn_v1"`,
		`"payload_mode":"full"`,
	} {
		if !bytes.Contains(stub.CapturedBody, []byte(want)) {
			t.Fatalf("request body missing %s: %s", want, string(stub.CapturedBody))
		}
	}

	got := stdout.String()
	checks := map[string]string{
		"data.memory_key":                                   writingStyleMemoryKey,
		"data.variant_key":                                  writingStyleVariantKey,
		"data.environment":                                  writingStyleDefaultEnv,
		"data.namespace":                                    "writing-style-test",
		"data.writing_style.file_name":                      writingStyleFileName,
		"data.writing_style.title":                          "写作风格",
		"data.writing_style.sections.0.title":               "跨场景稳定特征",
		"data.writing_style.sections.0.subsections.0.title": "信息组织",
		"data.writing_style.sections.1.title":               "场景化写作模式",
		"data.writing_style.available_scenarios.0":          "技术方案",
		"data.references.documents.0.token":                 "DocToken123456789012345678901",
		"data.references.documents.0.title":                 "参考技术方案",
	}
	for path, want := range checks {
		if value := gjson.Get(got, path).String(); value != want {
			t.Fatalf("%s = %q, want %q; output=%s", path, value, want, got)
		}
	}
	if count := gjson.Get(got, "data.ai_guidance.steps.#").Int(); count < 4 {
		t.Fatalf("general AI guidance step count = %d, want at least 4; output=%s", count, got)
	}
	if count := gjson.Get(got, "data.ai_guidance.guardrails.#").Int(); count < 4 {
		t.Fatalf("ai guidance guardrail count = %d, want at least 4; output=%s", count, got)
	}
	queryPrompt := gjson.Get(got, "data.ai_guidance.prompt_template").String()
	for _, required := range []string{"{query}", "事实", "创建或修改文档", "完整代表性区块"} {
		if !strings.Contains(queryPrompt, required) {
			t.Fatalf("general query prompt missing %q: %s", required, queryPrompt)
		}
	}
	if count := gjson.Get(got, "data.ai_guidance.document_steps.#").Int(); count < 5 {
		t.Fatalf("document step count = %d, want at least 5; output=%s", count, got)
	}
	if step := gjson.Get(got, "data.ai_guidance.document_steps.1").String(); !strings.Contains(step, "必须") || !strings.Contains(step, "完整代表性区块") {
		t.Fatalf("document reference step is not strict enough: %s", step)
	}
	if count := gjson.Get(got, "data.ai_guidance.document_guardrails.#").Int(); count < 4 {
		t.Fatalf("document guardrail count = %d, want at least 4; output=%s", count, got)
	}
	if count := gjson.Get(got, "data.ai_guidance.template_requirements.#").Int(); count != 8 {
		t.Fatalf("template requirement count = %d, want 8; output=%s", count, got)
	}
	if count := gjson.Get(got, "data.ai_guidance.verification_checklist.#").Int(); count < 6 {
		t.Fatalf("verification checklist count = %d, want at least 6; output=%s", count, got)
	}
	for _, required := range []string{
		"唯一格式模板",
		"完整代表性区块",
		"严格复刻参考文档的格式骨架",
		"创建后回读并逐项对照验证",
	} {
		if prompt := gjson.Get(got, "data.ai_guidance.document_rewrite_prompt_template").String(); !strings.Contains(prompt, required) {
			t.Fatalf("document rewrite prompt template missing %q: %s", required, prompt)
		}
	}
}

func TestMemoryWritingStyleUsesTTEnvOverride(t *testing.T) {
	t.Setenv(envMemoryTTEnv, "custom_writing_lane")
	f, stdout, _, reg := cmdutil.TestFactory(t, memoryTestConfig(t))
	stub := &httpmock.Stub{
		Method: "POST",
		URL:    memoryAPIBasePath + "/get_memory",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"memory_key":  writingStyleMemoryKey,
				"variant_key": writingStyleVariantKey,
				"status":      "ready",
				"payload":     testWritingStylePayload(t),
			},
		},
	}
	reg.Register(stub)

	if err := runMemoryShortcut(t, MemoryWritingStyle, []string{
		writingStyleCommand,
		"--as", "user",
		"--format", "json",
	}, f, stdout); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := stub.CapturedHeaders.Get("x-tt-env"); got != "custom_writing_lane" {
		t.Fatalf("x-tt-env = %q, want custom_writing_lane", got)
	}
	if got := gjson.Get(stdout.String(), "data.environment").String(); got != "custom_writing_lane" {
		t.Fatalf("output environment = %q, want custom_writing_lane", got)
	}
}

func TestMemoryWritingStyleRejectsNonReadyMemory(t *testing.T) {
	_, err := parseWritingStyleMemory(map[string]interface{}{
		"memory_key":  writingStyleMemoryKey,
		"variant_key": writingStyleVariantKey,
		"status":      "building",
		"payload":     testWritingStylePayload(t),
	}, writingStyleDefaultEnv)
	if err == nil {
		t.Fatal("expected non-ready error")
	}
	var validation *errs.ValidationError
	if !errors.As(err, &validation) || validation.Subtype != errs.SubtypeFailedPrecondition {
		t.Fatalf("error = %T %v, want validation/failed_precondition", err, err)
	}
}

func TestMemoryWritingStyleRejectsMalformedPayload(t *testing.T) {
	_, err := parseWritingStyleMemory(map[string]interface{}{
		"memory_key":  writingStyleMemoryKey,
		"variant_key": writingStyleVariantKey,
		"status":      "ready",
		"payload":     "not-json",
	}, writingStyleDefaultEnv)
	if err == nil {
		t.Fatal("expected malformed payload error")
	}
	var internal *errs.InternalError
	if !errors.As(err, &internal) || internal.Subtype != errs.SubtypeInvalidResponse {
		t.Fatalf("error = %T %v, want internal/invalid_response", err, err)
	}
}

func TestMemoryWritingStyleRejectsUnsupportedOutputFormat(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, memoryTestConfig(t))
	err := runMemoryShortcut(t, MemoryWritingStyle, []string{
		writingStyleCommand,
		"--as", "user",
		"--format", "table",
	}, f, stdout)
	if err == nil {
		t.Fatal("expected output-format validation error")
	}
	var validation *errs.ValidationError
	if !errors.As(err, &validation) || validation.Param != "--format" {
		t.Fatalf("error = %T %v, want --format validation error", err, err)
	}
}

func TestDecodeWritingStyleFilesAcceptsViewerPayload(t *testing.T) {
	file := testWritingStyleFile()
	raw, err := json.Marshal(map[string]interface{}{
		"files": []writingStylePayloadFile{file},
	})
	if err != nil {
		t.Fatalf("marshal viewer payload: %v", err)
	}
	files, err := decodeWritingStyleFiles(string(raw))
	if err != nil {
		t.Fatalf("decode viewer payload: %v", err)
	}
	if len(files) != 1 || files[0].FileName != writingStyleFileName {
		t.Fatalf("files = %#v", files)
	}
}

func testWritingStylePayload(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(testWritingStyleFile())
	if err != nil {
		t.Fatalf("marshal writing-style payload: %v", err)
	}
	return string(raw)
}

func testWritingStyleFile() writingStylePayloadFile {
	docSource := map[string]string{
		"2026-09-01-DocToken123456789012345678901": "参考技术方案",
	}
	return writingStylePayloadFile{
		FileName: writingStyleFileName,
		Items: []writingStylePayloadItem{
			{ID: "title", Type: "h1", Content: "写作风格"},
			{ID: "stable", Type: "h2", Content: "跨场景稳定特征"},
			{ID: "organization", Type: "h3", Content: "信息组织"},
			{
				ID:      "rule-1",
				Type:    "text",
				Content: "先给结论，再展开依据。",
				Source:  writingStyleItemSources{Documents: docSource},
			},
			{ID: "scenarios", Type: "h2", Content: "场景化写作模式"},
			{ID: "tech", Type: "h3", Content: "技术方案"},
			{
				ID:      "rule-2",
				Type:    "text",
				Content: "使用结论、边界、方案和检查项的结构。",
				Source:  writingStyleItemSources{Documents: docSource},
			},
		},
	}
}

func TestWritingStyleDocumentToken(t *testing.T) {
	if got := writingStyleDocumentToken("2026-08-05-SQ9wdDaSSoQBD3xaVu6cq0Sxnzh"); got != "SQ9wdDaSSoQBD3xaVu6cq0Sxnzh" {
		t.Fatalf("token = %q", got)
	}
	if got := writingStyleDocumentToken("plain-token"); !strings.EqualFold(got, "plain-token") {
		t.Fatalf("plain token = %q", got)
	}
}
