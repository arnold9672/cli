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

managed_skills=("lark-memory" "memory-list" "memory-get" "memory-graph-range" "memory-graph-one-hop" "memory-graph-search" "memory-writing-style")
mkdir -p "$source_dir/skills/lark-shared" "$app_dir" "$home"
for skill in "${managed_skills[@]}"; do
  mkdir -p "$source_dir/skills/$skill"
  printf '%s\n' "$skill skill v1" > "$source_dir/skills/$skill/SKILL.md"
done
printf '%s\n' '#!/usr/bin/env bash' 'echo "fake lark-memory-cli"' > "$app_dir/lark-cli"
chmod +x "$app_dir/lark-cli"
printf '%s\n' 'shared skill v1' > "$source_dir/skills/lark-shared/SKILL.md"
printf '%s\n' "${managed_skills[@]}" > "$source_dir/skills/memory-managed-skills.txt"

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
if grep -Fq 'open.feishu-pre.cn' "$prefix/bin/lark-memory-cli"; then
  fail "wrapper should not force pre OpenAPI domain"
fi
test -f "$home/.agents/skills/lark-memory/SKILL.md" || fail "agents memory skill was not enabled"
test -f "$home/.agents/skills/memory-graph-search/SKILL.md" || fail "agents graph search skill was not enabled"
test -f "$home/.agents/skills/memory-writing-style/SKILL.md" || fail "agents writing style skill was not enabled"
for skill in memory-list memory-get memory-graph-range memory-graph-one-hop; do
  test -f "$home/.agents/skills/$skill/SKILL.md" || fail "agents $skill selector was not enabled"
done
test -f "$home/.agents/skills/lark-shared/SKILL.md" || fail "agents shared skill was not installed"
test ! -e "$home/.codex/skills/lark-memory" || fail "memory skill was duplicated into codex root"
test ! -e "$home/.codex/skills/memory-graph-search" || fail "graph search skill was duplicated into codex root"

mkdir -p "$home/.agents/skills/memory-graph-query"
printf '%s\n' 'retired graph query selector' > "$home/.agents/skills/memory-graph-query/SKILL.md"

for skill in "${managed_skills[@]}" graph-search memory-graph-query; do
  mkdir -p "$home/.codex/skills/$skill"
  printf '%s\n' "$skill legacy duplicate" > "$home/.codex/skills/$skill/SKILL.md"
done
status="$(run_memoryctl status --json)"
assert_contains "$status" '"status": "partial"'
assert_contains "$status" '"duplicate_codex_skill_count": 9'
assert_contains "$status" '"active_retired_agent_skill_count": 1'
status="$(run_memoryctl refresh --json)"
assert_contains "$status" '"status": "enabled"'
assert_contains "$status" '"duplicate_codex_skill_count": 0'
assert_contains "$status" '"archived_duplicate_codex_skill_count": 9'
assert_contains "$status" '"active_retired_agent_skill_count": 0'
assert_contains "$status" '"archived_retired_agent_skill_count": 1'
test ! -e "$home/.agents/skills/memory-graph-query" || fail "refresh left retired graph query selector active"
test -f "$home/.agents/skills/.disabled/lark-memory-cli-retired/memory-graph-query/SKILL.md" || fail "refresh did not archive retired graph query selector"
for skill in "${managed_skills[@]}" graph-search memory-graph-query; do
  test ! -e "$home/.codex/skills/$skill" || fail "refresh left duplicate codex skill active: $skill"
  test -f "$home/.codex/skills/.disabled/lark-memory-cli-duplicates/$skill/SKILL.md" || fail "refresh did not archive codex duplicate: $skill"
done

rm -rf "$home/.agents/skills/memory-graph-search"
status="$(run_memoryctl refresh --json)"
assert_contains "$status" '"status": "enabled"'
test -f "$home/.agents/skills/memory-graph-search/SKILL.md" || fail "refresh did not restore missing active agents graph search skill"

printf '%s\n' 'memory skill v2' > "$source_dir/skills/lark-memory/SKILL.md"
printf '%s\n' 'graph search skill v2' > "$source_dir/skills/memory-graph-search/SKILL.md"
status="$(run_memoryctl enable --json)"
assert_contains "$status" '"status": "enabled"'
grep -Fq 'memory skill v2' "$home/.agents/skills/lark-memory/SKILL.md" || fail "enable did not refresh active agents memory skill"
grep -Fq 'graph search skill v2' "$home/.agents/skills/memory-graph-search/SKILL.md" || fail "enable did not refresh active agents graph search skill"

mkdir -p "$source_dir/skills/future-memory-skill"
printf '%s\n' 'future memory skill v1' > "$source_dir/skills/future-memory-skill/SKILL.md"
printf '%s\n' "${managed_skills[@]}" 'future-memory-skill' > "$source_dir/skills/memory-managed-skills.txt"
status="$(run_memoryctl refresh --json)"
assert_contains "$status" '"status": "enabled"'
test -f "$home/.agents/skills/future-memory-skill/SKILL.md" || fail "refresh did not discover future agents skill from manifest"
test ! -e "$home/.codex/skills/future-memory-skill" || fail "refresh duplicated future skill into codex root"

status="$(run_memoryctl disable --json)"
assert_contains "$status" '"status": "disabled"'
test ! -e "$prefix/bin/lark-memory-cli" || fail "active wrapper still exists after disable"
test -x "$prefix/bin/.disabled/lark-memory-cli" || fail "disabled wrapper was not retained"
test ! -e "$home/.agents/skills/lark-memory" || fail "agents memory skill still active after disable"
test ! -e "$home/.agents/skills/memory-graph-search" || fail "agents graph search skill still active after disable"
test ! -e "$home/.agents/skills/memory-writing-style" || fail "agents writing style skill still active after disable"
test -f "$home/.agents/skills/.disabled/lark-memory/SKILL.md" || fail "agents memory skill was not disabled"
test -f "$home/.agents/skills/.disabled/memory-graph-search/SKILL.md" || fail "agents graph search skill was not disabled"
test -f "$home/.agents/skills/.disabled/memory-writing-style/SKILL.md" || fail "agents writing style skill was not disabled"

rm -rf "$home/.agents/skills/.disabled/memory-graph-search"
status="$(run_memoryctl refresh --json)"
assert_contains "$status" '"status": "disabled"'
test -f "$home/.agents/skills/.disabled/memory-graph-search/SKILL.md" || fail "refresh did not restore missing disabled agents graph search skill"
test ! -e "$home/.agents/skills/memory-graph-search" || fail "refresh re-enabled disabled agents graph search skill"

status="$(run_memoryctl enable --json)"
assert_contains "$status" '"status": "enabled"'

status="$(run_memoryctl disable --skills-only --json)"
assert_contains "$status" '"status": "skills_disabled"'
test -x "$prefix/bin/lark-memory-cli" || fail "wrapper should stay active with --skills-only"
test ! -e "$home/.agents/skills/lark-memory" || fail "agents memory skill still active after --skills-only"
test ! -e "$home/.agents/skills/memory-graph-search" || fail "agents graph search skill still active after --skills-only"
test ! -e "$home/.agents/skills/memory-writing-style" || fail "agents writing style skill still active after --skills-only"
test -f "$home/.agents/skills/.disabled/lark-memory/SKILL.md" || fail "agents memory skill was not disabled by --skills-only"
test -f "$home/.agents/skills/.disabled/memory-graph-search/SKILL.md" || fail "agents graph search skill was not disabled by --skills-only"
test -f "$home/.agents/skills/.disabled/memory-writing-style/SKILL.md" || fail "agents writing style skill was not disabled by --skills-only"

legacy_home="$tmp/legacy-home"
mkdir -p "$legacy_home/.codex/skills/.disabled/lark-memory"
printf '%s\n' 'legacy disabled memory' > "$legacy_home/.codex/skills/.disabled/lark-memory/SKILL.md"
legacy_status="$(
  HOME="$legacy_home" \
  LARK_CLI_MEMORY_DIR="$source_dir" \
  LARK_CLI_PREFIX="$tmp/legacy-prefix" \
  PATH="$tmp/legacy-prefix/bin:$PATH" \
  bash "$repo_root/scripts/memoryctl.sh" refresh --json
)"
assert_contains "$legacy_status" '"status": "disabled"'
test -f "$legacy_home/.agents/skills/.disabled/memory-list/SKILL.md" || fail "legacy codex disabled state did not carry to canonical agents root"
test ! -e "$legacy_home/.agents/skills/memory-list" || fail "legacy codex disabled state was re-enabled"
