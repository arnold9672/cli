#!/usr/bin/env bash
# Copyright (c) 2026 Lark Technologies Pte. Ltd.
# SPDX-License-Identifier: MIT
set -euo pipefail

SOURCE_DIR="${LARK_CLI_MEMORY_DIR:-$HOME/.lark-cli-memory}"
PREFIX="${LARK_CLI_PREFIX:-$HOME/.local}"
APP_DIR="$PREFIX/libexec/lark-memory-cli"
APP_BIN="$APP_DIR/lark-cli"
WRAPPER="$PREFIX/bin/lark-memory-cli"
DISABLED_BIN_DIR="$PREFIX/bin/.disabled"
DISABLED_WRAPPER="$DISABLED_BIN_DIR/lark-memory-cli"
MANAGED_SKILLS_FILE="$SOURCE_DIR/skills/memory-managed-skills.txt"
MANAGED_SKILLS=()
LEGACY_MEMORY_SKILLS=("graph-search" "memory-graph-query")
RETIRED_AGENT_SKILLS=("memory-graph-query")
SHARED_SKILL="lark-shared"
SKILL_LABELS=("agents")
SKILL_ROOTS=("$HOME/.agents/skills")
SKILL_STATES=()
CODEX_SKILL_ROOT="$HOME/.codex/skills"
CODEX_DUPLICATE_ARCHIVE_ROOT="$CODEX_SKILL_ROOT/.disabled/lark-memory-cli-duplicates"
CODEX_DUPLICATES=()
ARCHIVED_CODEX_DUPLICATES=()
AGENT_RETIRED_ARCHIVE_ROOT="$HOME/.agents/skills/.disabled/lark-memory-cli-retired"
ACTIVE_RETIRED_AGENT_SKILLS=()
ARCHIVED_RETIRED_AGENT_SKILLS=()

usage() {
  cat <<'EOF'
Usage:
  memoryctl status [--json]
  memoryctl enable [--json]
  memoryctl refresh [--json]
  memoryctl disable [--json] [--skills-only]

Environment:
  LARK_CLI_MEMORY_DIR    source checkout, default: $HOME/.lark-cli-memory
  LARK_CLI_PREFIX        install prefix, default: $HOME/.local
EOF
}

fail() {
  printf 'memoryctl: %s\n' "$*" >&2
  exit 1
}

load_managed_skills() {
  [[ -f "$MANAGED_SKILLS_FILE" ]] || fail "managed skills manifest not found: $MANAGED_SKILLS_FILE"
  local raw skill
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    skill="${raw%%#*}"
    skill="${skill#"${skill%%[![:space:]]*}"}"
    skill="${skill%"${skill##*[![:space:]]}"}"
    [[ -z "$skill" ]] && continue
    [[ "$skill" =~ ^[a-z0-9][a-z0-9-]*$ ]] || fail "invalid managed skill name: $skill"
    local existing
    for existing in "${MANAGED_SKILLS[@]:-}"; do
      [[ "$existing" != "$skill" ]] || fail "duplicate managed skill name: $skill"
    done
    [[ -d "$SOURCE_DIR/skills/$skill" ]] || fail "managed skill source not found: $SOURCE_DIR/skills/$skill"
    MANAGED_SKILLS+=("$skill")
  done < "$MANAGED_SKILLS_FILE"
  [[ ${#MANAGED_SKILLS[@]} -gt 0 ]] || fail "managed skills manifest is empty: $MANAGED_SKILLS_FILE"
  local found_primary=false
  local existing
  for existing in "${MANAGED_SKILLS[@]}"; do
    [[ "$existing" != "lark-memory" ]] || found_primary=true
  done
  [[ "$found_primary" == true ]] || fail "managed skills manifest must include lark-memory"
}

load_managed_skills

json_escape() {
  local s="${1:-}"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

path_state() {
  local active="$1"
  local disabled="$2"
  if [[ -e "$active" && -e "$disabled" ]]; then
    printf 'conflict'
  elif [[ -e "$active" ]]; then
    printf 'active'
  elif [[ -e "$disabled" ]]; then
    printf 'disabled'
  else
    printf 'missing'
  fi
}

copy_dir_atomic() {
  local src="$1"
  local dst="$2"
  [[ -d "$src" ]] || fail "skill source not found: $src"
  mkdir -p "$(dirname "$dst")"
  local tmp
  tmp="$(dirname "$dst")/.$(basename "$dst").tmp.$$"
  rm -rf "$tmp"
  mkdir -p "$tmp"
  cp -R "$src"/. "$tmp"/
  rm -rf "$dst"
  mv "$tmp" "$dst"
}

write_wrapper() {
  local target="$1"
  [[ -x "$APP_BIN" ]] || fail "memory binary not found or not executable: $APP_BIN"
  mkdir -p "$(dirname "$target")"
  {
    printf '%s\n' '#!/usr/bin/env bash'
    printf '%s\n' 'export LARKSUITE_CLI_REMOTE_META="${LARKSUITE_CLI_REMOTE_META:-off}"'
    printf 'exec "%s" "$@"\n' "$APP_BIN"
  } > "$target"
  chmod +x "$target"
}

enable_wrapper() {
  local state
  state="$(path_state "$WRAPPER" "$DISABLED_WRAPPER")"
  case "$state" in
    active)
      write_wrapper "$WRAPPER"
      ;;
    disabled)
      mkdir -p "$(dirname "$WRAPPER")"
      mv "$DISABLED_WRAPPER" "$WRAPPER"
      write_wrapper "$WRAPPER"
      ;;
    missing)
      write_wrapper "$WRAPPER"
      ;;
    conflict)
      fail "wrapper exists in both active and disabled locations: $WRAPPER and $DISABLED_WRAPPER"
      ;;
  esac
}

disable_wrapper() {
  local state
  state="$(path_state "$WRAPPER" "$DISABLED_WRAPPER")"
  case "$state" in
    active)
      mkdir -p "$DISABLED_BIN_DIR"
      mv "$WRAPPER" "$DISABLED_WRAPPER"
      ;;
    disabled|missing)
      ;;
    conflict)
      fail "wrapper exists in both active and disabled locations: $WRAPPER and $DISABLED_WRAPPER"
      ;;
  esac
}

refresh_wrapper() {
  local state
  state="$(path_state "$WRAPPER" "$DISABLED_WRAPPER")"
  case "$state" in
    active)
      write_wrapper "$WRAPPER"
      ;;
    disabled)
      write_wrapper "$DISABLED_WRAPPER"
      ;;
    missing)
      ;;
    conflict)
      fail "wrapper exists in both active and disabled locations: $WRAPPER and $DISABLED_WRAPPER"
      ;;
  esac
}

ensure_shared_skill() {
  local root="$1"
  local active="$root/$SHARED_SKILL"
  local src="$SOURCE_DIR/skills/$SHARED_SKILL"
  if [[ ! -e "$active" && -d "$src" ]]; then
    copy_dir_atomic "$src" "$active"
  fi
}

enable_named_skill_root() {
  local root="$1"
  local skill="$2"
  local active="$root/$skill"
  local disabled="$root/.disabled/$skill"
  local src="$SOURCE_DIR/skills/$skill"
  local state
  state="$(path_state "$active" "$disabled")"
  case "$state" in
    active)
      copy_dir_atomic "$src" "$active"
      ;;
    disabled)
      mkdir -p "$root"
      mv "$disabled" "$active"
      copy_dir_atomic "$src" "$active"
      ;;
    missing)
      copy_dir_atomic "$src" "$active"
      ;;
    conflict)
      fail "skill exists in both active and disabled locations: $active and $disabled"
      ;;
  esac
}

enable_skill_root() {
  local root="$1"
  local skill
  for skill in "${MANAGED_SKILLS[@]}"; do
    enable_named_skill_root "$root" "$skill"
  done
  ensure_shared_skill "$root"
}

disable_named_skill_root() {
  local root="$1"
  local skill="$2"
  local active="$root/$skill"
  local disabled="$root/.disabled/$skill"
  local state
  state="$(path_state "$active" "$disabled")"
  case "$state" in
    active)
      mkdir -p "$(dirname "$disabled")"
      mv "$active" "$disabled"
      ;;
    disabled|missing)
      ;;
    conflict)
      fail "skill exists in both active and disabled locations: $active and $disabled"
      ;;
  esac
}

disable_skill_root() {
  local root="$1"
  local skill
  for skill in "${MANAGED_SKILLS[@]}"; do
    disable_named_skill_root "$root" "$skill"
  done
}

refresh_named_skill_root() {
  local root="$1"
  local skill="$2"
  local primary_state="$3"
  local active="$root/$skill"
  local disabled="$root/.disabled/$skill"
  local src="$SOURCE_DIR/skills/$skill"
  local state target
  state="$(path_state "$active" "$disabled")"
  case "$state" in
    active)
      target="$active"
      ;;
    disabled)
      target="$disabled"
      ;;
    missing)
      if [[ "$primary_state" == "disabled" ]]; then
        target="$disabled"
      else
        target="$active"
      fi
      ;;
    conflict)
      fail "skill exists in both active and disabled locations: $active and $disabled"
      ;;
  esac
  copy_dir_atomic "$src" "$target"
}

refresh_skill_root() {
  local root="$1"
  local primary_state
  primary_state="$(path_state "$root/lark-memory" "$root/.disabled/lark-memory")"
  [[ "$primary_state" != "conflict" ]] || fail "lark-memory exists in both active and disabled locations under $root"
  if [[ "$primary_state" == "missing" && -e "$CODEX_SKILL_ROOT/.disabled/lark-memory" ]]; then
    primary_state="disabled"
  fi
  local skill
  for skill in "${MANAGED_SKILLS[@]}"; do
    refresh_named_skill_root "$root" "$skill" "$primary_state"
  done
  ensure_shared_skill "$root"
}

next_skill_archive_path() {
  local archive_root="$1"
  local skill="$2"
  local suffix=0
  local candidate
  while ((suffix < 1000)); do
    candidate="$archive_root/$skill"
    ((suffix == 0)) || candidate="$candidate.$suffix"
    if [[ ! -e "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
    suffix=$((suffix + 1))
  done
  fail "no available skill archive path for $skill under $archive_root"
}

next_duplicate_archive_path() {
  next_skill_archive_path "$CODEX_DUPLICATE_ARCHIVE_ROOT" "$1"
}

archive_codex_duplicate() {
  local skill="$1"
  local active="$CODEX_SKILL_ROOT/$skill"
  [[ -e "$active" ]] || return 0
  local target
  target="$(next_duplicate_archive_path "$skill")"
  mkdir -p "$(dirname "$target")"
  mv "$active" "$target"
  ARCHIVED_CODEX_DUPLICATES+=("$target")
}

archive_codex_duplicates() {
  ARCHIVED_CODEX_DUPLICATES=()
  local skill
  for skill in "${MANAGED_SKILLS[@]}" "${LEGACY_MEMORY_SKILLS[@]}"; do
    archive_codex_duplicate "$skill"
  done
}

collect_codex_duplicates() {
  CODEX_DUPLICATES=()
  local skill
  for skill in "${MANAGED_SKILLS[@]}" "${LEGACY_MEMORY_SKILLS[@]}"; do
    [[ ! -e "$CODEX_SKILL_ROOT/$skill" ]] || CODEX_DUPLICATES+=("$CODEX_SKILL_ROOT/$skill")
  done
}

archive_retired_agent_skills() {
  ARCHIVED_RETIRED_AGENT_SKILLS=()
  local skill active target
  for skill in "${RETIRED_AGENT_SKILLS[@]}"; do
    active="$HOME/.agents/skills/$skill"
    [[ -e "$active" ]] || continue
    target="$(next_skill_archive_path "$AGENT_RETIRED_ARCHIVE_ROOT" "$skill")"
    mkdir -p "$(dirname "$target")"
    mv "$active" "$target"
    ARCHIVED_RETIRED_AGENT_SKILLS+=("$target")
  done
}

collect_retired_agent_skills() {
  ACTIVE_RETIRED_AGENT_SKILLS=()
  local skill active
  for skill in "${RETIRED_AGENT_SKILLS[@]}"; do
    active="$HOME/.agents/skills/$skill"
    [[ ! -e "$active" ]] || ACTIVE_RETIRED_AGENT_SKILLS+=("$active")
  done
}

source_branch() {
  if [[ -d "$SOURCE_DIR/.git" ]]; then
    git -C "$SOURCE_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || true
  fi
}

source_commit() {
  if [[ -d "$SOURCE_DIR/.git" ]]; then
    git -C "$SOURCE_DIR" rev-parse --short HEAD 2>/dev/null || true
  fi
}

command_path() {
  command -v lark-memory-cli 2>/dev/null || true
}

overall_status() {
  local wrapper_state="$1"
  local app_exists="$2"
  shift 2
  local active_count=0
  local conflict_count=0
  local expected_count
  expected_count=$((${#SKILL_ROOTS[@]} * ${#MANAGED_SKILLS[@]}))
  local state
  for state in "$@"; do
    [[ "$state" == "active" ]] && active_count=$((active_count + 1))
    [[ "$state" == "conflict" ]] && conflict_count=$((conflict_count + 1))
  done

  if [[ "$wrapper_state" == "conflict" || "$conflict_count" -gt 0 || ${#CODEX_DUPLICATES[@]} -gt 0 || ${#ACTIVE_RETIRED_AGENT_SKILLS[@]} -gt 0 ]]; then
    printf 'partial'
  elif [[ "$wrapper_state" == "active" && "$app_exists" == "true" && "$active_count" -eq "$expected_count" ]]; then
    printf 'enabled'
  elif [[ "$wrapper_state" == "active" && "$active_count" -eq 0 ]]; then
    printf 'skills_disabled'
  elif [[ "$wrapper_state" != "active" && "$active_count" -eq 0 ]]; then
    printf 'disabled'
  else
    printf 'partial'
  fi
}

collect_skill_states() {
  local i skill
  SKILL_STATES=()
  for ((i = 0; i < ${#SKILL_ROOTS[@]}; i++)); do
    local root="${SKILL_ROOTS[$i]}"
    for skill in "${MANAGED_SKILLS[@]}"; do
      SKILL_STATES+=("$(path_state "$root/$skill" "$root/.disabled/$skill")")
    done
  done
}

print_status_text() {
  local wrapper_state app_exists cmd_path in_path branch commit status
  wrapper_state="$(path_state "$WRAPPER" "$DISABLED_WRAPPER")"
  app_exists=false
  [[ -x "$APP_BIN" ]] && app_exists=true
  cmd_path="$(command_path)"
  in_path=false
  [[ "$cmd_path" == "$WRAPPER" ]] && in_path=true
  branch="$(source_branch)"
  commit="$(source_commit)"
  collect_skill_states
  collect_codex_duplicates
  collect_retired_agent_skills
  status="$(overall_status "$wrapper_state" "$app_exists" "${SKILL_STATES[@]}")"

  printf 'lark-memory status: %s\n\n' "$status"
  printf 'binary:\n'
  printf '  app:     %s  %s\n' "$APP_BIN" "$([[ "$app_exists" == true ]] && printf ok || printf missing)"
  printf '  wrapper: %s  %s\n' "$WRAPPER" "$wrapper_state"
  printf '  path:    %s' "$in_path"
  [[ -n "$cmd_path" ]] && printf ' (%s)' "$cmd_path"
  printf '\n\n'
  printf 'skills:\n'
  local i skill state_index=0
  for ((i = 0; i < ${#SKILL_ROOTS[@]}; i++)); do
    for skill in "${MANAGED_SKILLS[@]}"; do
      printf '  %-6s %s/%s  %s\n' "${SKILL_LABELS[$i]}:" "${SKILL_ROOTS[$i]}" "$skill" "${SKILL_STATES[$state_index]}"
      state_index=$((state_index + 1))
    done
  done
  if [[ ${#CODEX_DUPLICATES[@]} -gt 0 ]]; then
    printf '\nduplicate Codex skills:\n'
    printf '  %s\n' "${CODEX_DUPLICATES[@]}"
    printf '  run memoryctl refresh to archive these duplicate selector entries\n'
  fi
  if [[ ${#ARCHIVED_CODEX_DUPLICATES[@]} -gt 0 ]]; then
    printf '\narchived duplicate Codex skills:\n'
    printf '  %s\n' "${ARCHIVED_CODEX_DUPLICATES[@]}"
  fi
  if [[ ${#ACTIVE_RETIRED_AGENT_SKILLS[@]} -gt 0 ]]; then
    printf '\nretired Agent selectors still active:\n'
    printf '  %s\n' "${ACTIVE_RETIRED_AGENT_SKILLS[@]}"
    printf '  run memoryctl refresh to archive these retired selector entries\n'
  fi
  if [[ ${#ARCHIVED_RETIRED_AGENT_SKILLS[@]} -gt 0 ]]; then
    printf '\narchived retired Agent selectors:\n'
    printf '  %s\n' "${ARCHIVED_RETIRED_AGENT_SKILLS[@]}"
  fi
  printf '\n'
  printf 'source:\n'
  printf '  dir:    %s\n' "$SOURCE_DIR"
  printf '  branch: %s\n' "${branch:-unknown}"
  printf '  commit: %s\n' "${commit:-unknown}"
  printf '\n'
  printf 'next:\n'
  printf '  restart Codex/Agent sessions for skill discovery changes to take effect\n'
}

print_status_json() {
  local wrapper_state app_exists cmd_path in_path branch commit status
  wrapper_state="$(path_state "$WRAPPER" "$DISABLED_WRAPPER")"
  app_exists=false
  [[ -x "$APP_BIN" ]] && app_exists=true
  cmd_path="$(command_path)"
  in_path=false
  [[ "$cmd_path" == "$WRAPPER" ]] && in_path=true
  branch="$(source_branch)"
  commit="$(source_commit)"
  collect_skill_states
  collect_codex_duplicates
  collect_retired_agent_skills
  status="$(overall_status "$wrapper_state" "$app_exists" "${SKILL_STATES[@]}")"

  printf '{\n'
  printf '  "ok": true,\n'
  printf '  "status": "%s",\n' "$(json_escape "$status")"
  printf '  "binary": {\n'
  printf '    "app": "%s",\n' "$(json_escape "$APP_BIN")"
  printf '    "app_exists": %s,\n' "$app_exists"
  printf '    "wrapper": "%s",\n' "$(json_escape "$WRAPPER")"
  printf '    "disabled_wrapper": "%s",\n' "$(json_escape "$DISABLED_WRAPPER")"
  printf '    "wrapper_state": "%s",\n' "$(json_escape "$wrapper_state")"
  printf '    "path": "%s",\n' "$(json_escape "$cmd_path")"
  printf '    "in_path": %s\n' "$in_path"
  printf '  },\n'
  printf '  "duplicate_codex_skill_count": %d,\n' "${#CODEX_DUPLICATES[@]}"
  printf '  "archived_duplicate_codex_skill_count": %d,\n' "${#ARCHIVED_CODEX_DUPLICATES[@]}"
  printf '  "duplicate_archive_root": "%s",\n' "$(json_escape "$CODEX_DUPLICATE_ARCHIVE_ROOT")"
  printf '  "active_retired_agent_skill_count": %d,\n' "${#ACTIVE_RETIRED_AGENT_SKILLS[@]}"
  printf '  "archived_retired_agent_skill_count": %d,\n' "${#ARCHIVED_RETIRED_AGENT_SKILLS[@]}"
  printf '  "retired_agent_skill_archive_root": "%s",\n' "$(json_escape "$AGENT_RETIRED_ARCHIVE_ROOT")"
  printf '  "skills": {\n'
  local i j skill state_index=0
  for ((i = 0; i < ${#SKILL_ROOTS[@]}; i++)); do
    local comma=','
    [[ "$i" -eq $((${#SKILL_ROOTS[@]} - 1)) ]] && comma=''
    printf '    "%s": {\n' "$(json_escape "${SKILL_LABELS[$i]}")"
    for ((j = 0; j < ${#MANAGED_SKILLS[@]}; j++)); do
      skill="${MANAGED_SKILLS[$j]}"
      local skill_comma=','
      [[ "$j" -eq $((${#MANAGED_SKILLS[@]} - 1)) ]] && skill_comma=''
      printf '      "%s": {"path": "%s", "disabled_path": "%s", "state": "%s"}%s\n' \
        "$(json_escape "$skill")" \
        "$(json_escape "${SKILL_ROOTS[$i]}/$skill")" \
        "$(json_escape "${SKILL_ROOTS[$i]}/.disabled/$skill")" \
        "$(json_escape "${SKILL_STATES[$state_index]}")" \
        "$skill_comma"
      state_index=$((state_index + 1))
    done
    printf '    }%s\n' "$comma"
  done
  printf '  },\n'
  printf '  "source": {\n'
  printf '    "dir": "%s",\n' "$(json_escape "$SOURCE_DIR")"
  printf '    "branch": "%s",\n' "$(json_escape "$branch")"
  printf '    "commit": "%s"\n' "$(json_escape "$commit")"
  printf '  },\n'
  printf '  "restart_required": true\n'
  printf '}\n'
}

print_status() {
  if [[ "${JSON_OUTPUT:-false}" == true ]]; then
    print_status_json
  else
    print_status_text
  fi
}

cmd="${1:-status}"
[[ $# -gt 0 ]] && shift || true
JSON_OUTPUT=false
SKILLS_ONLY=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json)
      JSON_OUTPUT=true
      ;;
    --skills-only)
      SKILLS_ONLY=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
  shift
done

case "$cmd" in
  status)
    print_status
    ;;
  enable)
    archive_retired_agent_skills
    archive_codex_duplicates
    enable_wrapper
    for root in "${SKILL_ROOTS[@]}"; do
      enable_skill_root "$root"
    done
    print_status
    ;;
  refresh)
    archive_retired_agent_skills
    archive_codex_duplicates
    refresh_wrapper
    for root in "${SKILL_ROOTS[@]}"; do
      refresh_skill_root "$root"
    done
    print_status
    ;;
  disable)
    archive_retired_agent_skills
    archive_codex_duplicates
    for root in "${SKILL_ROOTS[@]}"; do
      disable_skill_root "$root"
    done
    if [[ "$SKILLS_ONLY" != true ]]; then
      disable_wrapper
    fi
    print_status
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    fail "unknown command: $cmd"
    ;;
esac
