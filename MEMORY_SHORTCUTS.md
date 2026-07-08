# Memory Shortcuts 安装与使用指南

这份指南面向只需要使用 Memory Hub shortcuts 的用户：

- `lark-memory-cli memory +list`
- `lark-memory-cli memory +get`

这两个 shortcut 使用用户身份调用，需要 `search:message` scope。当前 Memory
Hub 后端运行在 pre OpenAPI 域名和 `ppe_memory_hub` 泳道下，`x-tt-env:
ppe_memory_hub` 请求头已经内置在 shortcut 中。

安装脚本不会覆盖用户已有的 `lark-cli`。它会安装一个独立命令
`lark-memory-cli`，并且只在这个命令内部把 OpenAPI 域名指向 pre。
默认情况下，`lark-memory-cli` 复用现有 `lark-cli` 的配置和用户登录态；如果你想
完全隔离配置，可以运行时额外设置 `LARKSUITE_CLI_CONFIG_DIR`。

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

安装脚本会完成以下事情：

- 拉取 `jhn_memory` 分支到 `$HOME/.lark-cli-memory`。
- 自动选择一个可用的 Go 1.23+，并清理旧 `GOROOT` 对构建的影响。
- 直接执行 `go build`，实际二进制默认放到 `$HOME/.local/libexec/lark-memory-cli/lark-cli`。
- 生成 `$HOME/.local/bin/lark-memory-cli` wrapper。
- 同步 `lark-memory` skill 到 `$HOME/.agents/skills/lark-memory` 和
  `$HOME/.codex/skills/lark-memory`。
- 如果目标 skill root 下还没有 `lark-shared`，会同步一份 `lark-shared` 作为
  `lark-memory` 的依赖。
- 向 `~/.zshrc` 写入 `$HOME/.local/bin` 到 `PATH`。
- 不改写已有 `lark-cli`，也不向 shell 写入全局 pre 域名。

注意：Codex/Agent 的 skill 列表通常在会话启动时加载。安装后命令可以立即使用；
如果希望 `lark-memory` 出现在当前工具的 skill 列表里，请重启或新开一个 Codex/Agent
会话。

安装完成后，重新打开一个终端，或直接执行：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

如果你希望自定义安装目录，可以在执行一键安装前设置：

```bash
export LARK_CLI_MEMORY_DIR="$HOME/dev/larksuite-cli-memory"
export LARK_CLI_PREFIX="$HOME/.local"
export LARK_CLI_MEMORY_BRANCH="jhn_memory"
export GO_BIN="/path/to/go"
```

如果希望和现有 `lark-cli` 配置完全隔离，可以额外设置：

```bash
export LARKSUITE_CLI_CONFIG_DIR="$HOME/.config/lark-memory-cli"
```

## 升级

安装完成后，后续升级可以直接执行：

```bash
lark-memory-cli --update
```

升级命令会复用安装脚本的目录约定：

- 源码目录：`${LARK_CLI_MEMORY_DIR:-$HOME/.lark-cli-memory}`
- 二进制：`${LARK_CLI_PREFIX:-$HOME/.local}/libexec/lark-memory-cli/lark-cli`
- wrapper：`${LARK_CLI_PREFIX:-$HOME/.local}/bin/lark-memory-cli`

它会拉取 `jhn_memory` 分支、重新构建二进制、重写 wrapper，并同步
`lark-memory` skill 到 `$HOME/.agents/skills/lark-memory` 和
`$HOME/.codex/skills/lark-memory`。

只检查是否有新提交，不执行安装：

```bash
lark-memory-cli update --check --json
```

## 登录授权

如果本机已经有可用的 `lark-cli` profile 和用户登录态，可以跳过本节。
`lark-memory-cli` 默认读取同一份配置。

```bash
lark-memory-cli config init
lark-memory-cli auth login --recommend
lark-memory-cli auth status
```

应用和用户授权必须包含 `search:message`。如果授权缺失，先确认应用 scope 已经开通，
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

当前预期会看到的 memory 包括：

- `personal_memory_snapshot`
- `personalized_conclusion`

## 获取单个 Memory

`+get` 默认返回完整 payload。

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

确认 `lark-memory-cli` wrapper 内部使用的是 pre 域名：

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

等价的原始请求形态：

```bash
curl -i -X GET 'https://open.feishu-pre.cn/open-apis/search/v2/memory_hub/list_memory' \
  -H "Authorization: Bearer <USER_ACCESS_TOKEN>" \
  -H 'x-tt-env: ppe_memory_hub' \
  -H 'Content-Type: application/json' \
  --data '{}'
```
