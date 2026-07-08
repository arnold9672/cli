#!/usr/bin/env bash
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT
set -euo pipefail

fail() {
  echo "memoryctl.test: $*" >&2
  exit 1
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  case "$haystack" in
    *"$needle"*) ;;
    *) fail "expected output to contain $needle, got: $haystack" ;;
  esac
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/memoryctl-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

source_dir="$tmp/source"
prefix="$tmp/prefix"
home="$tmp/home"
app_dir="$prefix/libexec/lark-memory-cli"

mkdir -p "$source_dir/skills/lark-memory" "$source_dir/skills/lark-shared" "$app_dir" "$home"
printf '%s\n' '#!/usr/bin/env bash' 'echo "fake lark-memory-cli"' > "$app_dir/lark-cli"
chmod +x "$app_dir/lark-cli"
printf '%s\n' 'memory skill v1' > "$source_dir/skills/lark-memory/SKILL.md"
printf '%s\n' 'shared skill v1' > "$source_dir/skills/lark-shared/SKILL.md"

run_memoryctl() {
  HOME="$home" \
  LARK_CLI_MEMORY_DIR="$source_dir" \
  LARK_CLI_PREFIX="$prefix" \
  PATH="$prefix/bin:$PATH" \
  bash "$repo_root/scripts/memoryctl.sh" "$@"
}

status="$(run_memoryctl status --json)"
assert_contains "$status" '"status": "disabled"'

status="$(run_memoryctl enable --json)"
assert_contains "$status" '"status": "enabled"'
test -x "$prefix/bin/lark-memory-cli" || fail "active wrapper was not created"
grep -Fq 'open.feishu-pre.cn' "$prefix/bin/lark-memory-cli" || fail "wrapper does not set pre OpenAPI domain"
test -f "$home/.agents/skills/lark-memory/SKILL.md" || fail "agents memory skill was not enabled"
test -f "$home/.codex/skills/lark-memory/SKILL.md" || fail "codex memory skill was not enabled"
test -f "$home/.agents/skills/lark-shared/SKILL.md" || fail "agents shared skill was not installed"
test -f "$home/.codex/skills/lark-shared/SKILL.md" || fail "codex shared skill was not installed"

printf '%s\n' 'memory skill v2' > "$source_dir/skills/lark-memory/SKILL.md"
status="$(run_memoryctl enable --json)"
assert_contains "$status" '"status": "enabled"'
grep -Fq 'memory skill v2' "$home/.agents/skills/lark-memory/SKILL.md" || fail "enable did not refresh active agents memory skill"
grep -Fq 'memory skill v2' "$home/.codex/skills/lark-memory/SKILL.md" || fail "enable did not refresh active codex memory skill"

status="$(run_memoryctl disable --json)"
assert_contains "$status" '"status": "disabled"'
test ! -e "$prefix/bin/lark-memory-cli" || fail "active wrapper still exists after disable"
test -x "$prefix/bin/.disabled/lark-memory-cli" || fail "disabled wrapper was not retained"
test ! -e "$home/.agents/skills/lark-memory" || fail "agents memory skill still active after disable"
test -f "$home/.agents/skills/.disabled/lark-memory/SKILL.md" || fail "agents memory skill was not disabled"
test -f "$home/.codex/skills/.disabled/lark-memory/SKILL.md" || fail "codex memory skill was not disabled"

status="$(run_memoryctl enable --json)"
assert_contains "$status" '"status": "enabled"'

status="$(run_memoryctl disable --skills-only --json)"
assert_contains "$status" '"status": "skills_disabled"'
test -x "$prefix/bin/lark-memory-cli" || fail "wrapper should stay active with --skills-only"
test ! -e "$home/.agents/skills/lark-memory" || fail "agents memory skill still active after --skills-only"
test -f "$home/.agents/skills/.disabled/lark-memory/SKILL.md" || fail "agents memory skill was not disabled by --skills-only"
