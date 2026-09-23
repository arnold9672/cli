# Memory Shortcuts 安装与使用指南

这份指南面向只需要使用 Memory Hub shortcuts 的用户：

- `lark-memory-cli memory +list`
- `lark-memory-cli memory +get`
- `lark-memory-cli memory +graph-range`
- `lark-memory-cli memory +graph-one-hop`
- `lark-memory-cli memory +graph-search`
- `lark-memory-cli memory +writing-style`

这六个 shortcut 使用用户身份调用，需要 `memory:hub` scope。当前 Memory Hub 默认调用线上
OpenAPI 域名；通用 Memory/Graph/FaaS 命令默认发送 `x-tt-env: ppe_memory_hub`，
`+writing-style` 默认读取 `ppe_memory_schema`，均可通过 `LARKSUITE_CLI_MEMORY_TT_ENV`
为当前进程覆盖。

安装脚本不会覆盖用户已有的 `lark-cli`。它会安装一个独立命令
`lark-memory-cli`。
默认情况下，`lark-memory-cli` 复用现有 `lark-cli` 的配置和用户登录态；如果你想
完全隔离配置，可以运行时额外设置 `LARKSUITE_CLI_CONFIG_DIR`。

## 一键安装

在安装有 Git 和 Go 1.23+ 的 shell 中执行。源码来自公开 GitHub 仓库，
无需 Codebase 权限或 GitHub 登录：

```bash
bash -lc 'set -euo pipefail
repo="https://github.com/arnold9672/cli.git"
branch="${LARK_CLI_MEMORY_BRANCH:-jhn_memory}"
dir="${LARK_CLI_MEMORY_DIR:-$HOME/.lark-cli-memory}"
prefix="${LARK_CLI_PREFIX:-$HOME/.local}"
module="code.byted.org/lark_search/larksuite-cli"
app_dir="$prefix/libexec/lark-memory-cli"
wrapper="$prefix/bin/lark-memory-cli"

if [ -d "$dir/.git" ]; then
  git -C "$dir" remote set-url origin "$repo"
  git -C "$dir" fetch origin "$branch"
  git -C "$dir" checkout "$branch"
  git -C "$dir" pull --ff-only origin "$branch"
else
  git clone -b "$branch" "$repo" "$dir"
fi

go_bin="$(GO_BIN="${GO_BIN:-}" bash "$dir/scripts/memory-upgrade.sh" --print-go)"

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

mkdir -p "$dir/bin"
cp "$dir/scripts/memoryctl.sh" "$dir/bin/memoryctl"
chmod +x "$dir/bin/memoryctl"
LARK_CLI_MEMORY_DIR="$dir" LARK_CLI_PREFIX="$prefix" "$dir/bin/memoryctl" enable

shell_rc="$HOME/.zshrc"
grep -qxF "export PATH=\"$prefix/bin:\$PATH\"" "$shell_rc" 2>/dev/null || echo "export PATH=\"$prefix/bin:\$PATH\"" >> "$shell_rc"

export PATH="$prefix/bin:$PATH"
lark-memory-cli --version
'
```

安装脚本会完成以下事情：

- 从公开 GitHub 仓库拉取 `jhn_memory` 分支到 `$HOME/.lark-cli-memory`；已有安装会自动把旧 Codebase `origin` 迁移到 GitHub。
- 自动选择一个可用的 Go 1.23+，并清理旧 `GOROOT` 对构建的影响。
- 直接执行 `go build`，实际二进制默认放到 `$HOME/.local/libexec/lark-memory-cli/lark-cli`。
- 安装 `$HOME/.lark-cli-memory/bin/memoryctl`，并用它生成 `$HOME/.local/bin/lark-memory-cli` wrapper。
- 同步总路由 `lark-memory` 和六个命令选择器：`memory-list`、`memory-get`、
  `memory-graph-range`、`memory-graph-one-hop`、`memory-graph-search`、`memory-writing-style`；统一安装到
  `$HOME/.agents/skills`，不再同时写入 `$HOME/.codex/skills`。
- 归档旧的 `memory-graph-query` 选择器，避免升级后新旧名称同时展示。
- 如果目标 skill root 下还没有 `lark-shared`，会同步一份 `lark-shared` 作为
  `lark-memory` 的依赖。
- 向 `~/.zshrc` 写入 `$HOME/.local/bin` 到 `PATH`。
- 不改写已有 `lark-cli`，也不向 shell 写入 OpenAPI 域名覆盖。

注意：Codex/Agent 的 skill 列表通常在会话启动时加载。安装后命令可以立即使用；
如果希望新的 `memory-*` 入口出现在当前工具的 skill 列表里，请重启或新开一个
Codex/Agent 会话。

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

## 热插拔和状态

安装脚本会同时安装一个控制命令，专门用于对比「Agent 能看到 Memory CLI」和
「Agent 看不到 Memory CLI」两种实验状态：

```bash
$HOME/.lark-cli-memory/bin/memoryctl status
```

常用操作：

```bash
# 查看状态，JSON 适合脚本或 Agent 读取
$HOME/.lark-cli-memory/bin/memoryctl status --json

# 启用 wrapper 和所有受管 Memory skills
$HOME/.lark-cli-memory/bin/memoryctl enable

# 停用 wrapper 和 skill，保留源码、二进制、登录态和配置
$HOME/.lark-cli-memory/bin/memoryctl disable

# 只隐藏 skill，保留 lark-memory-cli 命令供手动测试
$HOME/.lark-cli-memory/bin/memoryctl disable --skills-only
```

状态含义：

- `enabled`：`lark-memory-cli` wrapper 在 `$HOME/.local/bin`，所有受管 Memory skills
  在 Codex/Agent 可扫描目录。
- `disabled`：wrapper 和所有受管 Memory skills 都被移到 `.disabled` 目录，Agent
  不会主动看到 Memory CLI。
- `skills_disabled`：wrapper 仍可用，但受管 Memory skills 已隐藏，适合人工保留命令
  但不让 Agent 自动发现能力。
- `partial`：active 和 `.disabled` 目录同时存在、部分受管 skill 状态不一致，或检测到旧版
  `$HOME/.codex/skills` 重复项，需要人工检查。

Codex/Agent 通常只在会话启动时扫描 skill，切换后请重启或新开会话。

## 升级

从不包含六个 `memory-*` 命令选择器的旧版本升级时，使用下面的一次性兼容命令。它会自动寻找
Go 1.23+、升级二进制，再用刚拉取的 `memoryctl refresh` 补齐所有受管 skills，同时保留当前
启用或停用状态；旧版遗留在 `$HOME/.codex/skills` 的 Memory 重复项会被无损迁移到
`$HOME/.codex/skills/.disabled/lark-memory-cli-duplicates`：

```bash
bash -lc 'set -o pipefail; curl --proto "=https" --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/arnold9672/cli/jhn_memory/scripts/memory-upgrade.sh | bash'
```

完成这次迁移后，后续日常升级可以继续直接执行：

```bash
lark-memory-cli --update
```

升级命令会复用安装脚本的目录约定：

- 源码目录：`${LARK_CLI_MEMORY_DIR:-$HOME/.lark-cli-memory}`
- 二进制：`${LARK_CLI_PREFIX:-$HOME/.local}/libexec/lark-memory-cli/lark-cli`
- wrapper：`${LARK_CLI_PREFIX:-$HOME/.local}/bin/lark-memory-cli`

它会拉取 `jhn_memory` 分支、重新构建二进制、刷新
`$HOME/.lark-cli-memory/bin/memoryctl`，并按源码中的 `skills/memory-managed-skills.txt` 清单同步
受管 skills：启用时更新启用路径，停用时更新 `.disabled` 路径，不会因为升级自动启用。
如果发现旧版曾同时写入 `$HOME/.codex/skills`，升级器会先归档对应重复项，避免 Codex 选择器展示两份。

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

应用和用户授权必须包含 `memory:hub`。如果授权缺失，先确认应用 scope 已经开通，
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
  --jq '{count: (.data.memories | length), memories: (.data.memories | map({memory_key, name, status, variants, default_variant_key}))}'
```

当前预期会看到的 memory 包括：

- `personal_memory_snapshot`
- `personalized_conclusion`

## 获取单个 Memory

`+get` 默认返回完整 payload。

读取前先根据 `memory +list` 的返回选择 variant：

- 如果 `variants` 中存在 `agentic_v1`，优先传 `--variant-key agentic_v1`。
- 如果不存在 `agentic_v1`，传 `default_variant_key`；如果该字段为空，可以不传 `--variant-key`。

```bash
lark-memory-cli memory +get --as user --memory-key personal_memory_snapshot --variant-key agentic_v1
```

输出 JSON：

```bash
lark-memory-cli memory +get --as user --memory-key personal_memory_snapshot --variant-key agentic_v1 --format json
```

不打印 payload 内容的安全检查命令：

```bash
lark-memory-cli memory +get --as user --memory-key personal_memory_snapshot --variant-key agentic_v1 --format json \
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

## 获取并应用个人写作风格

该命令固定读取以下 Memory：

- `memory_key=personal_memory_snapshot`
- `variant_key=agentic_v1_writing_pattn_v1`
- `payload_mode=full`
- 默认 `x-tt-env=ppe_memory_schema`
- 固定 `destination-idc=lf`

```bash
lark-memory-cli memory +writing-style --as user --format json
```

返回结果会把 `写作风格.md` 的 `fileName + items[{id,type,content,source}]` 解析为：

- `writing_style`：标题、章节、子章节、规则和可选写作场景；
- `references`：从每条规则来源汇总的文档、IM 和会议索引；
- `ai_guidance`：原始改写 prompt、单一模板选择、完整 block 读取、严格格式复刻及创建后回读验证的步骤与边界。

如需在其它泳道验证，可以仅对当前进程覆盖：

```bash
LARKSUITE_CLI_MEMORY_TT_ENV='<ppe_env>' \
  lark-memory-cli memory +writing-style --as user --format json
```

`status != ready`、payload 不是合法 JSON、缺少写作风格文件或层级结构非法时，命令返回结构化错误，
不会把不完整内容作为可用写作风格输出。

## 查询 Memory Graph 历史数据

仅在需要查询**当前登录用户**的历史 Memory Graph 节点或边时使用 `memory +graph-range`。该命令
只支持本人查询：请求体中的 `user_id` 会自动使用登录态的 `open_id`，不接受 UID、不会做
UID/open_id 转换，也不依赖通讯录，不能查询其他用户。

登录态没有有效 `open_id` 时，请先执行：

```bash
lark-memory-cli auth login --scope "memory:hub"
```

```bash
lark-memory-cli memory +graph-range --as user \
  --start-time-sec 1784476800 \
  --end-time-sec 1785081600 \
  --detail-format markdown \
  --format ndjson
```

时间范围是 Unix 秒的半开区间 `[start_time_sec, end_time_sec)`，必须满足
`0 < end-start <= 604800`，即最多七天（恰好七天有效）。CLI 从传入的精确起点开始，
拆成连续且每段最多 `86400` 秒的窗口，不会按自然日或本地时区对齐。最多七个窗口并发请求；
每个窗口遇到 API code `2200`、typed network error 或其他 retryable error 时，按固定
`200ms` 间隔最多调用三次。context cancel/deadline、auth、permission、validation 和
content safety 错误不重试。输出始终按时间顺序逐窗口写到 stdout，不会因后续窗口先完成而乱序。

Graph 流式输出支持 `json`、`ndjson` 和 `pretty`；`table`、`csv` 与 `--jq` 会被拒绝。
脚本和 Agent 推荐使用 `--format ndjson`，逐行消费完整 JSON。最早失败窗口重试耗尽后，
只保留它之前连续成功窗口的有效输出；后续窗口即使已经请求成功也不会渲染。整个命令仍然失败，
消费者必须检查进程退出状态，不能仅凭已有 stdout 判定全范围成功。

旧命令 `memory +graph-query` 已移除；调用方需要迁移到 `memory +graph-range`。

追加 `--dry-run` 会生成全部窗口请求计划，唯一硬保证是不会执行 Graph API 请求；命令仍会加载
身份和配置，并尝试 token/scope 预检，dry-run 成功不代表真实 Graph 请求一定可用。

## 查询 Memory Graph 一跳关系

OneHop 需要调用方已经知道一个或多个稳定查询起点的 NodeType 和 RootID：

```bash
lark-memory-cli memory +graph-one-hop --as user \
  --root '2:doc_123' \
  --root '3:meeting_123' \
  --lookback-days 7 \
  --node-type 2,3 \
  --relation-type meeting_discusses_doc \
  --detail-format markdown \
  --hop 1 \
  --trace-id trace-xxx \
  --format json
```

- NodeType 编号：IM_DAY=1、DOC_DAY=2、MEETING=3、USER=4、CALENDAR=5；当前目标过滤只支持 1、2、3。
- `--root` 和 `--hop` 必填；Root 格式是 `<node_type>:<root_id>`，可重复传入。
- USER（4）不能作为 Root 或 `--node-type`；CALENDAR（5）尚未支持作为 Root 或 `--node-type`；未传时不发送 `filters.node_types`。
- `--lookback-days` 默认 7，以执行时刻为右开边界；OneHop 不受 GraphQuery 单次 24 小时限制。
- CLI 每次只发送一个 OneHop 请求，不切窗、不自动循环；`--hop` 范围 1～10，仅记录当前探索层数。
- `--trace-id` 未传时自动生成，供 Agent 的多次手工探索串联；`--scene` 固定为 `graphcli`。
  这三个字段仅用于 CLI 输出元数据，不发送给 GraphHub；下行 `params` 只发送 `detailFormat`。
- `--relation-type` 可重复；未传时不做关系类型过滤。
- 输出支持 `json` 和 `pretty`；嵌套的节点、边和 meta 不支持 `table`、`csv` 或 `ndjson`。

CLI 会拒绝显式传入的 USER Root/过滤条件，并在响应中排除 USER。响应节点增加 `expandable`；节点和边增加
`expanded_from`，记录请求 Root、实际扩展的上游 NodeID 和关系边。多 Root 命中同一节点时保留
多个来源，不压成一个。输出 `meta` 同时记录结果数量、USER 过滤数量、Detail 字节数、耗时、
hop、trace ID 和 `log_id`。试用期不做静默截断；下游错误、413、超时或结构异常均整体失败。

## 内网自然语言 Graph Search

`memory +graph-search` 用自然语言 query 调用字节跳动内网 Knowledge QA 和 GraphQuery，生成可追溯
Graph Root。推荐 Skill 只执行第一跳，之后由 Agent 判断相关性和扩展价值，再决定是否继续：

最终合成使用“知识问答答案基线 + 节点 Detail + 边 Detail + 时间与来源”。Graph 没有有效增量时
直接保留基线；只有当前证据与 Query 明确相关、答案仍不完整，并且 `next_roots` 中存在可能补齐信息
的节点时才继续 OneHop。该判断由模型完成，不引入额外的 Checklist 或缺失项状态机。

安装后可在 Codex Skill 选择器中直接选择 `memory-graph-search`，并把选择项后的文本作为 query：

```text
@memory-graph-search 什么是知识问答
```

命令会验证当前 UAT，读取其 `open_id` 并自动转换为内部 UID，不再依赖本地 UID 环境变量。Skill
内部仍调用稳定的 `lark-memory-cli memory +graph-search --query ...` 接口。

```bash
lark-memory-cli memory +graph-search --as user \
  --query "什么是知识问答" \
  --concurrency 8 \
  --detail-format markdown \
  --graph-query-mode on \
  --graph-query-lookback-days 7 \
  --format json
```

- 仅内网可用。Knowledge QA 的 `TenantID=1`、`AppID=1234` 固定。
- Graph Search 的身份转换 FaaS、Knowledge QA FaaS、OneHop 与补充 GraphQuery 统一发送
  `x-tt-env: ppe_memory_hub`，与其它四个命令一致。`LARKSUITE_CLI_MEMORY_TT_ENV`
  可临时覆盖当前进程。
- 调用 Knowledge QA 前，命令先用当前 UAT 调用 `/open-apis/authen/v1/user_info` 获取 `open_id`，再通过
  `https://lgadymoe.fn.bytedance.net/knowledge_qa/out_id_to_in_id` 转换为内部 UID；Knowledge QA 使用同一
  FaaS 下的 `/knowledge_qa/search`。转换请求与 Graph
  请求使用相同 `x-tt-env`。不得把 UID、OpenID 或 UAT 写入输出或日志。
- Knowledge QA 只消费权限过滤后的 `passages`，不使用 `passages_ignore_filter`；候选 `content` 会保留
  在 `search_candidates` 中，供 Agent 作为最新事实基线。
- Doc URL Token 映射为 DOC_DAY；Wiki `node_token` 通过 get-node 转成 `obj_token` 后映射为
  DOC_DAY；Message URL 的内部 `chatId` 映射为 IM_DAY。
- Minutes 没有稳定 MEETING Root，写入 `skipped_candidates`，不进入 OneHop。
- 日期按 Asia/Shanghai 自然日生成。逻辑 Root 按 NodeType + RootID 合并，遍历任务再加
  graph_date 去重；节点和边分别按 node_id、edge_id 去重。
- `roots[].search_origins` 显式记录 NodeType、RootID 和日期的推导来源，禁止用 `source_id` 猜 Root。
- 第一跳按日期分组并发，默认 `--concurrency=8`。首版不限制 Root、节点或关系数量。
- `data.evidence` 为点和边建立非重复 Detail 索引，记录 Detail 路径/字节数、关系端点、自环和
  `edge_groups`；原始 Detail 仍只保留在 `nodes` / `edges` 中。每跳输出新增点边及 Detail 数量；
  没有新增图证据时提前停止。
- Skill 使用 `--graph-query-mode=on`，所有 Query 都尝试当前用户时间窗 Graph；明确时间意图使用精确
  时间窗，否则使用 1～7 天滚动窗口。GraphQuery 点边按 NodeID/EdgeID 合并，可靠 Root 写入
  `next_roots`，但不会自动扩展。补充窗口失败不丢弃主链路结果。
- Knowledge QA 返回后立即开始 Wiki/OneHop，不等待独立 GraphQuery；最终合并仍等待两路结束。
  FaaS、GraphQuery、Wiki 和 OneHop 共用 `--concurrency` 全局请求上限。
- Knowledge QA 或 Graph 请求前严格解析并刷新 UAT，并通过 `user_info` 在服务端验证；认证或身份转换失败
  时不执行检索。`graph-search` 固定只执行初始 OneHop，不提供跳数参数；Agent 根据 Query 相关性、
  信息增量和扩展价值决定是否调用 `graph-one-hop --hop`，10 跳为安全上限。
  同一 Wiki `node_token` 在单次命令内只解析一次。
- 内网试用期不做硬截断或静默裁剪 Detail；未被选择为下一跳的证据仍保留。`evidence` /
  `edge_groups` 只做归因和重复感知。
- 单组失败不会取消其它分组；成功分支仍会返回给 Agent 判断。部分结果设置 `meta.complete=false` 并返回
  `failed_batches`；所有 OneHop 分组均失败时命令整体失败。
- `expanded_from` 保留直接上游，`search_origins` 保留最初 Knowledge QA passage，
  `first_seen_hop` 标记首次出现层级。
- Agent 必须同时解析支持结论的节点 Detail 和边 Detail，并在候选基线与 Graph 增量间做事实级
  去重、时效排序和冲突检查；关系类型、拓扑或边数量本身不能证明语义价值。
- 最终回答必须保留知识问答基线中的相关人名、数字、时间、任务清单、状态与结论。Graph 仅用于
  补充、修正、解释或重要佐证；没有有效增量时直接返回基线，不为了体现 Graph 而增加重复内容。
- Knowledge QA 不自动重试；OneHop 仅对临时网络错误或明确可重试错误最多重试三次。
- `--dry-run` 使用 OpenID/UID 占位符，展示 user_info、ID 转换、Knowledge QA 与动态后续步骤。

## 排障

确认 `lark-memory-cli` wrapper 指向独立的 memory binary，且没有强制覆盖 OpenAPI 域名：

```bash
head -n 5 "$(command -v lark-memory-cli)"
```

期望能看到：

```text
LARKSUITE_CLI_REMOTE_META="${LARKSUITE_CLI_REMOTE_META:-off}"
```

默认请求走线上 OpenAPI 域名。通用 Memory/Graph/FaaS 命令默认发送
`x-tt-env: ppe_memory_hub`，`+writing-style` 默认发送 `x-tt-env: ppe_memory_schema`。
如需覆盖，可以显式设置：

```bash
export LARKSUITE_CLI_OPEN_BASE_URL="https://open.feishu-pre.cn"
export LARKSUITE_CLI_MEMORY_TT_ENV="ppe_memory_hub"
```

等价的原始请求形态：

```bash
curl -i -X GET 'https://open.feishu.cn/open-apis/search/v2/memory_hub/list_memory' \
  -H "Authorization: Bearer <USER_ACCESS_TOKEN>" \
  -H 'x-tt-env: ppe_memory_hub' \
  -H 'Content-Type: application/json' \
  --data '{}'
```
