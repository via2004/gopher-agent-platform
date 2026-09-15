# GopherAI 邮箱验证码注册开发计划

## 目标

在当前邮箱密码注册流程上增加邮箱所有权验证：

```text
用户请求邮箱验证码
  -> Backend 生成 6 位数字验证码
  -> Redis 临时保存验证码
  -> SMTP 将验证码发送到目标邮箱
  -> 用户提交邮箱、密码和验证码
  -> Backend 原子校验并消费验证码
  -> PostgreSQL 创建用户
```

这里实现的是邮箱一次性验证码（Email OTP），不是用于区分人和机器的图片 CAPTCHA。API 和代码统一使用 `email verification code` 命名，避免混淆。

## 模板依据与当前差距

GopherAI-v2 已经存在：

- `POST /api/v1/user/captcha` 发送邮箱验证码。
- 注册请求携带邮箱、密码和验证码。
- 验证码存入 Redis，并设置两分钟有效期。
- 通过 QQ SMTP 发送验证码邮件。
- 验证成功后删除验证码。

当前 Backend 已经存在：

- 邮箱密码注册和登录。
- PostgreSQL 用户唯一邮箱约束。
- Redis Client 和 Lua 脚本使用经验。
- Handler -> Service -> Repository 分层。
- Docker Compose 和环境变量配置。

当前缺少验证码生成、Redis 临时状态、SMTP Sender、发送接口，以及注册前的验证码校验。

模板用于确定功能范围，但不直接复制以下行为：

- 模板没有发送冷却时间，容易重复发送邮件。
- 模板先 GET 再 DEL，验证码校验和消费不是原子操作。
- 模板使用全局 Context，不能正确传播请求取消。
- 模板发送失败后可能残留无法使用的验证码。
- 模板把功能称作 CAPTCHA，实际语义是邮箱 OTP。

## 功能定位

当前注册：

```text
email + password -> 创建账号
```

启用邮箱验证后的注册：

```text
email -> 收到 verification_code
email + password + verification_code -> 创建账号
```

这个模块只证明用户能够接收目标邮箱的邮件。它不是：

- 登录二次验证。
- 找回或重置密码。
- 修改邮箱验证。
- 防机器人的图形验证码。
- 营销邮件系统。

## 冻结的功能范围

### 本轮实现

- 一个发送邮箱验证码的公开 HTTP 接口。
- 注册接口增加可选的 `verification_code` 字段。
- 功能启用时，注册必须验证并消费验证码。
- 使用 `crypto/rand` 生成 6 位数字验证码。
- Redis 保存验证码、有效期、发送冷却和失败尝试次数。
- 验证成功后原子删除验证码，保证一个验证码只能成功使用一次。
- 错误验证码最多尝试 5 次，达到上限后验证码失效。
- 同一邮箱 60 秒内只能发送一次。
- 使用 SMTP STARTTLS 发送纯文本邮件。
- 邮箱验证默认关闭，不影响现有本地开发和 E2E。
- 更新配置示例、Compose、README 和测试。

### 本轮不实现

- 不新增用户或验证码数据库表。
- 不保存验证码发送历史和审计记录。
- 不使用 RabbitMQ 异步发送邮件。
- 不实现登录、找回密码或修改邮箱验证码。
- 不实现图片 CAPTCHA。
- 不实现短信验证码。
- 不实现邮件模板管理、HTML 编辑器或多语言模板。
- 不实现营销邮件、批量邮件或邮件队列。
- 不引入第三方邮件 SaaS API。
- 不增加基于 IP 的复杂风控系统。

## HTTP 接口

### 发送验证码

该接口不需要 JWT，因为用户尚未注册或登录。

```http
POST /api/v1/auth/email-verification-codes
Content-Type: application/json

{
  "email": "user@example.com"
}
```

成功响应：

```http
HTTP/1.1 202 Accepted

{
  "message": "verification code accepted"
}
```

响应中绝不返回验证码。

为避免通过接口判断邮箱是否已经注册，合法邮箱的发送请求使用统一成功响应。第一版不在发送阶段查询用户表；最终注册仍由数据库唯一约束阻止重复邮箱。

### 注册

请求扩展为：

```http
POST /api/v1/auth/register
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "password123",
  "verification_code": "482915"
}
```

行为规则：

```text
EMAIL_VERIFICATION_ENABLED=false
-> 保持当前注册行为
-> verification_code 可以省略

EMAIL_VERIFICATION_ENABLED=true
-> verification_code 必须是 6 位数字
-> 校验成功并消费后才能创建账号
```

登录接口保持不变。注册成功仍返回当前项目已有的用户信息，不自动签发 JWT。

## 验证码状态与生命周期

默认参数：

```text
验证码长度：       6 位数字
验证码有效期：     10 分钟
重复发送冷却：     60 秒
最大错误尝试次数： 5 次
```

单个邮箱的生命周期：

```text
不存在
  |
  | 发送成功
  v
有效验证码 + 冷却标记
  |
  | 正确验证
  v
删除验证码和尝试次数 -> 只能成功使用一次
```

其他分支：

```text
验证码过期
-> Redis TTL 自动删除

输入错误
-> 错误次数 +1
-> 未达 5 次：验证码继续有效
-> 达到 5 次：删除验证码，必须重新发送

冷却结束后重新发送
-> 新验证码覆盖旧验证码
-> 旧验证码立即失效
```

## Redis 设计

Key 使用规范化邮箱的 SHA-256 摘要，避免把明文邮箱直接暴露在 Redis Key 中：

```text
gopherai:email_verification:code:<email_sha256>
gopherai:email_verification:cooldown:<email_sha256>
gopherai:email_verification:attempts:<email_sha256>
```

验证码本身仍作为短期 Redis Value 保存，TTL 到期后自动清理。Redis 只允许在内部网络访问，不记录验证码内容。

发送冷却必须小于或等于验证码有效期，避免验证码已经失效但用户仍不能重新发送。

### 保存验证码

Redis Repository 提供一个原子保存操作：

```go
Save(ctx, email, code string, ttl, cooldown time.Duration) error
```

Lua 脚本一次完成：

```text
1. 检查 cooldown key 是否存在
2. 存在 -> 返回 ErrSendTooFrequent
3. 不存在 -> 写入 code key 和 TTL
4. 写入 cooldown key 和 TTL
5. 删除旧 attempts key
```

### 校验并消费

```go
VerifyAndConsume(ctx, email, code string, maxAttempts int) error
```

Lua 脚本一次完成：

```text
1. 验证码不存在 -> ErrCodeInvalidOrExpired
2. 验证码正确 -> 删除 code 和 attempts，返回成功
3. 验证码错误 -> attempts + 1
4. 达到上限 -> 删除 code 和 attempts
5. 未到上限 -> 保留验证码直到原 TTL
```

校验和删除必须在同一个 Lua 脚本中，避免两个并发注册请求同时使用同一个验证码成功。

## 发送失败的处理

发送流程使用以下顺序：

```text
Redis 原子保存验证码和冷却
-> SMTP 发送邮件
-> 成功：结束
-> 失败：尽力删除本次验证码和冷却标记
```

先保存再发送，能够保证用户收到邮件时验证码已经可用。SMTP 失败时执行补偿删除，使用户可以重新发送。补偿删除会同时比较验证码，只删除本次发送保存的旧值，避免慢请求误删后来覆盖的新验证码。

如果补偿删除也失败，请求仍返回发送失败；Redis 中的验证码会在 TTL 后自动清理。这是可接受的短期不一致，不引入事务、消息队列或工作流引擎。

## 目标架构

```text
POST verification-codes
        |
        v
EmailVerificationHandler
        |
        v
emailverification.Service
        |                 |
        v                 v
Redis CodeStore       SMTP Sender


POST register
        |
        v
UserHandler
        |
        v
user.Service
        |
        | EmailVerifier.VerifyAndConsume
        v
emailverification.Service -> Redis
        |
        v
PostgreSQL UserRepository
```

预计代码结构：

```text
internal/emailverification/
    errors.go
    repository.go
    service.go
    service_test.go

internal/platform/redis/
    email_verification_repository.go
    email_verification_repository_integration_test.go

internal/platform/smtp/
    sender.go
    errors.go
    sender_test.go

internal/httpapi/
    email_verification_handler.go
    email_verification_handler_test.go
```

验证码模块只负责生成、保存、发送和验证，不创建用户。用户模块只依赖一个小接口：

```go
type EmailVerifier interface {
    VerifyAndConsume(ctx context.Context, email, code string) error
}
```

## 核心接口草案

```go
type CodeStore interface {
    Save(ctx context.Context, email, code string, ttl, cooldown time.Duration) error
    VerifyAndConsume(ctx context.Context, email, code string, maxAttempts int) error
    Delete(ctx context.Context, email, code string) error
}

type Sender interface {
    SendVerificationCode(ctx context.Context, email, code string) error
}

type Service struct {
    codes  CodeStore
    sender Sender
}

func (s *Service) Send(ctx context.Context, email string) error
func (s *Service) VerifyAndConsume(ctx context.Context, email, code string) error
```

接口由使用方定义，只为 Redis、SMTP 和单元测试替身保留必要边界，不继续为内部小函数抽 interface。

## SMTP 决策

第一版使用标准 SMTP，而不是绑定 QQ 邮箱专用 API。默认配置沿用模板使用的 QQ SMTP：

```env
SMTP_HOST=smtp.qq.com
SMTP_PORT=587
SMTP_USERNAME=sender@qq.com
SMTP_PASSWORD=<QQ 邮箱 SMTP 授权码>
SMTP_FROM=sender@qq.com
SMTP_FROM_NAME=GopherAI
```

端口 587 使用 STARTTLS。`SMTP_PASSWORD` 对 QQ 邮箱来说是 SMTP 授权码，不是邮箱登录密码。

发送实现使用 `github.com/wneessen/go-mail`，原因是它支持 Context、STARTTLS 和常见 SMTP 鉴权；不使用已经冻结且缺少 Context 支持的标准库 `net/smtp`，也不复制模板的旧 `gomail.v2` 实现。

第一版邮件固定为纯文本：

```text
Subject: GopherAI 邮箱验证码

你的验证码是：482915
请在有效期内尽快使用，并且不要将验证码告诉他人。
```

## 配置

新增环境变量：

```env
EMAIL_VERIFICATION_ENABLED=false
EMAIL_VERIFICATION_CODE_TTL_SECONDS=600
EMAIL_VERIFICATION_RESEND_INTERVAL_SECONDS=60
EMAIL_VERIFICATION_MAX_ATTEMPTS=5

SMTP_HOST=smtp.qq.com
SMTP_PORT=587
SMTP_USERNAME=
SMTP_PASSWORD=
SMTP_FROM=
SMTP_FROM_NAME=GopherAI
SMTP_TIMEOUT_SECONDS=10
```

配置规则：

```text
EMAIL_VERIFICATION_ENABLED=false
-> 不构造 SMTP Sender 和验证码 Redis Repository
-> 当前邮箱密码注册继续工作
-> 发送验证码接口返回 503

EMAIL_VERIFICATION_ENABLED=true
-> 所有 SMTP 配置必须完整有效
-> 注册必须携带验证码
-> 配置不完整时 Backend 启动失败
```

`.env` 与 `.env.compose` 保存真实 SMTP 授权码并继续由 Git 忽略；example 文件只保留空值占位。

## 错误边界

验证码领域错误：

```text
ErrInvalidEmail
ErrInvalidCode
ErrCodeInvalidOrExpired
ErrTooManyAttempts
ErrSendTooFrequent
ErrNotConfigured
ErrGenerateCodeFailed
ErrStoreFailed
ErrSendFailed
```

HTTP 映射：

```text
邮箱或验证码格式错误       -> 400 Bad Request
验证码错误、过期或已消费   -> 400 Bad Request
错误尝试达到上限           -> 400 Bad Request
同一邮箱发送过于频繁       -> 429 Too Many Requests
功能未启用                 -> 503 Service Unavailable
Redis 或 SMTP 暂时不可用   -> 503 Service Unavailable
请求超时                   -> 504 Gateway Timeout
其他内部错误               -> 500 Internal Server Error
```

接口响应不返回 SMTP 原始错误、Redis Key、验证码或邮箱凭据。

## 注册一致性边界

注册顺序：

```text
1. 校验并规范化邮箱和密码
2. 计算密码哈希
3. 原子验证并消费验证码
4. PostgreSQL 创建用户
```

先完成输入校验和密码哈希，避免可预见的本地错误消耗验证码。验证码必须在创建用户前消费，才能阻止同一验证码并发创建多个请求。

如果验证码消费成功后 PostgreSQL 创建失败，验证码不会恢复，用户需要重新发送。这是本轮接受的失败边界；跨 Redis 与 PostgreSQL 无法使用本地事务，当前范围不引入分布式事务或工作流引擎。

## 安全边界

- 验证码使用 `crypto/rand`，不使用 `math/rand`、时间戳或可预测序列。
- 验证码、SMTP 密码和邮件正文不写日志。
- Redis Key 不包含明文邮箱。
- 验证码使用成功后立即删除。
- 验证码 TTL、发送冷却和最大尝试次数同时限制暴力猜测与邮件滥用。
- SMTP 必须使用 STARTTLS，不允许明文发送凭据。
- 发送接口不返回验证码，也不区分邮箱是否已经注册。
- 第一版只有按邮箱限流；跨大量邮箱的 IP/设备风控明确延期。

## 测试策略

### Email Verification Service

- 生成的验证码恰好为 6 位数字。
- 非法邮箱在 Redis 和 SMTP 调用前被拒绝。
- 保存成功后才调用 SMTP。
- SMTP 失败时按邮箱和验证码执行 Redis 条件补偿删除。
- Context 取消能够阻止后续步骤。
- 验证请求正确传递邮箱、验证码和最大尝试次数。
- 未配置时 Send 和 Verify 都返回 503 对应错误。

### Redis Repository

- 首次 Save 成功并设置正确 TTL。
- 冷却时间内重复 Save 返回 ErrSendTooFrequent。
- 冷却结束后新验证码覆盖旧验证码。
- 正确验证码只允许一次成功消费。
- 并发校验只有一个请求成功。
- 错误验证码累计尝试次数并保留原 TTL。
- 达到最大尝试次数后验证码失效。
- Redis 异常映射为稳定错误。

### SMTP Sender

- 配置字段和邮箱地址校验。
- 邮件收件人、发件人、主题和纯文本正文正确。
- Context 取消和超时正常传播。
- 错误信息不包含 SMTP 密码或验证码。

### User 与 Handler

- 功能关闭时保持现有注册行为。
- 功能开启时缺少、错误、过期和正确验证码。
- 验证成功后再创建用户。
- 验证失败时不访问用户 Repository。
- 发送接口的 202、400、429、503 和 504 映射。
- Router 将发送接口注册为公开路由。

### 真实验收

```text
启用 EMAIL_VERIFICATION_ENABLED
-> 请求发送验证码
-> 真实邮箱收到邮件
-> 使用验证码注册成功
-> 同一验证码再次注册失败
-> 60 秒内重复发送返回 429
```

## 开发阶段

### 阶段 0：范围与状态确认

状态：`completed`

- 确认该功能来自 GopherAI-v2 和根 README。
- 冻结为邮箱注册 OTP，不扩展到登录、找回密码或短信。
- 确认 Redis 临时存储、SMTP 同步发送，不新增 migration 和 RabbitMQ 流程。

### 阶段 1：验证码领域 Service

状态：`completed`

完成记录：

- 新增 `internal/emailverification` 的 CodeStore、Sender、Config 和 Service。
- 使用 `crypto/rand` 生成包含前导零的 6 位数字验证码。
- Send 会规范化邮箱、先保存验证码再发送，SMTP 失败时使用独立清理 Context 按验证码补偿删除。
- VerifyAndConsume 校验 6 位数字格式，并把最大尝试次数交给原子 Store 边界。
- Store、Sender 和随机数错误已收敛成稳定领域错误，同时保留 Context 错误。
- Store 与 Sender 同时为空表示功能未启用，只缺一个依赖则拒绝构造。
- 单元测试覆盖配置、输入、正常调用、错误传播、失败补偿、禁用状态和 Context 取消。

- 新增 `internal/emailverification`。
- 定义 CodeStore、Sender、错误和配置。
- 实现安全验证码生成、发送编排和校验编排。
- 使用 fake Store/Sender 补单元测试。

### 阶段 2：Redis CodeStore

状态：`completed`

完成记录：

- 新增 Redis EmailVerificationCodeStore，并用 SHA-256 邮箱摘要构造三个隔离 Key。
- Save Lua 脚本原子检查冷却、保存验证码和冷却 TTL，并清空旧尝试次数。
- VerifyAndConsume Lua 脚本原子校验、累计错误次数、同步剩余 TTL，并在成功或达到上限时删除验证码。
- Delete Lua 脚本只清理仍与指定验证码匹配的状态，避免慢 SMTP 请求误删新验证码。
- 直接调用 Repository 时也会校验邮箱、6 位数字、TTL、冷却和最大尝试次数。
- Integration 测试覆盖并发保存、单次消费、错误上限、TTL、条件补偿和邮箱隔离。

- 实现 SHA-256 邮箱 Key。
- 使用 Lua 原子保存、冷却、尝试计数和消费。
- 补 Redis integration 测试。

### 阶段 3：SMTP Sender

状态：`completed`

完成记录：

- 引入 `github.com/wneessen/go-mail v0.8.1`，新增 SMTP Sender。
- SMTP Client 固定要求 STARTTLS，启用鉴权自动发现并配置连接超时。
- Sender 构造固定主题、From Name 和纯文本验证码正文。
- 每次发送同时受调用方 Context 和独立 SMTP 超时控制。
- 配置、收件人和六位验证码会在网络调用前校验。
- 普通投递错误收敛为稳定错误，不暴露 SMTP 密码、验证码或底层错误文本。
- 单元测试覆盖 TLS 配置、邮件内容、输入、取消、超时和敏感信息保护。

- 引入并固定 `github.com/wneessen/go-mail` 版本。
- 实现 STARTTLS SMTP Sender。
- 固定纯文本验证码邮件格式。
- 补配置、消息内容、取消和敏感信息测试。

### 阶段 4：注册与 HTTP 接入

状态：`completed`

完成记录：

- 注册请求新增 `verification_code`，并显式传入 user.Service。
- user.Service 新增启用邮箱验证的构造方式；默认构造仍保持原有直接注册行为。
- 启用时先校验输入并计算密码哈希，再验证并消费验证码，最后写入用户 Repository。
- 新增公开的发送验证码 Handler 与 `/api/v1/auth/email-verification-codes` 路由。
- 验证码输入、过期、尝试上限、Redis/SMTP 不可用和超时已映射成稳定 HTTP 错误。
- User、Handler 和 Router 测试覆盖关闭/开启行为、调用顺序、错误映射、请求大小与公开访问。

- 注册请求增加 `verification_code`。
- `user.Service` 接入可选 EmailVerifier。
- 新增发送验证码 Handler 和公开路由。
- 补 User、Handler 和 Router 测试。

### 阶段 5：Bootstrap 与配置

状态：`pending`

- 新增可选 Email Verification Feature。
- 解析开关、TTL、冷却、最大尝试次数和 SMTP 配置。
- 更新 `.env.example`、`.env.compose.example` 和 README。
- 验证默认关闭时现有功能不受影响。

### 阶段 6：真实 E2E 与收尾

状态：`pending`

- 使用真实 SMTP 授权码发送邮件。
- 完成验证码注册、单次消费和发送冷却验收。
- 运行 `go test ./...`、`go vet ./...` 和相关竞态测试。
- 模块级 code review 后提交最终 MVP。

## 参考

- 模板实现：`GopherAI-v2/controller/user/user.go`
- [go-mail 官方仓库](https://github.com/wneessen/go-mail)
- [go-mail Go 文档](https://pkg.go.dev/github.com/wneessen/go-mail)
