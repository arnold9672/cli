---
name: memory-graph-range
version: 0.1.0
description: "Query the current user's Memory Graph nodes and edges for an explicit time range of up to seven days. Use for time-window Graph history, not stable-root traversal or natural-language knowledge search."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory +graph-range --help"
---

# Memory Graph Range

Resolve the requested range to Unix seconds as the half-open interval `[start_time_sec, end_time_sec)`.
The range must be positive and no longer than seven days. If the user supplies no usable range, ask for
one rather than silently choosing a different period.

Run:

```bash
lark-memory-cli memory +graph-range \
  --start-time-sec '<start_unix_seconds>' \
  --end-time-sec '<end_unix_seconds>' \
  --detail-format markdown \
  --as user \
  --format ndjson
```

The CLI splits the range into windows of at most 24 hours, executes them concurrently, and emits complete
lines in chronological order. Always check the process exit status: emitted lines before a failure are only
the valid continuous prefix. Parse both node and edge Detail, filter conclusions to the user's request, and
label partial output explicitly. Never accept or infer another user's UID or OpenID.

For login or `memory:hub` authorization failures, follow `../lark-shared/SKILL.md`.
