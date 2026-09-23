---
name: memory-writing-style
description: "Apply the current user's writing-style Memory to the query following $memory-writing-style, including summaries and requests to create or modify documents. Memory guides expression, not factual retrieval."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +writing-style --help"
---

# Memory Writing Style

Treat everything after `$memory-writing-style` as the user's request. It can be any writing task, not just a
document rewrite. For example, `$memory-writing-style 帮忙总结下过去两个月的工作` asks for a time-bounded work
summary in the user's style; it does not ask for a new Lark document.

Use the current user identity and run:

```bash
lark-memory-cli memory +writing-style --as user --format json
```

The command reads `personal_memory_snapshot` variant `agentic_v1_writing_pattn_v1` from
`ppe_memory_schema` through the LF IDC by default, parses `写作风格.md`, and returns `writing_style`,
`references`, and `ai_guidance`. An explicit `LARKSUITE_CLI_MEMORY_TT_ENV` overrides the lane for the current
process; it does not change `destination-idc=lf`. Do not change the lane unless the user asks.

## Apply the result to the request

1. Check the success envelope and require `data.status == "ready"`.
2. Fulfill the user's query using appropriate factual sources. For a time range such as "过去两个月", retrieve work
   records covering that period; writing-style Memory is not evidence of what happened during it. Explain any
   material evidence gap rather than inventing activity.
3. Apply `跨场景稳定特征` to the organization and wording. Select a matching entry from `场景化写作模式` when one
   exists; otherwise use the stable rules without forcing a scenario template. Before adopting a referenced
   document's format for that scenario, read its complete representative blocks and inspect the real structure.
4. Deliver in the form the user requested. Answer in chat unless the request asks to create or modify a document.
   Follow `ai_guidance.steps` and `ai_guidance.guardrails` for every query.

## When the query asks to create or modify a document

Use `ai_guidance.document_steps`, `template_requirements`, `verification_checklist`, and
`document_guardrails` together with the steps below.

1. Determine whether the user asked for a new document or an edit to an existing one. For an edit, read the
   target and preserve unrelated content and structure. For a new document, gather the requested factual material.
2. Use the stable and matching scenario rules to shape the document. If Memory provides relevant same-type
   reference documents, select the most relevant one from `source.doc`, resolve its token through
   `references.documents`, and **read complete representative blocks** to confirm the real block hierarchy.
   Use it as the sole format template when the user has not specified a different template. Never infer format
   from the Memory summary alone. If none fits, proceed with the user's requested structure and applicable rules.
3. Apply the writing style to the actual document's organization, format, wording, and tone. Honor any explicit
   user template or existing structure that should remain. Leave unsupported facts empty.
4. Create or edit exactly as requested, then read the result back and correct unsupported facts, style mismatches,
   structural deviations, and remnants of the old structure when rewriting.

Current user instructions and supplied facts remain authoritative. Do not invent facts, mix unrelated scenario
patterns, or expose unrelated Memory content. `ai_guidance.prompt_template` applies to any query;
`document_rewrite_prompt_template` covers only an explicit request to rewrite an existing document into a new one.

If login or `memory:hub` authorization is missing, follow `../lark-shared/SKILL.md`; do not switch to bot
identity. If the PPE Memory is not ready or the API returns a structured failure, report its status and `log_id`
instead of falling back to another variant.
