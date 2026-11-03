---
name: memory-graph-one-hop
version: 0.4.0
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
  --as user \
  --format json
```

Use `expanded_from` to retain the direct upstream Root, node, and edge. Parse both node and edge Detail;
topology or relation type alone is not evidence. Respect `expandable=false`, filtered USER counts, and any
typed error. Do not automatically issue another OneHop call unless the user explicitly requests continued
traversal.

The standalone command sends no lane header by default. Only an explicitly set, nonblank
`LARKSUITE_CLI_MEMORY_TT_ENV` adds `x-tt-env`. Do not invent a lane; when continuing a Graph Search
traversal, unset that variable for the OneHop process so it does not inherit the prerequisite FaaS lane.

Read node and edge bodies only from `detail.markdown`. `detail.source[]` is provenance, not body
evidence; keep `entity_id` and `entity_type` as strings, distinct from RootID and SourceID.
Distinguish missing Detail, source-only Detail, and a nonempty Markdown body. Do not fall back to old
fields or endpoints. `meta.detail_bytes` counts UTF-8 Markdown bytes, excluding provenance metadata.
Output `--format` controls the CLI envelope, not the Detail contract. There are no detail-format or
backend-version flags; requests preserve the server's data-version policy.

For login or `memory:hub` authorization failures, follow `../lark-shared/SKILL.md`.
