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
MEMORY_SKILL="lark-memory"
SHARED_SKILL="lark-shared"
SKILL_LABELS=("agents" "codex")
SKILL_ROOTS=("$HOME/.agents/skills" "$HOME/.codex/skills")
SKILL_STATES=()

usage() {
  cat <<'EOF'
Usage:
  memoryctl status [--json]
  memoryctl enable [--json]
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
  tmp="$(dirname "$dst")/.${MEMORY_SKILL}.tmp.$$"
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

ensure_shared_skill() {
  local root="$1"
  local active="$root/$SHARED_SKILL"
  local src="$SOURCE_DIR/skills/$SHARED_SKILL"
  if [[ ! -e "$active" && -d "$src" ]]; then
    copy_dir_atomic "$src" "$active"
  fi
}

enable_skill_root() {
  local root="$1"
  local active="$root/$MEMORY_SKILL"
  local disabled="$root/.disabled/$MEMORY_SKILL"
  local src="$SOURCE_DIR/skills/$MEMORY_SKILL"
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
  ensure_shared_skill "$root"
}

disable_skill_root() {
  local root="$1"
  local active="$root/$MEMORY_SKILL"
  local disabled="$root/.disabled/$MEMORY_SKILL"
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
  local state
  for state in "$@"; do
    [[ "$state" == "active" ]] && active_count=$((active_count + 1))
    [[ "$state" == "conflict" ]] && conflict_count=$((conflict_count + 1))
  done

  if [[ "$wrapper_state" == "conflict" || "$conflict_count" -gt 0 ]]; then
    printf 'partial'
  elif [[ "$wrapper_state" == "active" && "$app_exists" == "true" && "$active_count" -eq "${#SKILL_ROOTS[@]}" ]]; then
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
  local i
  SKILL_STATES=()
  for ((i = 0; i < ${#SKILL_ROOTS[@]}; i++)); do
    local root="${SKILL_ROOTS[$i]}"
    SKILL_STATES+=("$(path_state "$root/$MEMORY_SKILL" "$root/.disabled/$MEMORY_SKILL")")
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
  status="$(overall_status "$wrapper_state" "$app_exists" "${SKILL_STATES[@]}")"

  printf 'lark-memory status: %s\n\n' "$status"
  printf 'binary:\n'
  printf '  app:     %s  %s\n' "$APP_BIN" "$([[ "$app_exists" == true ]] && printf ok || printf missing)"
  printf '  wrapper: %s  %s\n' "$WRAPPER" "$wrapper_state"
  printf '  path:    %s' "$in_path"
  [[ -n "$cmd_path" ]] && printf ' (%s)' "$cmd_path"
  printf '\n\n'
  printf 'skills:\n'
  local i
  for ((i = 0; i < ${#SKILL_ROOTS[@]}; i++)); do
    printf '  %-6s %s/%s  %s\n' "${SKILL_LABELS[$i]}:" "${SKILL_ROOTS[$i]}" "$MEMORY_SKILL" "${SKILL_STATES[$i]}"
  done
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
  printf '  "skills": {\n'
  local i
  for ((i = 0; i < ${#SKILL_ROOTS[@]}; i++)); do
    local comma=','
    [[ "$i" -eq $((${#SKILL_ROOTS[@]} - 1)) ]] && comma=''
    printf '    "%s": {"path": "%s", "disabled_path": "%s", "state": "%s"}%s\n' \
      "$(json_escape "${SKILL_LABELS[$i]}")" \
      "$(json_escape "${SKILL_ROOTS[$i]}/$MEMORY_SKILL")" \
      "$(json_escape "${SKILL_ROOTS[$i]}/.disabled/$MEMORY_SKILL")" \
      "$(json_escape "${SKILL_STATES[$i]}")" \
      "$comma"
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
    enable_wrapper
    for root in "${SKILL_ROOTS[@]}"; do
      enable_skill_root "$root"
    done
    print_status
    ;;
  disable)
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
