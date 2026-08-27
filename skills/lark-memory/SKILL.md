---
name: lark-memory
version: 0.1.3
description: "Use when users need Memory Hub long-term context, preferences, history, project background, or explicit Memory Graph queries."
metadata:
  requires:
    bins: ["lark-memory-cli"]
  cliHelp: "lark-memory-cli memory --help"
---

# Memory Hub

**CRITICAL — 开始前 MUST 先用 Read 工具读取 [`../lark-shared/SKILL.md`](../lark-shared/SKILL.md)，其中包含认证、权限处理。**

## 何时使用

使用本 skill：

- 用户提到 memory、记忆、偏好、长期上下文、历史决策、项目背景。
- 当前任务缺少用户长期偏好、团队约定或历史背景，且这些信息不应从实时系统推断。
- 需要先发现有哪些可读 memory，再按最小必要原则读取其中一个。

不要使用本 skill：

- 查询最新事实、代码当前状态、线上事故、实时配置、价格、排期或权限状态。
- 替代代码搜索、日志、文档、接口测试或真实系统返回。
- 批量倾倒所有 memory 内容，或把 memory 原文写入仓库、日志、临时文档。

## 命令路由

- 查询可见 memory 或读取 memory 快照、偏好、长期上下文：使用 `+list` / `+get`。
- 仅当用户明确要求查询历史 Memory Graph 节点或边时使用 `+graph-query`；不得用
  `+list` / `+get` 替代 Graph 查询。

## List/Get 流程

1. 先列出当前用户可见的 memory：

```bash
lark-memory-cli memory +list --as user --format json
```

2. 从返回的 `memories[].memory_key` 中选择最小必要项，并按 `variants` 选择读取版本：

   - 如果 `variants` 中存在 `agentic_v1`，优先读取 `agentic_v1`。
   - 如果不存在 `agentic_v1`，读取 `default_variant_key`；如果该字段为空，再不传 `--variant-key` 走服务端默认值。

3. 读取单个 memory：

```bash
lark-memory-cli memory +get --memory-key <memory_key> --variant-key <variant_key> --as user --format json
```

本 fork 中 `+get` 的 `--payload-mode` 默认是 `full`。如只需要元信息或摘要，可以显式传：

```bash
lark-memory-cli memory +get --memory-key <memory_key> --payload-mode summary --as user --format json
lark-memory-cli memory +get --memory-key <memory_key> --payload-mode metadata --as user --format json
```

4. 只使用与当前任务相关的字段；最终回答里不要无关复述 memory 内容。

## 命令

| 目标 | 命令 |
|---|---|
| 列出可见 memory | `lark-memory-cli memory +list --as user --format json` |
| 获取 memory 详情 | `lark-memory-cli memory +get --memory-key <key> --as user --format json` |
| 获取指定 variant | `lark-memory-cli memory +get --memory-key <key> --variant-key <variant> --as user --format json` |
| 查询历史 Graph | `lark-memory-cli memory +graph-query --start-time-sec <start> --end-time-sec <end> --as user --format ndjson` |
| 调试请求结构 | 在上述命令后追加 `--dry-run` |

## 权限与身份

- 只使用 user 身份：`--as user`。
- 当前 scope：`memory:hub`。
- 如果报 scope 或登录问题，按 `lark-shared` 的用户授权恢复流程处理；不要切到 bot。

## 输出使用规则

- `+list` 重点读取：`memories[].memory_key`、`name`、`variants`、`default_variant_key`、`status`、`description`、`showcase`。
- 读取 variant 优先级：`agentic_v1` > `default_variant_key` > 服务端默认值；这是 Agent 使用规则，CLI 不会在未传 `--variant-key` 时自动改写。
- `+get` 重点读取：`memory_key`、`variant_key`、`status`、`payload_type`、`payload`、`metadata`。
- `payload` 可能是 JSON 字符串，具体结构由 `payload_type` 决定；不要在 CLI 层假设统一 schema。
- `status != ready` 时，不要把内容当作完整事实使用，应向用户说明 memory 尚未就绪或读取失败。

## Graph 查询契约

- 只查询当前登录用户。“我的 Graph”直接使用当前登录身份；请求体中的 `user_id` 自动使用登录态
  `open_id`。不接受 UID，不做 UID/open_id 转换，也不依赖通讯录；不得查询或尝试推断其他用户。
- 登录态缺少有效 `open_id` 时，提示用户执行
  `lark-memory-cli auth login --scope "memory:hub"`，不要向用户索取 `user_id`。
- 时间范围是 Unix 秒半开区间 `[start_time_sec, end_time_sec)`，须满足
  `0 < end-start <= 604800`。超过七天时请用户缩小范围，不要自行截断。
- CLI 从精确起点拆成连续、最多 `86400` 秒的窗口，最多七个窗口并发请求。每个窗口对
  API code `2200`、任何 typed network 错误（即使 `Retryable=false`）或其他 retryable 错误按固定
  `200ms` 间隔最多调用三次；context cancel/deadline、auth、permission、validation 与
  content safety 错误即使标记为 `Retryable` 也不重试。输出仍严格按时间顺序返回。追加 `--dry-run` 会生成全部窗口
  请求计划，唯一硬保证是不会调用 Graph API。命令仍会加载身份/配置，并尝试执行 token/scope 预检；
  预检可能刷新凭证或访问非 Graph 认证端点。dry-run 成功不证明 user token、所需 scope 或真实 Graph API 调用可用。
- 输出仅支持 `json`、`ndjson`、`pretty`；`table`、`csv` 和 `--jq` 不适用于 Graph 流式输出。
- Agent 优先使用 `--format ndjson`，只消费完整行，并始终检查进程退出状态。最早失败窗口会使命令整体
  以非零状态退出；已输出行只是它之前连续成功的有效但不完整前缀，更晚的窗口即使已请求成功也绝不渲染。
  必须明确标注“部分/不完整”，不得声称全范围成功。

## 安全规则

- 最小读取：读取快照时先 list，再 get 一个最相关 memory；明确的 Graph 查询直接按上述契约执行。
- 最小披露：只在答案里使用必要结论，不展开无关原文。
- 不写入：除非用户明确要求，不把 memory 或 Graph 详情保存到代码、文档、日志或长期存储。
- Graph 详情不得批量复述，只披露完成当前任务所必需的信息。
- 不当实时真相：memory 只代表历史上下文，不能覆盖当前代码、文档、日志、接口和用户最新指令。
