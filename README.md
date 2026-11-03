# lark-memory-cli

`lark-memory-cli` 是这个 fork 中面向 Memory Hub 的专用命令入口，目前只聚焦六个
shortcuts：

- `lark-memory-cli memory +list`
- `lark-memory-cli memory +get`
- `lark-memory-cli memory +graph-range`
- `lark-memory-cli memory +graph-one-hop`
- `lark-memory-cli memory +graph-search`
- `lark-memory-cli memory +writing-style`

这六个命令使用用户身份调用，需要 `memory:hub` scope，默认调用线上 OpenAPI 域名。
`+list`、`+get`、`+graph-range`、`+graph-one-hop` 默认不发送 `x-tt-env`，由网关按默认路由处理。
`+graph-search` 仅 OpenID 转换和知识问答 FaaS 默认使用 `ppe_memory_hub`，内部 GraphQuery/OneHop
不带泳道头；`+writing-style` 仍默认使用 `ppe_memory_schema`。非空 `LARKSUITE_CLI_MEMORY_TT_ENV`
可覆盖四个直接命令和 writing-style 的泳道，但对 Graph Search 只覆盖前置 FaaS，不会传入内部
Graph 数据请求。安装脚本不会覆盖已有 `lark-cli` 命令。

更完整的安装与排障说明见 [MEMORY_SHORTCUTS.md](./MEMORY_SHORTCUTS.md)。

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

```bash
export LARKSUITE_CLI_CONFIG_DIR="$HOME/.config/lark-memory-cli"
```

安装脚本会把总路由 `lark-memory` 和六个命令选择器 `memory-list`、`memory-get`、
`memory-graph-range`、`memory-graph-one-hop`、`memory-graph-search`、`memory-writing-style` 同步到
`$HOME/.agents/skills`。Codex 和其它兼容 Agent 都从这个共享目录发现这些便携 skills，避免同时
写入 `$HOME/.codex/skills` 后在选择器中重复展示。
升级时会归档旧的 `memory-graph-query` 选择器，避免新旧名称同时展示。
如果目标目录缺少 `lark-shared`，会补一份作为依赖。Codex/Agent 的 skill 列表通常在会话
启动时加载；安装后命令立即可用，但新 skill 要出现在选择器里，需要重启或新开会话。

## 热插拔和状态

安装脚本会同时安装一个控制命令：

```bash
$HOME/.lark-cli-memory/bin/memoryctl status
```

常用操作：

```bash
# 查看当前是否启用，JSON 适合脚本或 Agent 读取
$HOME/.lark-cli-memory/bin/memoryctl status --json

# 启用 lark-memory-cli wrapper 和所有受管 Memory skills
$HOME/.lark-cli-memory/bin/memoryctl enable

# 停用 wrapper 和 skill，但保留源码、二进制、登录态和配置
$HOME/.lark-cli-memory/bin/memoryctl disable

# 只隐藏 skill，保留 lark-memory-cli 命令供手动测试
$HOME/.lark-cli-memory/bin/memoryctl disable --skills-only
```

`disable` 会把 wrapper 移到 `$HOME/.local/bin/.disabled/lark-memory-cli`，并把
所有受管 Memory skills 移到 `$HOME/.agents/skills/.disabled`。重新 `enable` 会原路恢复。
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

这个命令会拉取安装目录中的 `jhn_memory` 分支、重新构建
`$HOME/.local/libexec/lark-memory-cli/lark-cli`、刷新
`$HOME/.lark-cli-memory/bin/memoryctl`，并按源码中的 `skills/memory-managed-skills.txt` 清单同步
受管 skills：启用时更新启用路径，停用时更新 `.disabled` 路径，不会因为升级自动启用。
如果发现旧版曾同时写入 `$HOME/.codex/skills`，升级器会先归档对应重复项，避免 Codex 选择器展示两份。

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

应用和用户授权必须包含 `memory:hub`。如果授权缺失，先确认应用 scope 已开通，
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

当前预期会看到：

- `personal_memory_snapshot`
- `personalized_conclusion`

## 获取单个 Memory

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

在 Codex 输入框中，Skill 名称后可直接跟任意写作请求。例如：

```text
$memory-writing-style 帮忙总结下过去两个月的工作
```

Skill 会按请求寻找该时间范围内的工作事实，再用 Memory 调整总结的组织与措辞；不会因为使用
Skill 就自动创建飞书文档。若 query 要求创建或修改文档，Skill 会把风格实际用于该文档，并按
用户要求新建或原位修改。

`memory +writing-style` 固定读取 `personal_memory_snapshot` 的
`agentic_v1_writing_pattn_v1` variant，将 `写作风格.md` 的 `h1/h2/h3/text` item 流解析为章节、
子章节与规则，同时返回来源索引和 AI 使用指引：

```bash
lark-memory-cli memory +writing-style --as user --format json
```

该命令默认发送 `x-tt-env: ppe_memory_schema` 和 `destination-idc: lf`。如需在其它泳道验证，
可仅对当前进程覆盖 `x-tt-env`：

```bash
LARKSUITE_CLI_MEMORY_TT_ENV='<ppe_env>' \
  lark-memory-cli memory +writing-style --as user --format json
```

Agent 应先完成用户请求所需的事实检索，再应用“跨场景稳定特征”并选择匹配场景；创建或修改
文档时，要把风格用于文档的信息组织、格式和措辞。需要同类型参考文档作为格式模板时，只选择
最相关的一份；若 Memory 提供相关的同类型文档，必须实际读取完整代表性区块并确认 block 结构，
不能只依据 Memory 摘要推断格式。操作后回读文档，修正事实、风格与
结构偏差。Memory 不能替代当前事实或用户最新指令。

## 查询 Memory Graph 历史数据

`memory +graph-range` 仅支持查询当前登录用户的历史 Memory Graph 节点或边，请求体中的
`user_id` 会自动使用登录态的 `open_id`。

```bash
lark-memory-cli memory +graph-range --as user \
  --start-time-sec 1784476800 \
  --end-time-sec 1785081600 \
  --format ndjson
```

时间范围是 Unix 秒的半开区间 `[start_time_sec, end_time_sec)`，必须满足
`0 < end-start <= 604800`，即最多七天（恰好七天有效）。

参数支持：

- `--start-time-sec`：必填，包含边界的 Unix 秒起始时间。
- `--end-time-sec`：必填，不包含边界的 Unix 秒结束时间。
- `--format`：输出格式，支持 `json`、`ndjson` 和 `pretty`；不支持 `table`、`csv` 和 `--jq`。

旧命令 `memory +graph-query` 已移除；调用方需要迁移到 `memory +graph-range`。

三个 Graph 命令统一使用最新详情协议：`detail.markdown` 是正文，`detail.source[]` 是来源。
有来源不等于有正文；pretty 输出会区分 `source_only` 与 `missing`。旧详情字段不再作为正文回退。
`--format` 仅控制 CLI 输出；已移除 `--detail-format`、`--use-v2`、`--graph-query-use-v2`，
请求不发送格式或版本覆盖，后端数据版本由服务端默认策略决定。

## 查询 Memory Graph 一跳关系

已知稳定业务实体的 NodeType 和 RootID 后，使用 `memory +graph-one-hop` 查询该实体在滚动时间窗内
的时间线节点，以及这些节点通过入边或出边直接关联的一跳节点：

```bash
lark-memory-cli memory +graph-one-hop --as user \
  --root '2:doc_123' \
  --root '3:meeting_123' \
  --lookback-days 7 \
  --node-type 2,3 \
  --relation-type meeting_discusses_doc \
  --hop 1 \
  --trace-id trace-xxx \
  --format json
```

`--root` 和 `--hop` 必填。`--root` 可重复，格式为 `<node_type>:<root_id>`；节点类型编号为
IM_DAY=1、DOC_DAY=2、MEETING=3、USER=4、CALENDAR=5。当前目标过滤只支持 1、2、3；未传
`--node-type` 时不发送 `filters.node_types`。USER
不能作为 Root 或目标过滤类型，CALENDAR（5）尚未支持作为 Root 或目标过滤，CLI 还会从响应中删除
USER 节点及其关联边。

`--lookback-days` 默认 7，CLI 以执行时刻为右开边界构造完整滚动时间窗；一次命令只发送一次
OneHop 请求，不做 24 小时切片，也不自动继续下一跳。`--hop` 只记录 Agent 当前探索层数，范围
1～10；未传 `--trace-id` 时自动生成。`--hop`、`--trace-id` 和 `--scene` 仅作为 CLI 输出元数据，
不发送给 GraphHub。请求不携带详情格式或数据版本选项。输出支持 `json` 和 `pretty`。

JSON 输出的 `data.nodes` 和 `data.edges` 保留下游详情，并补充 `expanded_from` 来源信息；节点还会
包含 `expandable`。`meta` 包含 Root、节点、边、USER 过滤、Detail 字节数、耗时、hop、trace ID
和可用的 OpenAPI `log_id`。CLI 不截断 OneHop 结果；下游失败或响应结构不完整时返回结构化错误。

## 内网自然语言 Graph Search

`memory +graph-search` 通过 Knowledge QA 和 GraphQuery 生成可追溯的 Root 候选。推荐的 Skill
只执行第一跳 OneHop，返回 `next_roots` 后由 Agent 阅读节点和边的 Detail、判断与 Query 的相关性，
再决定是否调用下一次原子 OneHop。Minutes 只保留为候选，不进入 OneHop。

最终合成时，Agent 会把已有知识问答答案作为基线，并同时读取节点 Detail、边 Detail、时间和来源。
Graph 没有提供有效增量时原样保留基线；只有当前证据明确相关、答案仍不完整且存在可能补齐信息的
`next_root` 时才继续 OneHop，不要求额外维护 Query Checklist 或缺失项状态机。

面向 Codex 的推荐入口是从 Skill 选择器直接选择 `memory-graph-search`，然后输入 query，例如：

```text
@memory-graph-search 什么是知识问答
```

Codex 会把选择项后的文本作为 query，并在内部执行下面的 CLI 命令。命令会验证当前 UAT，读取其
`open_id` 并自动转换为内部 UID，不再需要本地 UID 环境变量：

```bash
lark-memory-cli memory +graph-search --as user \
  --query "什么是知识问答" \
  --concurrency 8 \
  --graph-query-mode on \
  --graph-query-lookback-days 7 \
  --format json
```

- 该命令仅在字节跳动内网可用；`TenantID=1`、`AppID=1234` 固定。
- Graph Search 仅身份转换 FaaS 与 Knowledge QA FaaS 默认发送 `x-tt-env: ppe_memory_hub`；
  `LARKSUITE_CLI_MEMORY_TT_ENV` 对此命令只覆盖这两类请求。`user_info`、Wiki 解析、内部
  GraphQuery/OneHop 不发送泳道头。基于 Search 结果调用独立 `+graph-one-hop` 续跳时，
  需为该次命令清除该环境变量，避免继承前置 FaaS 的泳道。
- 调用 Knowledge QA 前，命令先用当前 UAT 调用 `/open-apis/authen/v1/user_info` 获取 `open_id`，再通过
  `https://f3s0wk4f.fn.bytedance.net/knowledge_qa/out_id_to_in_id` 转换为内部 UID；Knowledge QA 使用同一
  FaaS 下的 `/knowledge_qa/search`。转换和问答使用前置 FaaS 泳道，Graph 数据请求不指定泳道；
  不存在本地 UID 覆盖入口。
- `search_candidates` 只来自权限过滤后的 `passages`，并保留候选 `content` 作为最新事实基线；
  `passages_ignore_filter` 永远不会进入输出或成为 Graph 起点。
- Wiki 节点会使用 `wiki:node:retrieve` 解析底层 `obj_token`；单个 Wiki 解析失败只跳过该候选。
- 第一跳按 Asia/Shanghai 日期分组并发请求；默认并发度 8，结果规模不截断。
- `roots[].search_origins` 记录 NodeType、RootID 和日期的推导来源；不得用 `source_id` 猜下一跳 Root。
- `data.evidence` 索引节点和边的 Detail 路径、缺失情况、端点完整性和重复关系分组；每跳同时输出
  `new_node_count`、`new_edge_count` 及对应 Detail 数量。若一跳没有新增点或边，将提前停止。
- Skill 使用 `--graph-query-mode=on`，让每个 Query 都并行尝试用户 Graph；有明确时间意图时使用
  精确自然时间窗，否则使用配置的 1～7 天滚动窗口。超过 7 天不静默截断，而是跳过补充并说明原因。
- GraphQuery 与 OneHop 均不指定泳道，返回点边按 NodeID/EdgeID 合并。带可靠 NodeType、RootID 和日期的
  GraphQuery 节点写入 `next_roots`，但不会自动进入 OneHop；Agent 判断后才能继续。`retrieved_via`
  和 evidence `retrieval_class` 标记 `one_hop_only`、`graph_query_only`、`corroborated`。
- Knowledge QA 返回后立即开始 Wiki/OneHop，不等待独立 GraphQuery；最终答案仍等待两路汇合。
  `--concurrency` 是 FaaS、GraphQuery、Wiki 和 OneHop 共用的全局请求上限，不会因流水并行而翻倍。
- 发起 Knowledge QA 或 Graph 请求前严格解析并刷新 UAT，并通过 `user_info` 在服务端验证；缺失、过期、
  刷新失败或身份转换失败时不执行检索。`graph-search` 固定只执行初始 OneHop，不提供跳数参数；Agent
  每次继续前重新判断，并通过 `graph-one-hop --hop` 显式执行后续跳，10 跳为安全上限。
- 同一命令内相同 Wiki `node_token` 只调用一次 get-node，重复候选复用解析结果，候选来源仍完整保留。
- 内网试用期不做硬截断或静默裁剪 Detail；权限内候选及全部返回点边 Detail 保持可用。选择下一跳
  Root 只决定后续请求，不删除未选择证据；`evidence` / `edge_groups` 只做归因和重复感知。
- 单个 OneHop 分组最终失败时，其它成功分组继续扩展；输出用 `meta.complete=false` 和
  `data.failed_batches` 标记部分成功。所有 OneHop 分组均失败时命令才整体失败。
- Skill 将完整知识问答答案及其引用作为基线，与全部节点、边 Detail 和路径一起交给最终合成；Graph
  只用于补充、修正、解释或重要佐证。不得因 Graph 局部信息丢失基线中的人名、数字、时间、任务清单
  或状态；没有有效增量时直接返回基线答案。
- `--dry-run` 展示脱敏的 user_info、ID 转换、Knowledge QA 请求和动态阶段说明，不执行任何远端调用。

## 排障

确认 wrapper 指向独立的 memory binary，且没有强制覆盖 OpenAPI 域名：

```bash
head -n 5 "$(command -v lark-memory-cli)"
```

期望能看到：

```text
LARKSUITE_CLI_REMOTE_META="${LARKSUITE_CLI_REMOTE_META:-off}"
```

默认请求走线上 OpenAPI 域名。`+list`、`+get`、`+graph-range`、`+graph-one-hop` 默认不发送
`x-tt-env`。`+graph-search` 仅身份转换、问答 FaaS 默认使用 `ppe_memory_hub`，内部 Graph 请求始终
不继承泳道。`+writing-style` 默认使用 `ppe_memory_schema`。入口域名与泳道是独立配置；需要覆盖时才显式设置：

```bash
export LARKSUITE_CLI_OPEN_BASE_URL="https://open.feishu-pre.cn"
export LARKSUITE_CLI_MEMORY_TT_ENV="ppe_memory_hub"
```
