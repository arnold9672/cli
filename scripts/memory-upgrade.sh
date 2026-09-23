#!/usr/bin/env bash
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT
set -euo pipefail

SOURCE_DIR="${LARK_CLI_MEMORY_DIR:-$HOME/.lark-cli-memory}"
PREFIX="${LARK_CLI_PREFIX:-$HOME/.local}"
MEMORY_CLI="${LARK_CLI_MEMORY_UPDATE_BIN:-$PREFIX/bin/lark-memory-cli}"
CONTROL="$SOURCE_DIR/bin/memoryctl"
MIN_GO_MAJOR=1
MIN_GO_MINOR=23

go_supported() {
  local candidate="$1"
  [[ -x "$candidate" ]] || return 1
  local output version major minor
  output="$($candidate version 2>/dev/null)" || return 1
  version="$(printf '%s\n' "$output" | awk 'match($0, /go[0-9]+\.[0-9]+/) { value = substr($0, RSTART + 2, RLENGTH - 2); split(value, parts, "."); print parts[1], parts[2]; exit }')"
  [[ -n "$version" ]] || return 1
  read -r major minor <<< "$version"
  ((major > MIN_GO_MAJOR || (major == MIN_GO_MAJOR && minor >= MIN_GO_MINOR)))
}

find_go() {
  local candidates=()
  [[ -z "${GO_BIN:-}" ]] || candidates+=("$GO_BIN")
  if [[ "${LARK_CLI_MEMORY_GO_CANDIDATES_ONLY:-0}" != "1" ]]; then
    candidates+=(
      /opt/homebrew/opt/go/libexec/bin/go
      /usr/local/opt/go/libexec/bin/go
      /usr/local/go/bin/go
      /usr/local/bytesuite-box/pkg/go/1.24.1/bin/go
      /opt/homebrew/Cellar/go/*/libexec/bin/go
      /usr/local/Cellar/go/*/libexec/bin/go
      /opt/homebrew/bin/go
      /usr/local/bin/go
    )
    local path_go
    path_go="$(command -v go 2>/dev/null || true)"
    [[ -z "$path_go" ]] || candidates+=("$path_go")
  fi

  local candidate
  for candidate in "${candidates[@]}"; do
    if go_supported "$candidate"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  printf 'memory-upgrade: no usable Go %d.%d+ toolchain found; set GO_BIN to an absolute Go binary path\n' \
    "$MIN_GO_MAJOR" "$MIN_GO_MINOR" >&2
  return 1
}

if [[ "${1:-}" == "--print-go" ]]; then
  find_go
  exit 0
fi

[[ -x "$MEMORY_CLI" ]] || {
  printf 'memory-upgrade: lark-memory-cli not found or not executable: %s\n' "$MEMORY_CLI" >&2
  exit 1
}

go_bin="$(find_go)"
printf 'memory-upgrade: using %s\n' "$go_bin" >&2
GO_BIN="$go_bin" "$MEMORY_CLI" --update

[[ -x "$CONTROL" ]] || {
  printf 'memory-upgrade: updated memoryctl not found or not executable: %s\n' "$CONTROL" >&2
  exit 1
}
LARK_CLI_MEMORY_DIR="$SOURCE_DIR" LARK_CLI_PREFIX="$PREFIX" "$CONTROL" refresh
printf 'memory-upgrade: complete; restart Codex/Agent sessions to reload skills\n' >&2
