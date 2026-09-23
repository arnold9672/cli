// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"code.byted.org/lark_search/larksuite-cli/errs"
	"code.byted.org/lark_search/larksuite-cli/shortcuts/common"
)

const (
	writingStyleCommand    = "+writing-style"
	writingStyleMemoryKey  = "personal_memory_snapshot"
	writingStyleVariantKey = "agentic_v1_writing_pattn_v1"
	writingStyleDefaultEnv = "ppe_memory_schema"
	writingStyleDefaultIDC = "lf"
	writingStyleFileName   = "写作风格.md"
)

// MemoryWritingStyle reads the current user's writing-style Memory, parses the
// heading/item stream into a stable hierarchy, and returns usage guidance for
// an AI writer. The command intentionally owns the fixed Memory key and
// variant so callers cannot accidentally read a different personal Memory.
var MemoryWritingStyle = common.Shortcut{
	Service:     memoryService,
	Command:     writingStyleCommand,
	Description: "Read the current user's writing style and return AI usage guidance",
	Risk:        "read",
	Scopes:      []string{memoryScope},
	AuthTypes:   []string{"user"},
	Tips: []string{
		"Reads personal_memory_snapshot variant agentic_v1_writing_pattn_v1 with full payload mode.",
		"Defaults to x-tt-env=ppe_memory_schema and destination-idc=lf; LARKSUITE_CLI_MEMORY_TT_ENV overrides only the lane.",
		"Use cross-scene rules globally, then select only the scenario pattern that matches the user's writing task.",
	},
	Validate: func(_ context.Context, rctx *common.RuntimeContext) error {
		if rctx.Format != "json" && rctx.Format != "pretty" {
			return errs.NewValidationError(errs.SubtypeInvalidArgument,
				"--format must be json or pretty for memory +writing-style").WithParam("--format")
		}
		_, err := resolveWritingStyleTTEnv()
		return err
	},
	DryRun: func(_ context.Context, _ *common.RuntimeContext) *common.DryRunAPI {
		ttEnv, err := resolveWritingStyleTTEnv()
		if err != nil {
			return common.NewDryRunAPI().Set("error", err.Error())
		}
		return common.NewDryRunAPI().
			POST(memoryAPIBasePath+"/get_memory").
			Desc("Read and parse the current user's writing-style Memory").
			Body(buildWritingStyleMemoryBody()).
			Set("headers", map[string]string{
				"X-Tt-Env":        ttEnv,
				"Destination-Idc": writingStyleDefaultIDC,
			}).
			Set("output", "parsed writing style, source references, and AI usage guidance")
	},
	Execute: func(_ context.Context, rctx *common.RuntimeContext) error {
		ttEnv, err := resolveWritingStyleTTEnv()
		if err != nil {
			return err
		}
		headers := memoryExtraHeaders()
		headers.Set("x-tt-env", ttEnv)
		headers.Set("destination-idc", writingStyleDefaultIDC)
		data, err := callMemoryAPITypedWithContextAndHeaders(
			rctx,
			rctx.Ctx(),
			"POST",
			memoryAPIBasePath+"/get_memory",
			buildWritingStyleMemoryBody(),
			headers,
		)
		if err != nil {
			return err
		}
		result, err := parseWritingStyleMemory(unwrapMemoryData(data), ttEnv)
		if err != nil {
			return err
		}
		rctx.OutFormat(result, nil, func(w io.Writer) {
			printWritingStylePretty(w, result)
		})
		return nil
	},
}

type writingStylePayload struct {
	FileName string                     `json:"fileName"`
	Items    []writingStylePayloadItem  `json:"items"`
	Files    []writingStylePayloadFile  `json:"files"`
	Latest   *writingStyleLatestPayload `json:"latest"`
}

type writingStyleLatestPayload struct {
	Files []writingStylePayloadFile `json:"files"`
}

type writingStylePayloadFile struct {
	FileName string                    `json:"fileName"`
	Items    []writingStylePayloadItem `json:"items"`
}

type writingStylePayloadItem struct {
	ID      string                  `json:"id"`
	Type    string                  `json:"type"`
	Content string                  `json:"content"`
	Source  writingStyleItemSources `json:"source"`
}

type writingStyleItemSources struct {
	Documents map[string]string `json:"doc,omitempty"`
	Messages  map[string]string `json:"im,omitempty"`
	Meetings  map[string]string `json:"meeting,omitempty"`
}

type writingStyleResult struct {
	MemoryKey    string                 `json:"memory_key"`
	VariantKey   string                 `json:"variant_key"`
	Status       string                 `json:"status"`
	PayloadType  string                 `json:"payload_type,omitempty"`
	Namespace    string                 `json:"namespace,omitempty"`
	Environment  string                 `json:"environment"`
	WritingStyle writingStyleDocument   `json:"writing_style"`
	References   writingStyleReferences `json:"references"`
	AIGuidance   writingStyleAIGuidance `json:"ai_guidance"`
}

type writingStyleDocument struct {
	FileName           string                `json:"file_name"`
	Title              string                `json:"title"`
	Preamble           []writingStyleRule    `json:"preamble,omitempty"`
	Sections           []writingStyleSection `json:"sections"`
	AvailableScenarios []string              `json:"available_scenarios,omitempty"`
}

type writingStyleSection struct {
	Title       string                   `json:"title"`
	Rules       []writingStyleRule       `json:"rules,omitempty"`
	Subsections []writingStyleSubsection `json:"subsections,omitempty"`
}

type writingStyleSubsection struct {
	Title string             `json:"title"`
	Rules []writingStyleRule `json:"rules,omitempty"`
}

type writingStyleRule struct {
	ID      string                  `json:"id"`
	Content string                  `json:"content"`
	Source  writingStyleItemSources `json:"source,omitempty"`
}

type writingStyleReferences struct {
	Documents []writingStyleReference `json:"documents,omitempty"`
	Messages  []writingStyleReference `json:"messages,omitempty"`
	Meetings  []writingStyleReference `json:"meetings,omitempty"`
}

type writingStyleReference struct {
	SourceID string `json:"source_id"`
	Token    string `json:"token,omitempty"`
	Title    string `json:"title"`
}

type writingStyleAIGuidance struct {
	PromptTemplate                string   `json:"prompt_template"`
	DocumentRewritePromptTemplate string   `json:"document_rewrite_prompt_template"`
	Steps                         []string `json:"steps"`
	DocumentSteps                 []string `json:"document_steps"`
	TemplateRequirements          []string `json:"template_requirements"`
	VerificationChecklist         []string `json:"verification_checklist"`
	Guardrails                    []string `json:"guardrails"`
	DocumentGuardrails            []string `json:"document_guardrails"`
}

func buildWritingStyleMemoryBody() map[string]interface{} {
	return map[string]interface{}{
		"memory_key":   writingStyleMemoryKey,
		"variant_key":  writingStyleVariantKey,
		"payload_mode": payloadModeFull,
	}
}

func resolveWritingStyleTTEnv() (string, error) {
	ttEnv := strings.TrimSpace(os.Getenv(envMemoryTTEnv))
	if ttEnv == "" {
		ttEnv = writingStyleDefaultEnv
	}
	if len(ttEnv) > 256 || strings.ContainsAny(ttEnv, "\r\n") {
		return "", errs.NewValidationError(errs.SubtypeInvalidArgument,
			"%s must be a valid single-line header value", envMemoryTTEnv)
	}
	return ttEnv, nil
}

func parseWritingStyleMemory(memory map[string]interface{}, ttEnv string) (writingStyleResult, error) {
	status := stringField(memory, "status")
	if !strings.EqualFold(status, "ready") {
		if status == "" {
			status = "unknown"
		}
		return writingStyleResult{}, errs.NewValidationError(errs.SubtypeFailedPrecondition,
			"writing-style Memory is not ready (status=%s)", status).
			WithHint("wait for the ppe_memory_schema build to reach ready, then retry")
	}

	files, err := decodeWritingStyleFiles(memory["payload"])
	if err != nil {
		return writingStyleResult{}, err
	}
	file, err := selectWritingStyleFile(files)
	if err != nil {
		return writingStyleResult{}, err
	}
	document, references, err := buildWritingStyleDocument(file)
	if err != nil {
		return writingStyleResult{}, err
	}

	memoryKey := stringField(memory, "memory_key")
	if memoryKey == "" {
		memoryKey = writingStyleMemoryKey
	}
	variantKey := stringField(memory, "variant_key")
	if variantKey == "" {
		variantKey = writingStyleVariantKey
	}
	if memoryKey != writingStyleMemoryKey || variantKey != writingStyleVariantKey {
		return writingStyleResult{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"writing-style Memory response identity mismatch: memory_key=%q variant_key=%q", memoryKey, variantKey)
	}

	return writingStyleResult{
		MemoryKey:    memoryKey,
		VariantKey:   variantKey,
		Status:       status,
		PayloadType:  stringField(memory, "payload_type"),
		Namespace:    nestedStringField(memory, "metadata", "namespace"),
		Environment:  ttEnv,
		WritingStyle: document,
		References:   references,
		AIGuidance:   defaultWritingStyleAIGuidance(),
	}, nil
}

func decodeWritingStyleFiles(payload interface{}) ([]writingStylePayloadFile, error) {
	if payload == nil {
		return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"writing-style Memory response is missing payload")
	}

	var raw []byte
	switch value := payload.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
				"writing-style Memory payload is empty")
		}
		raw = []byte(trimmed)
	default:
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
				"writing-style Memory payload cannot be encoded as JSON").WithCause(err)
		}
	}

	var envelope writingStylePayload
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"writing-style Memory payload is not valid JSON").WithCause(err)
	}
	if envelope.FileName != "" || len(envelope.Items) > 0 {
		return []writingStylePayloadFile{{FileName: envelope.FileName, Items: envelope.Items}}, nil
	}
	if len(envelope.Files) > 0 {
		return envelope.Files, nil
	}
	if envelope.Latest != nil && len(envelope.Latest.Files) > 0 {
		return envelope.Latest.Files, nil
	}

	var files []writingStylePayloadFile
	if err := json.Unmarshal(raw, &files); err == nil && len(files) > 0 {
		return files, nil
	}
	return nil, errs.NewInternalError(errs.SubtypeInvalidResponse,
		"writing-style Memory payload contains no files")
}

func selectWritingStyleFile(files []writingStylePayloadFile) (writingStylePayloadFile, error) {
	if len(files) == 1 {
		return files[0], nil
	}
	for _, file := range files {
		if file.FileName == writingStyleFileName || strings.Contains(file.FileName, "写作风格") {
			return file, nil
		}
	}
	return writingStylePayloadFile{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
		"writing-style Memory payload contains %d files but none is a writing-style file", len(files))
}

func buildWritingStyleDocument(file writingStylePayloadFile) (writingStyleDocument, writingStyleReferences, error) {
	if strings.TrimSpace(file.FileName) == "" {
		return writingStyleDocument{}, writingStyleReferences{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"writing-style Memory file is missing fileName")
	}
	if len(file.Items) == 0 {
		return writingStyleDocument{}, writingStyleReferences{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"writing-style Memory file %q has no items", file.FileName)
	}

	document := writingStyleDocument{FileName: file.FileName}
	var currentSection *writingStyleSection
	var currentSubsection *writingStyleSubsection
	for _, item := range file.Items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch item.Type {
		case "h1":
			if document.Title == "" {
				document.Title = content
			}
			currentSection = nil
			currentSubsection = nil
		case "h2":
			document.Sections = append(document.Sections, writingStyleSection{Title: content})
			currentSection = &document.Sections[len(document.Sections)-1]
			currentSubsection = nil
		case "h3":
			if currentSection == nil {
				return writingStyleDocument{}, writingStyleReferences{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
					"writing-style Memory h3 %q appears before any h2", content)
			}
			currentSection.Subsections = append(currentSection.Subsections, writingStyleSubsection{Title: content})
			currentSubsection = &currentSection.Subsections[len(currentSection.Subsections)-1]
			if currentSection.Title == "场景化写作模式" {
				document.AvailableScenarios = append(document.AvailableScenarios, content)
			}
		case "text":
			rule := writingStyleRule{ID: item.ID, Content: content, Source: item.Source}
			switch {
			case currentSubsection != nil:
				currentSubsection.Rules = append(currentSubsection.Rules, rule)
			case currentSection != nil:
				currentSection.Rules = append(currentSection.Rules, rule)
			default:
				document.Preamble = append(document.Preamble, rule)
			}
		default:
			return writingStyleDocument{}, writingStyleReferences{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
				"writing-style Memory item %q has unsupported type %q", item.ID, item.Type)
		}
	}
	if document.Title == "" {
		document.Title = strings.TrimSuffix(file.FileName, ".md")
	}
	if len(document.Sections) == 0 {
		return writingStyleDocument{}, writingStyleReferences{}, errs.NewInternalError(errs.SubtypeInvalidResponse,
			"writing-style Memory file %q has no h2 sections", file.FileName)
	}
	return document, collectWritingStyleReferences(file.Items), nil
}

func collectWritingStyleReferences(items []writingStylePayloadItem) writingStyleReferences {
	return writingStyleReferences{
		Documents: collectWritingStyleReferenceType(items, func(source writingStyleItemSources) map[string]string { return source.Documents }, true),
		Messages:  collectWritingStyleReferenceType(items, func(source writingStyleItemSources) map[string]string { return source.Messages }, false),
		Meetings:  collectWritingStyleReferenceType(items, func(source writingStyleItemSources) map[string]string { return source.Meetings }, false),
	}
}

func collectWritingStyleReferenceType(
	items []writingStylePayloadItem,
	selectSources func(writingStyleItemSources) map[string]string,
	includeToken bool,
) []writingStyleReference {
	byID := make(map[string]writingStyleReference)
	for _, item := range items {
		for sourceID, title := range selectSources(item.Source) {
			ref := writingStyleReference{SourceID: sourceID, Title: title}
			if includeToken {
				ref.Token = writingStyleDocumentToken(sourceID)
			}
			byID[sourceID] = ref
		}
	}
	ids := make([]string, 0, len(byID))
	for sourceID := range byID {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	refs := make([]writingStyleReference, 0, len(ids))
	for _, sourceID := range ids {
		refs = append(refs, byID[sourceID])
	}
	return refs
}

func writingStyleDocumentToken(sourceID string) string {
	parts := strings.SplitN(sourceID, "-", 4)
	if len(parts) == 4 && len(parts[0]) == 4 && len(parts[1]) == 2 && len(parts[2]) == 2 {
		return parts[3]
	}
	return sourceID
}

func defaultWritingStyleAIGuidance() writingStyleAIGuidance {
	return writingStyleAIGuidance{
		PromptTemplate: `参考我的写作风格 Memory，完成以下请求：{query}

先按请求查找必要的事实和素材，再判断输出类型与写作场景。将 Memory 用于信息组织、格式、措辞和语气；不要把 Memory 当作该请求的事实来源。若采用 Memory 中相关参考文档的格式，必须实际读取其完整代表性区块并确认真实 block 结构，不能只依据 Memory 摘要推断。若请求要求创建或修改文档，应将写作风格实际应用到文档，并按请求执行创建或原位修改，完成后回读。若没有要求文档操作，按指定形式直接回答。`,
		DocumentRewritePromptTemplate: `参考我的写作风格 memory，将 {文档链接} 改写成我的风格，并重新创建一个飞书云文档。

先判断原文的文档类型和使用场景，再从 memory 中选择最相关的同类型参考文档作为唯一格式模板。若 memory 提供了参考文档，必须实际读取其完整代表性区块，确认真实 block 结构，不得仅依据 memory 摘要推断格式。

严格复刻参考文档的格式骨架，包括标题层级、板块顺序、表格及列结构、列表层级、标签、checkbox、状态标记和内容归位方式；只按 memory 调整措辞和表达风格，不得改变模板结构。无依据内容留空，不得推测。创建后回读并逐项对照验证，修正所有结构偏差和旧结构残留后再交付。

{写作风格 memory}`,
		Steps: []string{
			"将用户在 Skill 名称后输入的内容作为原始请求，先确定所需事实来源、输出形式和写作场景。",
			"从适合该请求的实时或历史资料获取事实；Memory 只提供写作风格，不用于证明工作进展等事实。",
			"将“跨场景稳定特征”用于内容组织与表达，并选择匹配的场景化模式；无匹配项时不要强套模板。",
			"按请求指定的形式交付；若请求创建或修改文档，执行 document_steps 并将风格用于实际文档；否则直接回答。",
		},
		DocumentSteps: []string{
			"先判断用户要求新建文档还是修改现有文档；修改时读取目标文档与现有结构，新建时取得内容事实和素材。",
			"按文档类型选择匹配的场景化风格；若 Memory 提供相关的同类型参考文档，必须选最相关的一份并实际读取其完整代表性区块，确认真实 block 结构。采用格式时只能用这一份作为模板；用户明确指定的模板优先。",
			"将风格用于文档的信息组织、格式、措辞和语气；用户指定的格式和现有文档需保留的结构优先。无依据内容留空。",
			"按用户要求创建新文档或修改原文，不擅自把原位修改改成另建文档。",
			"操作后回读文档，检查事实、风格、结构和旧结构残留；修正偏差后再交付。",
		},
		TemplateRequirements: []string{
			"标题层级",
			"板块顺序",
			"表格及列结构",
			"列表层级",
			"标签",
			"checkbox",
			"状态标记",
			"内容归位方式",
		},
		VerificationChecklist: []string{
			"执行的是用户要求的新建或原位修改操作。",
			"使用参考格式时模板只有一个，且属于同类型、同场景；已读取完整代表性区块并确认真实 block 结构。",
			"标题、板块、表格列、列表层级、标签、checkbox、状态标记和内容位置符合用户要求及选定模板。",
			"写作风格已实际应用到文档的信息组织和表达，未覆盖用户指定或原文需保留的结构。",
			"已修正结构偏差；改写时已清除旧结构残留。",
			"无依据内容是否保持为空，且没有推测或补写业务事实。",
		},
		Guardrails: []string{
			"Memory 只代表历史写作偏好，不能替代用户本次提供的事实、最新文档或明确指令。",
			"不要把其他场景或单篇文档的局部格式当成跨场景稳定偏好。",
			"不得依据写作风格补写不存在的业务事实；缺少依据时说明缺口或向用户确认。",
			"不要因为使用了写作风格 Skill 就擅自新建文档或改变用户要求的输出形式。",
		},
		DocumentGuardrails: []string{
			"使用参考格式时只能选择一份最相关的同类型文档，不得混合多份模板结构。",
			"不得仅根据 Memory 摘要推断参考文档格式，必须以实际读取到的 block 结构为准。",
			"修改现有文档时保留用户未要求更改的内容和结构。",
			"引用参考文档时保留来源可追溯性，不要把来源正文整段复制为当前内容。",
		},
	}
}

func printWritingStylePretty(w io.Writer, result writingStyleResult) {
	fmt.Fprintf(w, "%s\n\n", result.WritingStyle.Title)
	for _, rule := range result.WritingStyle.Preamble {
		fmt.Fprintf(w, "- %s\n", rule.Content)
	}
	for _, section := range result.WritingStyle.Sections {
		fmt.Fprintf(w, "## %s\n", section.Title)
		for _, rule := range section.Rules {
			fmt.Fprintf(w, "- %s\n", rule.Content)
		}
		for _, subsection := range section.Subsections {
			fmt.Fprintf(w, "### %s\n", subsection.Title)
			for _, rule := range subsection.Rules {
				fmt.Fprintf(w, "- %s\n", rule.Content)
			}
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "## 任意请求的 AI 使用方法")
	for index, step := range result.AIGuidance.Steps {
		fmt.Fprintf(w, "%d. %s\n", index+1, step)
	}
	fmt.Fprintln(w, "\n通用约束：")
	for _, guardrail := range result.AIGuidance.Guardrails {
		fmt.Fprintf(w, "- %s\n", guardrail)
	}
	fmt.Fprintln(w, "\n## 创建或修改文档时")
	for index, step := range result.AIGuidance.DocumentSteps {
		fmt.Fprintf(w, "%d. %s\n", index+1, step)
	}
	fmt.Fprintln(w, "\n格式骨架检查项：")
	for _, requirement := range result.AIGuidance.TemplateRequirements {
		fmt.Fprintf(w, "- %s\n", requirement)
	}
	fmt.Fprintln(w, "\n交付前验证：")
	for _, item := range result.AIGuidance.VerificationChecklist {
		fmt.Fprintf(w, "- %s\n", item)
	}
	fmt.Fprintln(w, "\n文档操作约束：")
	for _, guardrail := range result.AIGuidance.DocumentGuardrails {
		fmt.Fprintf(w, "- %s\n", guardrail)
	}
}

func stringField(data map[string]interface{}, key string) string {
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}

func nestedStringField(data map[string]interface{}, objectKey, fieldKey string) string {
	nested, _ := data[objectKey].(map[string]interface{})
	if nested == nil {
		return ""
	}
	value, _ := nested[fieldKey].(string)
	return strings.TrimSpace(value)
}
