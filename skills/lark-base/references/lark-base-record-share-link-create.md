# base +record-share-link-create

> **前置条件：** 先阅读 [`../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

为单条记录生成分享链接。

## 推荐命令

```bash
lark-cli base +record-share-link-create \
  --base-token xxx \
  --table-id tbl_xxx \
  --record-id rec_xxx
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--base-token <token>` | 是 | Base Token |
| `--table-id <id>` | 是 | 表 ID |
| `--record-id <id>` | 是 | 记录 ID |

## API 入参详情

**HTTP 方法和路径：**

```http
POST /open-apis/base/v3/bases/:base_token/tables/:table_id/records/:record_id/share_links
```

## 返回重点

- 成功时直接返回接口 `data` 字段内容，包含该记录的分享链接信息。结构如下:
```json
{
  "record_share_link": "https://example.feishu.cn/record/TW2wrdbkoeoYXYcwvyIczJ2ZnFb"
}
```
- 若记录ID无权限/不存在，则会返回错误信息 `records do not exist or no read permission`

## 参考

- [lark-base-record.md](lark-base-record.md) — record 索引页
- [lark-base-record-share-link-batch-create.md](lark-base-record-share-link-batch-create.md) — 批量生成分享链接
