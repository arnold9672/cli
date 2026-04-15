
# sheets +list-float-images（查询浮动图片）

> **前置条件：** 先阅读 [`../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

本 skill 对应 shortcut：`lark-cli sheets +list-float-images`。

查询工作表中的所有浮动图片。

## 命令

```bash
lark-cli sheets +list-float-images --url "https://example.larksuite.com/sheets/shtxxxxxxxx" \
  --sheet-id "<sheetId>"
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--url` | 否 | 电子表格 URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 否 | 表格 token |
| `--sheet-id` | 是 | 工作表 ID |
| `--dry-run` | 否 | 仅打印参数，不执行请求 |

## 输出

JSON，包含 `items` 数组，每项为一个 float_image 对象。

## 参考

- [lark-sheets-get-float-image](lark-sheets-get-float-image.md)
- [lark-sheets-create-float-image](lark-sheets-create-float-image.md)
