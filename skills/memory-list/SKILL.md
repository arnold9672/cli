---
name: memory-list
version: 0.1.0
description: "List Memory Hub entries visible to the current user. Use for Memory inventory and variant discovery, not for reading payload content or querying Graph nodes and edges."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +list --help"
---

# Memory List

Use the current user identity and run:

```bash
lark-memory-cli memory +list --as user --format json
```

Summarize only the available entries and their `memory_key`, name, variants, default variant, status,
description, and showcase. Do not fetch a payload unless the user also asks to read one entry; use
`memory-get` for that follow-up. Do not substitute this command for GraphQuery, OneHop, or Graph Search.

If login or `memory:hub` authorization is missing, follow `../lark-shared/SKILL.md`. Do not switch to bot
identity or expose unrelated Memory content.
