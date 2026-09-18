# GopherAI Backend 最终审查修复计划

## 目标

本计划用于处理 MVP 功能闭环后的全项目 Code Review 结果。修复范围只覆盖已经确认的真实行为问题，不借机扩展模板之外的平台能力，也不进行无目的架构重构。

处理顺序：

```text
公开认证接口输入边界
  -> 公开认证接口限流
  -> SSE Tool Calling 输出一致性
  -> PostgreSQL integration 与最终 Compose 验收
```

ChatJob 发布一致性单独记录为可靠性专项；TTS 任务归属保持当前模板边界。

## 审查基线

审查时以下检查均通过：

```text
go test ./...
go test -race ./...
go vet ./...
git diff --check
podman compose config --quiet
```

当前未配置独立的 `TEST_DATABASE_URL`，因此带 `integration` build tag 的 PostgreSQL Repository 测试尚未实际执行。

## 问题总览

| 优先级 | 问题 | 当前决定 |
|---|---|---|
| P0 | 注册和登录请求体没有大小限制 | 立即修复 |
| P0 | 注册、登录、验证码发送缺少匿名请求限流 | 立即修复 |
| P1 | SSE Tool planning 文本可能先于最终回答输出 | 立即修复 |
| P1 | ChatJob 写库成功、发布失败时留下孤儿 pending Job | 可靠性专项，暂不修改 |
| P2 | TTS 查询没有本地任务归属校验 | 接受当前模板边界 |

## 阶段 1：认证请求输入边界

状态：`pending`

### 问题

注册和登录 Handler 直接执行 `ShouldBindJSON`，没有使用 `http.MaxBytesReader`。两个接口都是公开路由，调用方可以提交远大于实际需要的请求体。

此外，注册校验密码长度为 8 到 72 字节，登录只检查非空。两条路径应使用一致的输入边界，避免异常超长密码继续进入数据库查询和 bcrypt。

### 冻结方案

新增统一限制：

```go
const maxAuthRequestBodyBytes = 16 << 10 // 16 KiB
```

同时应用于：

```text
POST /api/v1/auth/register
POST /api/v1/auth/login
```

错误映射：

```text
请求体超过限制 -> 413 Request Entity Too Large
JSON 无效       -> 400 Bad Request
```

User Service 的登录与注册都执行相同的密码长度校验：

```text
8 <= password bytes <= 72
```

保持按字节计数，因为 bcrypt 的上限是 72 字节，不是 72 个 Unicode 字符。

### 不做

- 不调整密码复杂度规则。
- 不引入密码强度库。
- 不改变现有响应结构。

### 测试与验收

- 注册和登录超大请求体均返回 413，Service 不被调用。
- 无效 JSON 仍返回 400。
- 登录空、过短和超过 72 字节的密码返回 400。
- 正常注册和登录行为保持不变。

## 阶段 2：公开认证接口限流

状态：`pending`

### 问题

以下接口不需要 JWT，因此无法使用当前按 `userID` 限流的 `RateLimitMiddleware`：

```text
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/email-verification-codes
```

注册会执行 bcrypt；登录为了防止账号枚举，对不存在账号也执行 dummy bcrypt。没有匿名限流时，攻击者可以持续消耗 CPU。

邮箱验证码已有按邮箱的 60 秒发送冷却，但攻击者可以轮换大量邮箱，仍可能消耗 SMTP 配额。

### 冻结方案

新增按来源 IP 计数的匿名限流边界，不修改现有按用户限流器：

```go
type AnonymousRateLimiter interface {
    Allow(ctx context.Context, identity string) (
        allowed bool,
        retryAfter time.Duration,
        err error,
    )
}
```

Redis 使用三个独立前缀：

```text
gopherai:rate_limit:auth_register:ip:<ip_hash>
gopherai:rate_limit:auth_login:ip:<ip_hash>
gopherai:rate_limit:email_verification:ip:<ip_hash>
```

IP 使用 SHA-256 摘要作为 Key 后缀，不把原始 IP 直接写入 Redis Key。

默认配置：

```env
AUTH_REGISTER_RATE_LIMIT=10
AUTH_REGISTER_RATE_WINDOW_SECONDS=600

AUTH_LOGIN_RATE_LIMIT=20
AUTH_LOGIN_RATE_WINDOW_SECONDS=300

EMAIL_VERIFICATION_IP_RATE_LIMIT=10
EMAIL_VERIFICATION_IP_RATE_WINDOW_SECONDS=3600
```

现有按邮箱发送冷却继续保留，因此邮箱发送同时受：

```text
同一邮箱 60 秒冷却
+
同一来源 IP 每小时 10 次
```

### IP 来源规则

当前 Backend 由 Compose 直接映射宿主端口，第一版只使用 `RemoteAddr` 中的远端 IP，不信任调用方提供的 `X-Forwarded-For` 或 `X-Real-IP`。

未来增加明确配置的可信反向代理后，再单独调整真实 IP 提取规则。本轮不接受任意转发 Header，避免攻击者伪造 IP 绕过限流。

### HTTP 行为

```text
允许          -> 执行 Handler
超过限制      -> 429 + Retry-After
Redis 异常    -> 503
无法解析来源 IP -> 500，不绕过限流
```

### 不做

- 不增加设备指纹、浏览器指纹或复杂风控。
- 不增加 CAPTCHA。
- 不引入多级限流、滑动窗口或令牌桶。
- 不以邮箱是否存在区分发送接口响应。

### 测试与验收

- 三个公开接口使用相互独立的 Redis Key。
- 同一 IP 达到限制后返回 429 和正确的 `Retry-After`。
- 不同 IP 互不影响。
- 伪造转发 Header 不改变当前直连模式的限流身份。
- Redis 异常时 Handler 不执行。
- 邮箱冷却和 IP 限流可以同时生效。

## 阶段 3：SSE Tool Calling 输出一致性

状态：`pending`

### 问题

当前 Tool Calling 的流式 planning 请求直接使用最终 SSE callback：

```text
GenerateStreamWithTools(..., onDelta)
```

在完整响应结束前，Agent 不知道模型最终是否返回 ToolCall。如果模型同时输出 planning 文本和 ToolCall，客户端会先收到 planning 文本，工具执行后又收到最终回答。

结果可能变成：

```text
客户端 SSE 内容 = planning 文本 + 最终回答
数据库历史消息  = 最终回答
```

这违反 MCP 计划中“客户端只看到最终回答文本”的规则，也使实时页面与历史记录不一致。

### 技术约束

流式数据一旦发送给客户端就无法撤回。只有完整 planning 响应结束后，才能确定是否包含 ToolCall。

因此想严格保证只输出最终内容，就必须在 planning 阶段暂存 delta。

### 冻结方案

在 `agent.Client.GenerateStream` 内部使用临时缓冲 callback：

```text
planning 流式请求
  -> delta 暂存在内存
  -> planning 完成

没有 ToolCall
  -> 按原顺序把暂存 delta 交给外部 onDelta
  -> 返回 planning Result

有一个 ToolCall
  -> 丢弃 planning 文本
  -> 调用工具
  -> 最终模型回答直接流式写入外部 onDelta
```

缓存使用 `strings.Builder` 或 `[]string`，并设置 planning 文本上限，防止异常 Provider 无界占用内存。建议上限与最终消息业务上限一致，为 20000 个 Unicode 字符；超过上限返回明确错误并终止请求。

### 明确权衡

当模型不调用工具时，客户端必须等 planning 完成后才会收到暂存内容，因此首字节延迟会增加。这个代价是保证协议正确性所必需的；第一版不设计可撤回事件或额外 planning SSE 事件。

### 不做

- 不改变现有 `delta/error/done` SSE 事件格式。
- 不向客户端暴露 ToolCall 参数或原始工具结果。
- 不增加多轮或并行工具调用。

### 测试与验收

- planning 没有 ToolCall：暂存 delta 最终按顺序输出一次。
- planning 有 ToolCall：暂存 delta 不输出，客户端只收到 final delta。
- planning 返回多个 ToolCall：不输出暂存内容并返回现有错误。
- 工具调用失败：不输出 planning 内容，返回 error 事件。
- 客户端断开时 planning、MCP 和最终模型调用都能取消。
- SSE 拼接内容与最终保存的 assistant message 完全一致。

## 阶段 4：PostgreSQL Integration 与最终验收

状态：`pending`

### 目标

前三项修复完成后，建立独立测试数据库并真实运行现有 Repository integration tests：

```bash
TEST_DATABASE_URL='postgres://.../gopherai_test' \
  go test -tags=integration ./internal/platform/postgresql
```

测试数据库必须与开发数据库分开，并执行全部 migration。不得把生产或日常开发数据库用作 integration test 数据库。

随后执行：

```text
go test ./...
go test -race ./...
go vet ./...
podman compose config --quiet
完整 Compose 重建
关键接口 E2E
```

关键 E2E：

- 邮箱验证码发送、冷却、注册、单次消费和登录。
- 普通 Chat、SSE Chat、天气 Tool Calling 和 ChatJob。
- RAG 上传与检索。
- ONNX 图片识别。
- TTS 创建、查询和音频访问。
- Compose 停止并重启后的数据保留。

## 可靠性专项：ChatJob 发布一致性

状态：`deferred`

### 已确认问题

当前创建流程是：

```text
PostgreSQL INSERT pending Job
-> RabbitMQ Publish
```

如果 INSERT 成功而 Publish 失败：

```text
HTTP 返回错误
Job 留在 pending
客户端没有拿到 job_id
RabbitMQ 没有对应消息
Job 不会被 Worker 处理
```

### 为什么本轮不直接修

可靠修复会改变跨 PostgreSQL/RabbitMQ 的交付模型，通常需要以下方向之一：

```text
Transactional Outbox
pending Job 定时扫描补发
客户端幂等重试 + 可恢复发布状态
```

这些方案可能增加 migration、后台扫描器或新的状态语义，会触及已经冻结的 ChatJob 状态机。根据项目约定，本轮不擅自加入 outbox、调度线程或新状态。

### 进入专项前需要先确认

- 是否允许新增 outbox 表和 migration。
- 是否要求至少一次发布语义。
- 创建接口失败时是否仍返回已落库的 job_id。
- 如何避免补发与首次发布形成重复消息。
- 是否同时处理现有 lease、死信队列和 processing 卡住问题。

## 接受的 MVP 边界：TTS 任务归属

状态：`accepted`

当前 TTS 不在本地保存任务，查询接口只要求调用者已登录，再使用百度返回的不可预测 `task_id` 查询状态。

风险：如果 task ID 泄露，另一个登录用户可能查询到临时音频 URL。

严格授权需要保存：

```text
user_id -> provider task_id
```

这需要新增本地任务持久化和 migration，而 GopherAI-v2 同样只转发 Provider task ID。当前 MVP 接受该边界，不主动扩展 TTS 任务表。

## 完成标准

本修复计划完成时应满足：

- 公开认证请求有明确 body 上限。
- 注册、登录和验证码发送都有匿名 IP 限流。
- SSE Tool Calling 只向客户端输出最终可保存的回答内容。
- PostgreSQL integration tests 在独立数据库真实通过。
- 完整 Compose 镜像完成一次最终 E2E。
- ChatJob 与 TTS 的延期边界在 README 中明确记录。
- 不增加模板范围外的新业务模块。
