#!/usr/bin/env bash
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT
set -euo pipefail

fail() {
  printf 'memory-upgrade.test: %s\n' "$*" >&2
  exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/memory-upgrade-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

source_dir="$tmp/source"
prefix="$tmp/prefix"
log="$tmp/calls.log"
mkdir -p "$source_dir/bin" "$prefix/bin" "$tmp/go/bin"

cat > "$tmp/go/bin/go" <<'EOF'
#!/bin/sh
echo 'go version go1.24.1 darwin/arm64'
EOF
chmod +x "$tmp/go/bin/go"

printed_go="$(HOME="$tmp/home" GO_BIN="$tmp/go/bin/go" LARK_CLI_MEMORY_GO_CANDIDATES_ONLY=1 \
  /bin/bash "$repo_root/scripts/memory-upgrade.sh" --print-go)"
[[ "$printed_go" == "$tmp/go/bin/go" ]] || fail "--print-go returned $printed_go"

cat > "$prefix/bin/lark-memory-cli" <<EOF
#!/bin/sh
printf 'update GO_BIN=%s args=%s\n' "\$GO_BIN" "\$*" >> "$log"
EOF
chmod +x "$prefix/bin/lark-memory-cli"

cat > "$source_dir/bin/memoryctl" <<EOF
#!/bin/sh
printf 'memoryctl args=%s source=%s prefix=%s\n' "\$*" "\$LARK_CLI_MEMORY_DIR" "\$LARK_CLI_PREFIX" >> "$log"
printf '{"ok":true,"status":"enabled"}\n'
EOF
chmod +x "$source_dir/bin/memoryctl"

HOME="$tmp/home" \
GO_BIN="$tmp/go/bin/go" \
LARK_CLI_MEMORY_DIR="$source_dir" \
LARK_CLI_PREFIX="$prefix" \
/bin/bash "$repo_root/scripts/memory-upgrade.sh" >/dev/null

grep -Fq "update GO_BIN=$tmp/go/bin/go args=--update" "$log" || fail "updater was not called with selected GO_BIN"
grep -Fq "memoryctl args=refresh source=$source_dir prefix=$prefix" "$log" || fail "memoryctl refresh was not called"

cat > "$tmp/go/bin/go-old" <<'EOF'
#!/bin/sh
echo 'go version go1.22.9 darwin/arm64'
EOF
chmod +x "$tmp/go/bin/go-old"
if HOME="$tmp/home" GO_BIN="$tmp/go/bin/go-old" PATH="/usr/bin:/bin" LARK_CLI_MEMORY_GO_CANDIDATES_ONLY=1 \
  LARK_CLI_MEMORY_DIR="$source_dir" LARK_CLI_PREFIX="$prefix" \
  /bin/bash "$repo_root/scripts/memory-upgrade.sh" >/dev/null 2>&1; then
  fail "upgrade accepted Go older than 1.23"
fi
