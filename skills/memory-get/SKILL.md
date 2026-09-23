---
name: memory-get
version: 0.1.0
description: "Read one Memory Hub entry by memory_key for the current user. Use when a specific Memory payload, summary, or metadata record is needed; do not use for inventory or Graph queries."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +get --help"
---

# Memory Get

Require a reliable `memory_key`; never guess one. If it is missing, use `memory-list` to discover the
available keys first. When variants are available, prefer `agentic_v1`, then `default_variant_key`, then
the service default.

Run one of:

```bash
lark-memory-cli memory +get --memory-key '<memory_key>' --as user --format json
lark-memory-cli memory +get --memory-key '<memory_key>' --variant-key '<variant_key>' --as user --format json
lark-memory-cli memory +get --memory-key '<memory_key>' --payload-mode summary --as user --format json
lark-memory-cli memory +get --memory-key '<memory_key>' --payload-mode metadata --as user --format json
```

The default payload mode is `full`. Use the smallest mode that answers the request, and disclose only the
relevant fields. Treat a result whose status is not `ready` as incomplete. For login or `memory:hub`
authorization failures, follow `../lark-shared/SKILL.md`; do not switch to bot identity.
