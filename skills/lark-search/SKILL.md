---
name: lark-search
version: 1.0.0
description: "飞书搜索：搜索。"
metadata:
  requires:
    bins: ["lark-cli"]
  cliHelp: "lark-cli search --help"
---

# search (v2)

**CRITICAL — 开始前 MUST 先用 Read 工具读取 [`../lark-shared/SKILL.md`](../lark-shared/SKILL.md)，其中包含认证、权限处理**

## API Resources

```bash
lark-cli schema search.<resource>.<method>   # 调用 API 前必须先查看参数结构
lark-cli search <resource> <method> [flags] # 调用 API
```

> **重要**：使用原生 API 时，必须先运行 `schema` 查看 `--data` / `--params` 参数结构，不要猜测字段格式。

### memory_hub

  - `get_memory` — 获取 Memory 详情
  - `list_memory` — 获取 Memory Hub 记忆列表

## 权限表

| 方法 | 所需 scope |
|------|-----------|
| `memory_hub.get_memory` | `memory:hub` |
| `memory_hub.list_memory` | `memory:hub` |
