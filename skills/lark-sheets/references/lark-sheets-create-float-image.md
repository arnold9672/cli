
# sheets +create-float-image（创建浮动图片）

> **前置条件：** 先阅读 [`../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

本 skill 对应 shortcut：`lark-cli sheets +create-float-image`。

在工作表中创建浮动图片。

> [!CAUTION]
> 这是**写入操作** —— 执行前必须确认用户意图。可以先用 `--dry-run` 预览。

## 前置步骤：获取 float_image_token

`--float-image-token` 必须通过素材上传接口（`drive/v1/medias/upload_all`）获取。该接口目前未封装为 shortcut，需通过 `lark-cli api` 裸调：

```bash
# 1. 上传素材（parent_type 必须为 sheet_image）
SIZE=$(stat -f%z ./image.png 2>/dev/null || stat -c%s ./image.png)
lark-cli api POST /open-apis/drive/v1/medias/upload_all \
  --file "file=./image.png" \
  --data "{\"file_name\":\"image.png\",\"parent_type\":\"sheet_image\",\"parent_node\":\"<spreadsheetToken>\",\"size\":$SIZE}"
# 响应: {"data":{"file_token":"boxcnXXXX"}}

# 2. 用返回的 file_token 作为 --float-image-token
lark-cli sheets +create-float-image --url "<url>" --sheet-id "<sheetId>" \
  --float-image-token "boxcnXXXX" --range "<sheetId>!A1:A1"
```

> **常见错误**：
> - `parent_type` 不是 `sheet_image` → 报 `params error`
> - 用 `drive +upload` 的 token → 报 `Wrong Float Image Token`（两者是不同的上传接口）

## 命令

```bash
lark-cli sheets +create-float-image --url "https://example.larksuite.com/sheets/shtxxxxxxxx" \
  --sheet-id "<sheetId>" --float-image-token "boxcnXXXX" \
  --range "<sheetId>!A1:A1" --width 200 --height 150

# 指定自定义 ID 和偏移
lark-cli sheets +create-float-image --spreadsheet-token "shtxxxxxxxx" \
  --sheet-id "<sheetId>" --float-image-token "boxcnXXXX" \
  --range "<sheetId>!B2:B2" --width 300 --height 200 \
  --offset-x 10 --offset-y 20 --float-image-id "myImg12345"
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--url` | 否 | 电子表格 URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 否 | 表格 token |
| `--sheet-id` | 是 | 工作表 ID |
| `--float-image-token` | 是 | 图片 token（通过上方「前置步骤」的素材上传接口获取，不能用 `drive +upload` 的 token） |
| `--range` | 是 | 锚定单元格（如 `sheetId!A1:A1`） |
| `--width` | 否 | 宽度（像素，>=20） |
| `--height` | 否 | 高度（像素，>=20） |
| `--offset-x` | 否 | 横向偏移（像素，>=0） |
| `--offset-y` | 否 | 纵向偏移（像素，>=0） |
| `--float-image-id` | 否 | 自定义 10 位字母数字 ID（不传则自动生成） |
| `--dry-run` | 否 | 仅打印参数，不执行请求 |

## 输出

JSON，包含 `float_image`（float_image_id, float_image_token, range, width, height, offset_x, offset_y）。

## 参考

- [lark-sheets-update-float-image](lark-sheets-update-float-image.md)
- [lark-sheets-get-float-image](lark-sheets-get-float-image.md)
- [lark-sheets-list-float-images](lark-sheets-list-float-images.md)
- [lark-sheets-delete-float-image](lark-sheets-delete-float-image.md)
