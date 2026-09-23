---
name: memory-graph-one-hop
version: 0.1.0
description: "Query one-hop Memory Graph relations from known stable NodeType and RootID values. Use only when reliable roots already exist; do not guess roots or automatically continue multiple hops."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +graph-one-hop --help"
---

# Memory Graph OneHop

Require one or more reliable roots in `<node_type>:<root_id>` form. Supported roots are IM_DAY (`1`),
DOC_DAY (`2`), and MEETING (`3`). USER (`4`) is forbidden, and SourceID must never be substituted for a
missing RootID. Ask for the missing values when the user has not provided or established them.

Run exactly one OneHop request:

```bash
lark-memory-cli memory +graph-one-hop \
  --root '<node_type>:<root_id>' \
  --lookback-days 7 \
  --hop '<agent_hop_1_to_10>' \
  --detail-format markdown \
  --as user \
  --format json
```

Use `expanded_from` to retain the direct upstream Root, node, and edge. Parse both node and edge Detail;
topology or relation type alone is not evidence. Respect `expandable=false`, filtered USER counts, and any
typed error. Do not automatically issue another OneHop call unless the user explicitly requests continued
traversal.

For login or `memory:hub` authorization failures, follow `../lark-shared/SKILL.md`.
