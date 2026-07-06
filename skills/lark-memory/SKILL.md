---
name: lark-memory
version: 0.1.0
description: "Memory Hub：读取用户可见的长期记忆、偏好、历史上下文和项目背景。仅用于补充长期上下文，不用于实时事实。"
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

## 推荐流程

1. 先列出当前用户可见的 memory：

```bash
lark-memory-cli memory +list --as user --format json
```

2. 从返回的 `memories[].memory_key` 中选择最小必要项。

3. 读取单个 memory：

```bash
lark-memory-cli memory +get --memory-key <memory_key> --as user --format json
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
| 调试请求结构 | 在上述命令后追加 `--dry-run` |

## 权限与身份

- 只使用 user 身份：`--as user`。
- 当前 scope：`search:message`。
- 如果报 scope 或登录问题，按 `lark-shared` 的用户授权恢复流程处理；不要切到 bot。

## 输出使用规则

- `+list` 重点读取：`memories[].memory_key`、`name`、`default_variant_key`、`status`、`description`、`showcase`。
- `+get` 重点读取：`memory_key`、`variant_key`、`status`、`payload_type`、`payload`、`metadata`。
- `payload` 可能是 JSON 字符串，具体结构由 `payload_type` 决定；不要在 CLI 层假设统一 schema。
- `status != ready` 时，不要把内容当作完整事实使用，应向用户说明 memory 尚未就绪或读取失败。

## 安全规则

- 最小读取：先 list，再 get 一个最相关 memory。
- 最小披露：只在答案里使用必要结论，不展开无关原文。
- 不写入：除非用户明确要求，不把 memory 内容保存到代码、文档、日志或长期存储。
- 不当实时真相：memory 只代表历史上下文，不能覆盖当前代码、文档、日志、接口和用户最新指令。
