---
name: graph-search
version: 0.8.0
description: "Compatibility alias for the former Graph Search selector. Use only when the user explicitly invokes graph-search; prefer memory-graph-search for new requests."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +graph-search --help"
---

# Graph Search Compatibility Alias

Read `../memory-graph-search/SKILL.md` and follow that canonical workflow without changing the user's query.
This alias exists only for older installations and saved prompts; new selector usage should use
`memory-graph-search`.
