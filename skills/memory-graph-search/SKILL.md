---
name: memory-graph-search
version: 0.2.0
description: "Search ByteDance-intranet knowledge and produce evidence-grounded answers from permission-filtered candidates, seeded OneHop traversal, and optional time-window GraphQuery node and edge Detail. Use for natural-language knowledge and multi-hop Graph search, not direct time-window or stable-root Graph commands."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +graph-search --help"
---

# Graph Search

Use this skill when the user selects `memory-graph-search` in a supported AI tool or provides a natural-language
query after `memory graph search`. Everything after the invocation is the query; do not require the user to
know CLI flags.

This command is available only on the ByteDance intranet.

## Run

1. Use the current `lark-memory-cli` user login. Never ask the user for an internal UID or accept one in the
   query, flags, or environment.
2. Run:

```bash
lark-memory-cli memory +graph-search \
  --query '<user query>' \
  --concurrency 8 \
  --detail-format markdown \
  --graph-query-mode on \
  --graph-query-lookback-days 7 \
  --as user \
  --format json
```

Before any Knowledge QA, GraphQuery, Wiki, or OneHop request, the command strictly resolves the UAT, verifies it
with `/open-apis/authen/v1/user_info`, and obtains the current user's `open_id`. It then converts that OpenID to
the internal UID through the intranet identity service and uses the two identities consistently: the converted
UID for Knowledge QA and the UAT-derived OpenID for Graph. There is no local UID override. Do not retry business
requests after an authentication or identity conversion failure; follow the authorization recovery below.

The identity conversion, Knowledge QA, OneHop, and supplemental GraphQuery requests default to `ppe_memory_hub`. An explicit
`LARKSUITE_CLI_MEMORY_TT_ENV` overrides that default for the current process; do not change the lane unless
the user asks.

`--graph-query-mode on` makes every Graph Search query attempt a time-window Graph lookup during the pilot.
Queries with an explicit supported time intent use that exact range; other queries use the configured rolling
lookback. Use `off` only when the user asks to avoid GraphQuery. Unsupported explicit ranges over seven days
skip the supplement and expose `trigger_reason=time_range_unsupported`; they are never silently truncated.

Knowledge QA supplies the initial semantic roots, but it is not the only root source. GraphQuery runs in parallel
and returns every verified `NodeType + RootID` as `data.next_roots`; explicit reliable roots already present in
the conversation may be queried with the atomic `memory +graph-one-hop` command. GraphQuery roots are never
auto-expanded. A shared limiter enforces `--concurrency` across every Graph Search network request.

Graph Search always performs exactly the initial OneHop and has no hop-count flag. After that response, the
model must inspect node Detail, edge Detail, endpoints, time, and provenance, then decide whether any
`next_roots` deserve another atomic OneHop call. Continue only for a clearly unresolved Query aspect with new
relevant evidence. Ten hops is the absolute safety limit.

## Evidence workflow

Treat an existing Knowledge QA answer as the answer baseline and Graph as optional incremental evidence. If the
caller or current conversation already contains that answer, preserve it together with its citations. If no
answer exists, first form a candidate-only baseline from `data.search_candidates`; the CLI itself returns
passages and Graph evidence, not a generated Knowledge QA answer.

1. Confirm `data.evidence.candidate_source == "knowledge_qa.passages"` and
   `data.evidence.permission_filtered == true`. Use only `data.search_candidates`; never use
   `passages_ignore_filter` or other unfiltered diagnostics. This identifies the Knowledge QA candidate source,
   not the complete root set.
2. Read relevant candidate `content` first, especially for queries about recent status. Candidate content is
   allowed evidence but must remain traceable to its `passage_id`, URL, and timestamps.
3. Validate every seed through `roots[].search_origins`: `node_type_source`, `root_id_source`, and
   `anchor_time_source` must explain how NodeType, RootID, and graph date were obtained. Never substitute
   `source_id` for a missing RootID.
4. Parse both sides of the graph evidence:
   - Use `data.evidence.node_details[].detail_path` to locate and read relevant `nodes[].detail`.
   - Use `data.evidence.edge_details[].detail_path` to locate and read relevant `edges[].detail`.
   - Do not infer relationship meaning from `relation_type`, topology, or edge count alone.
5. Use `data.evidence.edge_groups` to organize structurally repeated edges without discarding their raw Detail.
   A group with many `im_reply` self-loops is one conversation cluster, not that many independent facts; inspect
   the member edge Detail to determine what the replies actually say.
6. Inspect `data.graph_query`:
   - GraphQuery runs in parallel with Knowledge QA and uses the same lane.
   - OneHop may execute while GraphQuery is still running, but final evidence merging waits for both branches.
   - Check `range_source`, `start_time_sec`, and `end_time_sec` before using time-window evidence.
   - Its nodes and edges are merged by NodeID/EdgeID only after the initial OneHop finishes.
   - Verified GraphQuery roots appear in `data.next_roots` with
     `search_origins[].candidate_source == "graph_query.nodes"`; they never enter OneHop without a model decision.
   - Use `retrieved_via` and `data.evidence.*[].retrieval_class` to distinguish `one_hop_only`,
     `graph_query_only`, and structurally `corroborated` objects.
   - GraphQuery-only evidence still requires Query relevance checks over both node and edge Detail; appearing
     in the time window is not sufficient.
7. Compare the node and edge Detail with the Query and the answer baseline. Keep Graph evidence only when it
   adds a relevant fact, supplies a causal or relationship explanation, corroborates a material claim, resolves
   a conflict, or provides a newer update. Otherwise ignore it.
8. Use `retrieval_class` and provenance to support the comparison, but do not require an explicit Query
   checklist, missing-aspect state machine, or user-visible evidence ledger. For “recent” queries, prefer the
   newest supported fact and label older Graph context.

The CLI's evidence counts and per-hop `new_*` fields prove structural novelty only. They do not prove semantic
value; the model must judge value from the Query together with the node Detail, edge Detail, endpoints, time,
and provenance.

## Baseline-preserving synthesis

The final synthesis input is the original Query, the complete answer baseline and its citations, plus every
returned node and edge Detail, traversal path, timestamp, and provenance record. Do not replace the baseline
with a fresh Graph-centric answer.

Use one simple decision: does Graph add or correct information that materially helps answer the Query?

- If no, return the baseline answer without adding Graph commentary.
- If yes, patch the useful facts into the baseline while preserving its relevant names, numbers, dates, task
  lists, status, and conclusions.
- Replace a baseline claim only when newer or more authoritative Graph evidence clearly corrects it. Explain an
  unresolved conflict instead of silently choosing one side.
- Do not treat extra length, repeated evidence, topology, or edge count as answer improvement.
- Do not mention Graph, OneHop, retrieval classes, or traversal mechanics in the user-facing answer unless the
  user explicitly asks about the retrieval process.

This is a fallback contract: Graph may improve the answer or leave it unchanged, but must not remove useful
baseline facts without a supported correction.

## Agent-controlled continuation

After the initial Graph Search command:

1. Compare the current node and edge Detail with the Query and the answer baseline.
2. Continue only when the current Graph evidence is clearly relevant, the answer is still materially incomplete,
   and a `data.next_roots` candidate is likely to supply the missing information.
3. Select only reliable, expandable roots. Do not select USER, `expandable=false`, missing RootID, repeated, or
   clearly irrelevant roots. If the three continuation conditions are not all true, stop.
4. Call exactly one additional hop with the same trace:

```bash
lark-memory-cli memory +graph-one-hop \
  --root '<node_type>:<root_id>' \
  --lookback-days '<days covering the earliest selected anchor_date through now>' \
  --hop '<2_to_10>' \
  --trace-id '<meta.trace_id>' \
  --detail-format markdown \
  --as user \
  --format json
```

Use the selected `next_roots[].anchor_dates` and the Query's time intent to compute the continuation lookback;
do not blindly use seven days for historical roots.

5. Reapply the same three-condition test after every call. Stop when the answer is complete, no relevant new
evidence appears, no valid root is likely to help, only repeated evidence remains, or hop 10 is reached. Record
a concise selection or stop reason; do not expose hidden chain-of-thought.

## Pilot upper-bound rule

During the initial internal trial, do not use hard truncation or silent Detail pruning.
Keep all permission-filtered candidate content and all returned node and edge Detail available for analysis so
the trial can measure the maximum useful contribution of Graph evidence. `data.evidence` and `edge_groups` are
for provenance, organization, and duplicate-aware reasoning; they never replace or delete the raw `nodes` and
`edges` payloads. Choosing which root to expand schedules the next network call; it must not delete the
unselected evidence. If the AI tool cannot process the complete result because of a real context or output limit,
report the limitation and mark the answer incomplete instead of silently selecting a subset.

## Result handling

- Check `meta.complete`. When it is `false`, return the successful results and clearly identify the failed
  OneHop batches from `data.failed_batches` and GraphQuery windows from `data.graph_query.failed_windows` as
  incomplete branches.
- If Knowledge QA returns no usable Doc, Wiki, or Message roots, still inspect GraphQuery evidence and
  `data.next_roots`. Explain that no initial OneHop ran; continue only when a non-QA root is relevant and valid.
- Minutes appear only in `skipped_candidates`; do not present them as traversed Graph results.
- If every OneHop batch fails, report the structured error and its `log_id` when present.
- Supplemental GraphQuery failure must not discard a successful Knowledge QA + OneHop result. Return the
  primary evidence, mark the answer incomplete, and do not invent GraphQuery-only conclusions.
- Do not use a node or edge whose evidence index reports `detail_present=false` to assert content that is not
  available elsewhere.
- Every material answer claim must link to a candidate URL or carry a traceable Graph path from an initial
  `search_origin` through the supporting edge and node. Surface conflicts instead of silently choosing one.
- Do not reveal the UID, OpenID, UAT, internal RPC headers, or raw unrelated passages.

## Missing setup

- For login or missing scopes, read [`../lark-shared/SKILL.md`](../lark-shared/SKILL.md) and follow its
  user-auth recovery guidance. Graph Search uses user identity and currently requires `memory:hub`; Wiki
  resolution may additionally require `wiki:node:retrieve`.
