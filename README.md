# lark-memory-cli

`lark-memory-cli` 是这个 fork 中面向 Memory Hub 的专用命令入口，目前只聚焦两个
shortcuts：

- `lark-memory-cli memory +list`
- `lark-memory-cli memory +get`

这两个命令使用用户身份调用，需要 `search:message` scope。Memory Hub 当前运行在
pre OpenAPI 域名和 `ppe_memory_hub` 泳道下；安装脚本会生成独立 wrapper，只在
`lark-memory-cli` 内部设置 pre 域名和泳道相关运行环境，不覆盖用户已有命令。

更完整的安装与排障说明见 [MEMORY_SHORTCUTS.md](./MEMORY_SHORTCUTS.md)。

## 一键安装

在具备 Codebase 访问权限和 Go 1.23+ 的 shell 中执行：

```bash
bash -lc 'set -euo pipefail
repo="git@code.byted.org:lark_search/larksuite-cli.git"
branch="${LARK_CLI_MEMORY_BRANCH:-jhn_memory}"
dir="${LARK_CLI_MEMORY_DIR:-$HOME/.lark-cli-memory}"
prefix="${LARK_CLI_PREFIX:-$HOME/.local}"
module="code.byted.org/lark_search/larksuite-cli"
app_dir="$prefix/libexec/lark-memory-cli"
wrapper="$prefix/bin/lark-memory-cli"

if [ -d "$dir/.git" ]; then
  git -C "$dir" fetch origin "$branch"
  git -C "$dir" checkout "$branch"
  git -C "$dir" pull --ff-only origin "$branch"
else
  git clone -b "$branch" "$repo" "$dir"
fi

if [ -n "${GO_BIN:-}" ]; then
  go_bin="$GO_BIN"
elif [ -x /opt/homebrew/opt/go/libexec/bin/go ]; then
  go_bin=/opt/homebrew/opt/go/libexec/bin/go
elif [ -x /usr/local/opt/go/libexec/bin/go ]; then
  go_bin=/usr/local/opt/go/libexec/bin/go
elif [ -x /usr/local/bytesuite-box/pkg/go/1.24.1/bin/go ]; then
  go_bin=/usr/local/bytesuite-box/pkg/go/1.24.1/bin/go
else
  go_bin="$(command -v go)"
fi

unset GOROOT
export GOTOOLCHAIN=local
"$go_bin" version

mkdir -p "$prefix/bin" "$app_dir"
(
  cd "$dir"
  version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
  build_date="$(date +%Y-%m-%d)"
  "$go_bin" build -trimpath \
    -ldflags "-s -w -X ${module}/internal/build.Version=${version} -X ${module}/internal/build.Date=${build_date}" \
    -o "$app_dir/lark-cli" .
)

{
  printf "%s\n" "#!/usr/bin/env bash"
  printf "%s\n" "export LARKSUITE_CLI_OPEN_BASE_URL=\"\${LARKSUITE_CLI_OPEN_BASE_URL:-https://open.feishu-pre.cn}\""
  printf "%s\n" "export LARKSUITE_CLI_REMOTE_META=\"\${LARKSUITE_CLI_REMOTE_META:-off}\""
  printf "%s\n" "exec \"$app_dir/lark-cli\" \"\$@\""
} > "$wrapper"
chmod +x "$wrapper"

sync_skill() {
  src="$1"
  dst_root="$2"
  skill_name="$(basename "$src")"
  tmp="$dst_root/.${skill_name}.tmp.$$"
  mkdir -p "$dst_root"
  rm -rf "$tmp"
  cp -R "$src" "$tmp"
  rm -rf "$dst_root/$skill_name"
  mv "$tmp" "$dst_root/$skill_name"
}

for skill_root in "$HOME/.agents/skills" "$HOME/.codex/skills"; do
  sync_skill "$dir/skills/lark-memory" "$skill_root"
  if [ ! -e "$skill_root/lark-shared" ]; then
    sync_skill "$dir/skills/lark-shared" "$skill_root"
  fi
done

shell_rc="$HOME/.zshrc"
grep -qxF "export PATH=\"$prefix/bin:\$PATH\"" "$shell_rc" 2>/dev/null || echo "export PATH=\"$prefix/bin:\$PATH\"" >> "$shell_rc"

export PATH="$prefix/bin:$PATH"
lark-memory-cli --version
'
```

安装完成后，重新打开终端，或执行：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

可选自定义项：

```bash
export LARK_CLI_MEMORY_DIR="$HOME/dev/larksuite-cli-memory"
export LARK_CLI_PREFIX="$HOME/.local"
export LARK_CLI_MEMORY_BRANCH="jhn_memory"
export GO_BIN="/path/to/go"
```

默认会复用现有配置和用户登录态。如果希望完全隔离配置，可以额外设置：

安装脚本也会把 `lark-memory` skill 同步到 `$HOME/.agents/skills/lark-memory`
和 `$HOME/.codex/skills/lark-memory`。如果目标目录缺少 `lark-shared`，会补一份
作为依赖。Codex/Agent 的 skill 列表通常在会话启动时加载；安装后命令立即可用，
但 `lark-memory` 要出现在 skill 列表里，需要重启或新开一个 Codex/Agent 会话。

```bash
export LARKSUITE_CLI_CONFIG_DIR="$HOME/.config/lark-memory-cli"
```

## 升级

安装完成后，后续升级可以直接执行：

```bash
lark-memory-cli --update
```

这个命令会拉取安装目录中的 `jhn_memory` 分支、重新构建
`$HOME/.local/libexec/lark-memory-cli/lark-cli`、重写 wrapper，并同步
`lark-memory` skill 到 Codex/Agent 的 skill 目录。

只检查是否有新提交，不执行安装：

```bash
lark-memory-cli update --check --json
```

## 登录授权

已有可用用户登录态时可以跳过本节。

```bash
lark-memory-cli config init
lark-memory-cli auth login --recommend
lark-memory-cli auth status
```

应用和用户授权必须包含 `search:message`。如果授权缺失，先确认应用 scope 已开通，
再重新执行登录。

## 查看 Memory 列表

```bash
lark-memory-cli memory +list --as user
```

输出 JSON：

```bash
lark-memory-cli memory +list --as user --format json
```

给 Agent 使用的精简 JSON：

```bash
lark-memory-cli memory +list --as user --format json \
  --jq '{count: (.data.memories | length), memories: (.data.memories | map({memory_key, name, status, default_variant_key}))}'
```

当前预期会看到：

- `personal_memory_snapshot`
- `personalized_conclusion`

## 获取单个 Memory

```bash
lark-memory-cli memory +get --as user --memory-key personal_memory_snapshot
```

输出 JSON：

```bash
lark-memory-cli memory +get --as user --memory-key personal_memory_snapshot --format json
```

不打印 payload 内容的安全检查命令：

```bash
lark-memory-cli memory +get --as user --memory-key personal_memory_snapshot --format json \
  --jq '{memory_key: .data.memory_key, variant_key: .data.variant_key, status: .data.status, payload_type: .data.payload_type, has_payload: (.data.payload != null and .data.payload != "")}'
```

可选参数：

```bash
lark-memory-cli memory +get --as user \
  --memory-key personalized_conclusion \
  --variant-key default \
  --payload-mode summary
```

`--payload-mode` 支持 `metadata`、`summary`、`full`，默认值是 `full`。

## 排障

确认 wrapper 内部使用的是 pre 域名：

```bash
head -n 5 "$(command -v lark-memory-cli)"
```

期望能看到：

```text
LARKSUITE_CLI_OPEN_BASE_URL="${LARKSUITE_CLI_OPEN_BASE_URL:-https://open.feishu-pre.cn}"
```

如果 `memory +list` 返回 `2200 Internal Error`，最常见原因是请求没有带 PPE
泳道头。本分支的 memory shortcuts 会自动发送 `x-tt-env: ppe_memory_hub`；如果仍然报错，
请确认已经从 `jhn_memory` 分支重新构建并安装。
