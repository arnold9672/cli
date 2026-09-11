# Memory Graph Query 项目说明

## 1. 背景

Memory Hub 通过以下 OpenAPI 提供 Graph 历史数据查询：

```text
POST /open-apis/search/v2/memory_hub/graph_query
```

下游接口是单次请求、单次响应的 unary API，单次时间范围最多为 86400 秒。CLI 需要在不修改
服务端协议的前提下支持最长七天查询，将大时间范围拆成每天一个小请求，并以流式方式尽快返回
已完成的数据。

本项目在 `jhn_memory` 分支已有的 Memory shortcut、用户身份、`memory:hub` scope、PPE header、
结构化错误和内容安全能力之上新增 `memory +graph-query`。

## 2. 最终目标

- 支持查询大于一天且不超过七天的 Memory Graph 历史数据。
- 从用户传入的精确起点开始，拆成连续、无重叠、无空洞的单日窗口。
- 最多并发执行七个窗口，降低多日查询总耗时。
- 无论窗口完成顺序如何，始终按时间顺序流式返回连续成功前缀。
- 每个小窗口独立执行固定次数重试，避免单日瞬时失败导致整段查询立即失败。
- 最早失败窗口重试耗尽后返回结构化错误，不渲染它之后的窗口结果。
- 复用现有鉴权、scope、错误分类、内容安全和 dry-run 基础设施。

## 3. 非目标

- 不修改 GraphQuery OpenAPI 或服务端单次最多 86400 秒的限制。
- 不新增 SSE、WebSocket、服务端 chunked streaming 或 streaming RPC。
- 不按自然日、时区或本地午夜重新对齐窗口。
- 不开放并发数、重试次数或重试间隔的用户配置。
- 不支持查询当前登录用户之外的 Graph 数据，也不接受 UID、`open_id` 或其他用户标识输入。
- 不额外调用认证或通讯录接口转换用户标识。
- 不把 Graph 节点、边或详情写入日志、临时文件或长期存储。
- 不为 Graph 嵌套结果提供 `table`、`csv` 或 `jq` 输出语义。

## 4. 命令接口

```text
lark-cli memory +graph-query \
  --start-time-sec <int64> \
  --end-time-sec <int64> \
  [--detail-format markdown|json] \
  [--format json|ndjson|pretty] \
  [--as user]
```

`lark-memory-cli` 专用入口使用相同参数和行为。

参数约束：

- 命令不暴露 `--user-id`；目标用户固定为 `--as user` 对应的当前登录用户。
- CLI 直接读取本地登录态中已经保存的当前用户 `open_id`，不接受内部数字 UID，也不执行
  UID 到 `open_id` 的通讯录转换。
- `--start-time-sec` 是左闭区间起点，必须是非负 Unix 秒。
- `--end-time-sec` 是右开区间终点，必须大于起点。
- `--detail-format` 默认是 `markdown`，仅支持 `markdown` 和 `json`。
- 总跨度必须满足 `0 < end-start <= 604800`；恰好七天合法，七天加一秒直接拒绝。

时间参数以字符串 flag 接收后显式解析为 `int64`，避免平台相关整数宽度和静默溢出。所有输入
错误均返回带精确 `param` 的 typed validation error。

Graph shortcut 在身份解析、配置读取、凭证预检、HTTP client 构造和 dry-run 计划生成前执行
原始 flag 预检。缺失时间参数、非法时间范围和不支持的详情格式会优先返回本地校验错误，不会误报为
鉴权或配置问题。原始 flag 预检通过后再从 runtime 读取当前用户 `open_id`；登录态缺少有效
`open_id` 时返回 `authentication/token_missing`，并提示重新完成 `memory:hub` 用户授权。

## 5. 时间窗口切分

时间范围使用左闭右开区间 `[start_time_sec, end_time_sec)`。切分从传入的精确起点开始：

```text
window_start = start
window_end   = min(window_start + 86400, end)
```

每生成一个窗口后令 `window_start = window_end`，直到到达总范围终点。最后一个窗口可以不足一天。

例如 `[100, 172901)` 会拆成：

```text
窗口 1：[100, 86500)
窗口 2：[86500, 172900)
窗口 3：[172900, 172901)
```

切分结果满足：

- 窗口索引从 1 开始且严格递增。
- 每个窗口跨度在 1 到 86400 秒之间。
- 相邻窗口首尾相接，无重叠、无空洞。
- 最多生成七个窗口。
- 与时区和自然日无关。

## 6. 请求体

每个窗口独立调用 GraphQuery，使用当前登录用户相同的 `open_id` 和详情格式：

```json
{
  "user_id": "ou_example_user",
  "time_range": {
    "start_time_sec": 1784476800,
    "end_time_sec": 1784563200
  },
  "params": {
    "detail_format": "json"
  }
}
```

请求固定使用用户身份、`memory:hub` scope 和已有 Memory Hub extra headers。CLI 只从已经完成
用户认证的本地登录态读取当前用户 `open_id`，不会从 Memory 内容、自然语言上下文或任意 UID
推断目标用户，也不会切换到 bot 身份查询通讯录。

## 7. 并发执行模型

`executeGraphQuery` 负责完整生命周期：

1. 根据命令 context 创建可取消的子 context。
2. 为每个时间窗口启动一个 worker；七天限制使 worker 数量天然不超过七个。
3. worker 只负责本窗口 API 调用和重试，不写 stdout。
4. 每个 worker 向有界结果 channel 发送且只发送一个终态结果。
5. coordinator 按 `window_index` 缓存乱序完成的结果。
6. 从当前期望索引开始，连续渲染已完成的成功窗口。
7. 遇到最早失败窗口或输出失败时取消其他 worker，等待全部退出后返回错误。

单日请求也走相同执行路径，只会启动一个 worker。并发优化只降低多窗口查询耗时，不会直接降低
单日窗口的下游处理时间。

只有 coordinator 可以执行内容安全检查和输出，避免并发写 stdout 导致 JSON 行交错。结果 channel
容量等于窗口数，worker 即使在取消阶段也能发送终态结果并退出。

## 8. 单窗口重试

重试归属于每个窗口，不会重新执行已经成功的窗口，也不会阻塞其他窗口的首次请求。

- 每个窗口最多调用三次：一次初始请求和两次重试。
- 重试间隔固定为 200ms。
- 等待使用 timer 并监听 `ctx.Done()`，不使用不可取消的 sleep。
- API code `2200` 需要重试。
- typed network error 需要重试，即使其 `Retryable` 字段为 false。
- 其他被结构化标记为 retryable 的错误需要重试。
- context cancel/deadline、auth、permission、validation 和 content safety 错误不重试。
- 输出、渲染和内容安全失败不重试。

每次 Graph API 调用继续使用 single-transport-attempt 标记，关闭 SDK 对 dial failure 的隐式二次尝试，
确保“最多三次”只由窗口 worker 控制。transport wrapper 通过 `Unwrap` 和 `errors.As` 保留底层 cause、
timeout 和 temporary 分类；未标记的其他 CLI 请求保持 SDK 原有行为。

重试耗尽后保留最后一次 typed error，并在安全 hint 中补充失败窗口索引、时间范围和实际尝试次数。

## 9. 有序流式返回

并发只影响请求执行，不改变输出顺序。coordinator 只输出从窗口 1 开始的连续成功前缀：

- 后面的窗口先完成时先缓存在内存中，不立即渲染。
- 前序窗口成功后，连续输出已经就绪的后续成功窗口。
- 多个窗口同时失败时，以时间最早的失败窗口作为命令最终错误。
- 失败窗口之后的成功结果不会渲染，即使其请求已经完成。
- 并发启动意味着较晚窗口可能已到达下游；保证的是输出顺序和失败语义，不保证前序失败时后续请求
  一定尚未发出。

### JSON 与 NDJSON

`--format json` 和 `--format ndjson` 都按每个窗口一行输出紧凑 JSON envelope：

```json
{"ok":true,"identity":"user","data":{"window_index":1,"start_time_sec":1784476800,"end_time_sec":1784563200,"data":{"nodes":[],"edges":[]}}}
```

每一行都能独立解析。调用方必须同时检查进程退出状态；存在 stdout 不代表全部窗口成功。

### Pretty

`--format pretty` 按窗口打印标题和 Graph 数据，先展示 Node，再展示 Edge。每个可输出窗口完成后立即
写入 stdout。

### 不支持的输出

- `--format table` 和 `--format csv` 返回 typed validation error。
- `--jq` 返回 typed validation error，因为它依赖单个最终 envelope，与多 envelope 流式语义冲突。

## 10. Dry-run

`--dry-run` 与真实执行共用参数解析、当前用户身份解析、七天校验、窗口切分和请求体构造，一次输出
全部窗口调用计划。

唯一硬保证是不会调用 GraphQuery API。命令仍可能加载身份和配置，并执行 token/scope 预检；凭证刷新
或认证端点访问不属于 Graph 数据请求。dry-run 成功也不代表真实 user token、scope 或 Graph API 一定可用。

## 11. 失败与取消语义

- 已经输出的成功窗口保留在 stdout，构成合法但可能不完整的连续前缀。
- 当前失败窗口不输出成功 envelope，后续窗口不再渲染。
- coordinator 取消子 context，并等待所有 worker 退出，避免 goroutine 泄漏和阻塞发送。
- 父 context 取消会传播到所有 API 调用和重试 timer。
- 保留错误 category、subtype、code、log_id、retryable、troubleshooter 和 cause。
- 对外 message 使用固定本地文案；hint 只保留失败窗口坐标和尝试次数，不透传下游自由文本。
- 根命令在 stderr 输出结构化错误 envelope，并返回对应非零退出码。

日志和错误禁止包含 Graph detail、Node/Edge ID、完整请求体、token 或认证信息。

## 12. 代码边界

- `shortcuts/memory/graph_query.go`：shortcut 定义、preflight、dry-run 和请求体构造。
- `shortcuts/memory/graph_window.go`：参数解析、范围校验和纯函数窗口切分。
- `shortcuts/memory/graph_query_execution.go`：并发 worker、重试、结果协调和取消清理。
- `shortcuts/memory/graph_output.go`：窗口 envelope 和 pretty 输出。
- `internal/client/single_attempt.go`：单次 transport 尝试标记与 SDK retry bypass。
- `shortcuts/common/runner.go`：原始 flag preflight 和内容安全流式输出边界。
- `shortcuts/memory/shortcuts.go`：注册 Graph shortcut 和 API 路径。
- `README.md`、`MEMORY_SHORTCUTS.md`：用户命令、限制和输出说明。
- `skills/lark-memory/SKILL.md`：Agent 使用契约和数据安全约束。

Graph 的时间窗口、重试和输出顺序保持在 `shortcuts/memory`，通用 runner 只提供业务无关的 preflight
和单行流式 envelope 能力。

## 13. 测试与验收

### 参数和切分

- 命令不注册 `--user-id`，传入该旧参数时由 Cobra 返回 unknown flag。
- 登录态包含当前用户合法 `open_id` 时进入请求体；缺失或格式非法时返回 typed authentication error。
- 负时间戳、零跨度、逆序、int64 溢出。
- 一天、一天加一秒、恰好七天和七天加一秒。
- 窗口连续性、边界和最大数量。

### 并发和有序输出

- 所有 worker 能在任一 mock 请求释放前启动。
- 后续窗口先完成时 stdout 保持为空，前序完成后严格按索引输出。
- 多窗口不同完成顺序下，始终返回最早失败窗口。
- 输出或内容安全失败后能取消并等待全部 worker。

### 重试

- code `2200`、typed network error 和 retryable error 最多调用三次。
- 非重试错误只调用一次。
- 200ms backoff 期间取消时不再发起下一次调用。
- 重试耗尽后的错误 hint 包含窗口坐标和 `attempts=3`。

### CLI E2E

- dry-run 单窗口和多窗口请求体准确。
- 当前登录用户的 `open_id` 原样进入每个请求体，命令行不出现用户标识参数。
- 超过七天在生成调用计划前失败。
- 当前分支二进制能够逐行返回完整 JSON envelope。

### PPE 验证

- 使用只读 Graph 查询验证一天、两天到七天范围。
- 只统计完整成功且 Node/Edge 非空的请求。
- 失败窗口按固定次数重试，成功输出保持时间有序。
- 一天请求的性能数据单独统计，不把多窗口并发收益错误归因到单日下游耗时。

## 14. 风险与取舍

- 并发可降低多日总耗时，但会提高瞬时下游请求数；七天限制同时提供最大并发上界。
- 固定重试可吸收瞬时路由和网络失败，但最坏情况下每个窗口最多产生三次下游调用。
- 有序输出会暂存先完成的后续窗口，以换取稳定、可预测的消费协议。
- 失败后的 stdout 只是连续成功前缀，消费者必须结合退出码判断完整性。
- 单日查询只有一个窗口，不具备窗口并发收益；其耗时主要由下游 GraphQuery 决定。
