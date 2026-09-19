# GopherAI Backend

GopherAI 的 Go 后端实现，使用 Gin、PostgreSQL、Valkey/Redis、RabbitMQ、
OpenAI 兼容 API 和 ONNX Runtime。

```text
backend/
├── cmd/server/                    应用入口和依赖组装
├── internal/httpapi/              Handler、中间件和路由
├── internal/                      业务 Service 和 Repository 接口
├── internal/platform/             PostgreSQL、Redis、RabbitMQ、OpenAI、ONNX 等实现
├── migrations/                    PostgreSQL migration
└── scripts/migrate.sh             Compose migration 入口
```

## Docker Compose 运行

Compose 会启动 Backend、PostgreSQL、Valkey、RabbitMQ，并通过一次性 migration
容器初始化或升级数据库。需要 Docker Compose v2，或者已配置 Compose provider 的
Podman。

先创建容器专用配置，并填写 JWT、Chat Provider 和 Embedding Provider 配置：

```bash
cp .env.compose.example .env.compose
```

`.env.compose` 包含本机密钥且已被 Git 忽略。PostgreSQL、Valkey 和 RabbitMQ 的
容器内部连接地址由 `compose.yaml` 设置，不需要写入该文件。

Podman 可能自动把宿主机的代理变量注入容器。如果代理只监听宿主的 `127.0.0.1`，
容器无法直接使用它，因此 Compose 默认清空运行时代理。确实需要代理时，应提供一个
容器能够访问的地址：

```bash
export CONTAINER_HTTP_PROXY=http://host.containers.internal:7897
export CONTAINER_HTTPS_PROXY=http://host.containers.internal:7897
```

代理服务必须监听容器可达的宿主地址；只监听 `127.0.0.1` 仍然不可用。镜像构建阶段
使用 host network，可以继续使用仅监听宿主 loopback 的本地代理。

构建并启动：

```bash
docker compose up --build -d
```

使用 Podman 时将 `docker compose` 替换为 `podman compose`。启动后检查：

```bash
docker compose ps
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

查看和跟踪 Backend 日志：

```bash
docker compose logs backend
docker compose logs -f backend
```

Compose 默认同时启动内部 MCP Weather Server。Backend 会通过
`http://mcp-server:8081/mcp` 发现并调用只读的 `get_weather` 工具；该地址由
`compose.yaml` 设置，不需要写入 `.env.compose`。天气 Provider 可以通过以下配置调整：

```env
WEATHER_API_BASE_URL=https://wttr.in
WEATHER_API_TIMEOUT_SECONDS=5
MCP_CALL_TIMEOUT_SECONDS=10
```

普通 Chat、SSE Chat 和 ChatJob 都共用同一套 Agent Tool Use 能力。可以用“上海现在天气
怎么样？”验证天气工具，用 `/chat/stream` 验证增量输出，用 `/chat-jobs` 验证 RabbitMQ
异步任务。

## TTS 文本转语音

TTS 是可选功能。需要在 `.env.compose` 中填写百度语音应用的凭据：

```env
BAIDU_TTS_API_KEY=
BAIDU_TTS_SECRET_KEY=
```

两个值都为空时 Backend 仍可启动，但 TTS 接口返回 `503`；只配置一个值会被视为错误配置。
创建与查询接口都需要 JWT：

```text
POST /api/v1/tts/tasks
GET  /api/v1/tts/tasks/:id
```

创建接口只接受 `{"text":"..."}`，返回 `task_id`。客户端轮询查询接口，状态变成
`succeeded` 后使用响应中的临时 `audio_url` 下载或播放 MP3。默认每个用户每小时最多
创建 5 个任务，查询不消耗创建额度。

## 邮箱验证码注册

邮箱验证默认关闭，现有邮箱密码注册流程保持不变。启用时在 `.env.compose` 中配置：

```env
EMAIL_VERIFICATION_ENABLED=true
SMTP_USERNAME=sender@qq.com
SMTP_PASSWORD=<SMTP 授权码>
SMTP_FROM=sender@qq.com
```

QQ 邮箱的 `SMTP_PASSWORD` 是开启 SMTP 后生成的授权码，不是邮箱登录密码。默认使用
`smtp.qq.com:587` 和 STARTTLS；验证码有效 10 分钟，同一邮箱 60 秒后可以重新发送。

公开发送接口：

```text
POST /api/v1/auth/email-verification-codes
```

请求体为 `{"email":"user@example.com"}`。收到验证码后，在现有注册请求中增加：

```json
{
  "email": "user@example.com",
  "password": "password123",
  "verification_code": "123456"
}
```

验证码验证成功后会被立即消费，只能用于一次注册。

重新构建 Backend 并启动：

```bash
docker compose build backend
docker compose up -d
```

如果 Podman 构建日志异常地复用了跨阶段 `COPY --from=builder` 缓存，可以强制完整重建：

```bash
podman compose build --no-cache backend
podman compose up -d --force-recreate backend
```

停止并删除容器，但保留 PostgreSQL、Valkey、RabbitMQ 和 RAG 数据卷：

```bash
docker compose down
```

彻底清空本项目的容器数据：

```bash
docker compose down -v
```

`down -v` 会不可恢复地删除数据库、队列、Redis 和 RAG 文档数据，只应在明确需要
重置开发环境时使用。

## 已接受的 MVP 边界

ChatJob 创建当前采用“PostgreSQL 写入 pending Job，再发布 RabbitMQ 消息”的顺序，
两步之间不是原子事务。若数据库写入成功但消息发布失败，可能留下没有对应队列消息的
pending Job。本阶段不引入 Transactional Outbox、后台扫描补发、lease 或死信队列，
后续如进入可靠性专项再统一设计交付语义。

TTS 任务由百度 Provider 保存，本地不持久化 `user_id -> task_id` 归属。查询接口要求 JWT，
但持有其他用户泄露的不可预测 `task_id` 时，理论上仍可能查询到临时音频 URL。严格归属
校验需要新增本地 TTS 任务表；当前模板范围接受这一边界。

## 本地验证

```bash
go test ./...
go vet ./...
```

PostgreSQL integration 测试需要一个已经执行 migration 的独立测试数据库：

```bash
TEST_DATABASE_URL='postgres://user:password@127.0.0.1:5432/gopherai_test' \
  go test -tags=integration ./internal/platform/postgresql
```
