
# sheets +update-float-image（更新浮动图片）

> **前置条件：** 先阅读 [`../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

本 skill 对应 shortcut：`lark-cli sheets +update-float-image`。

更新浮动图片的位置、大小和偏移量。

> [!CAUTION]
> 这是**写入操作** —— 执行前必须确认用户意图。可以先用 `--dry-run` 预览。

## 命令

```bash
lark-cli sheets +update-float-image --url "https://example.larksuite.com/sheets/shtxxxxxxxx" \
  --sheet-id "<sheetId>" --float-image-id "fi12345678" \
  --width 400 --height 300 --offset-y 20
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--url` | 否 | 电子表格 URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 否 | 表格 token |
| `--sheet-id` | 是 | 工作表 ID |
| `--float-image-id` | 是 | 浮动图片 ID |
| `--range` | 否 | 新锚定单元格 |
| `--width` | 否 | 宽度（像素，>=20） |
| `--height` | 否 | 高度（像素，>=20） |
| `--offset-x` | 否 | 横向偏移（像素，>=0） |
| `--offset-y` | 否 | 纵向偏移（像素，>=0） |
| `--dry-run` | 否 | 仅打印参数，不执行请求 |

## 输出

JSON，包含更新后的 `float_image` 对象。

## 参考

- [lark-sheets-create-float-image](lark-sheets-create-float-image.md)
- [lark-sheets-get-float-image](lark-sheets-get-float-image.md)
