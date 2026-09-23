---
name: memory-writing-style
description: "Read the current user's writing-style Memory and apply it to a writing or rewriting task. Use when the user asks to write in their own style; do not use for general Memory inventory or factual retrieval."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +writing-style --help"
---

# Memory Writing Style

Use the current user identity and run:

```bash
lark-memory-cli memory +writing-style --as user --format json
```

The command reads `personal_memory_snapshot` variant `agentic_v1_writing_pattn_v1` from
`ppe_memory_schema` through the LF IDC by default, parses `写作风格.md`, and returns `writing_style`,
`references`, and `ai_guidance`. An explicit `LARKSUITE_CLI_MEMORY_TT_ENV` overrides the lane for the current
process; it does not change `destination-idc=lf`. Do not change the lane unless the user asks.

## Apply the result

1. Check the success envelope and require `data.status == "ready"`.
2. Read the source document and determine its document type and writing scenario.
3. Apply the `跨场景稳定特征` section as global style guidance.
4. Under `场景化写作模式`, choose only the closest matching scenario from
   `writing_style.available_scenarios`. If none matches, do not force a scenario template.
5. Choose the most relevant same-type document from the selected rules' `source.doc` as the **only** format
   template. Resolve its token through `references.documents`.
6. Actually read complete representative blocks from that template with structure metadata sufficient to inspect
   its real block hierarchy. Never infer the template format from the Memory summary alone.
7. Create a new Lark document. Strictly reproduce the template's title levels, section order, tables and column
   structure, list nesting, tags, checkboxes, status markers, and content placement. Use Memory only to adjust
   wording and expression style; it must not change the template structure.
8. Leave unsupported content empty. Do not infer or invent facts.
9. Read the created document back and compare every item against `ai_guidance.template_requirements` and
   `ai_guidance.verification_checklist`. Fix every structural deviation and every remnant of the source
   document's old structure before delivery.

Use Memory for organization, structure, wording, and tone. Current user instructions and supplied facts remain
authoritative. Do not invent facts, mix multiple template structures, silently mix unrelated scenario patterns,
or expose unrelated Memory content. For document rewrites, treat `ai_guidance.prompt_template` as the canonical
prompt contract.

If login or `memory:hub` authorization is missing, follow `../lark-shared/SKILL.md`; do not switch to bot
identity. If the PPE Memory is not ready or the API returns a structured failure, report its status and `log_id`
instead of falling back to another variant.
