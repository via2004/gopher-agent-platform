# GopherAI 前端开发计划与 API 契约

更新日期：2026-09-19。后端基线：`31e9a34 fix: make compose proxy handling explicit`。

状态：接口核对和计划已完成；技术栈、页面结构与阶段 1 待讨论确认，尚未创建前端工程。

## 1. 工作范围与仓库边界

- 唯一开发、提交仓库：`/home/usr1/Projects/gopherAI/backend`。
- 已核实 remote：`git@github.com:via2004/gopher-agent-platform.git`。
- 前端位置：`/home/usr1/Projects/gopherAI/backend/frontend`。当前不存在该目录，后续创建独立 npm 工程，但不初始化嵌套 Git 仓库。
- 这个位置合理：Go 与前端共用 Git 历史；前端的依赖、构建、测试独立运行；无需引入 monorepo 管理工具。
- `GopherAI-v2/vue-frontend` 为主要页面参考；v1 为补充参考；父目录 README 只用于确认需求。三者均不修改，也不使用父目录的 Git remote。
- 后端 MVP 保持定版。如发现真实 API 接入阻碍，先说明复现证据、影响和最小调整建议，再讨论，不顺手重构。
- 本轮只新增本文档，不改业务代码、模板、Compose 或根需求 README，不提交、不推送。

### 本轮基线检查

已在 backend 仓库依次执行：

```sh
git status --short
git log -5 --oneline
go test ./...
go vet ./...
podman compose ps
```

结果：开始时工作区干净；HEAD 与交接一致；Go 测试与 vet 通过。PostgreSQL、Valkey、RabbitMQ、MCP 服务健康，backend 运行中，一次性 migration 容器退出码为 0。

额外只读检查：`/healthz` 与 `/readyz` 返回 200，后者也通过绕过宿主代理的直连请求确认。当前 Node 为 `v26.9.0`、npm 为 `12.0.2`。本轮没有重新执行 race、数据库 integration 或会产生业务数据的完整 E2E；不把交接中的历史结果当作本轮执行结果。

## 2. 契约来源与通用规则

主要依据：

- [Router](../internal/httpapi/router.go)：实际路由、认证与限流范围。
- [用户 Handler](../internal/httpapi/user_handler.go)、[验证码 Handler](../internal/httpapi/email_verification_handler.go)。
- [会话 Handler](../internal/httpapi/conversation_handler.go)、[消息 Handler](../internal/httpapi/message_handler.go)。
- [Chat Handler](../internal/httpapi/chat_handler.go)、[ChatJob Handler](../internal/httpapi/chatjob_handler.go)。
- [图片 Handler](../internal/httpapi/image_handler.go)、[RAG Handler](../internal/httpapi/rag_handler.go)、[TTS Handler](../internal/httpapi/tts_handler.go)。
- 对应 Service、状态模型、SQL、[启动配置](../cmd/server/bootstrap.go)、[README](../README.md) 与 [Compose](../compose.yaml)：补充校验约束、排序、持久化与配置行为。

以下是已实现的 API 契约。后续章节中标为“建议”的内容是前端设计，不是后端已有能力。

### 请求与响应

- 基础地址为 `http://127.0.0.1:8080`，业务路径前缀 `/api/v1`。
- 除健康检查、注册、登录、发送验证码外，均需 `Authorization: Bearer <access_token>`。
- JSON 请求使用 `Content-Type: application/json`；multipart 交给浏览器生成 boundary，不能手写不完整的 Content-Type。
- 成功直接返回对象，不存在模板的 `status_code: 1000` / `status_msg` 包装，也没有统一的 `data` 外层。
- 应用错误通常为 `{"code":"INVALID_REQUEST","message":"request is invalid"}`。代理错误、连接中断、Gin 未匹配路由或异常恢复不保证有该 JSON 结构，客户端必须有兜底。
- 后端生成响应头 `X-Request-ID`，错误详情可保留它，便于定位日志。
- 时间为 Go `time.Time` 的 JSON 字符串，即 RFC 3339 格式，可能包含小数秒。前端本地化显示，不改写原始契约。
- 用户、会话、消息、ChatJob ID 在 JSON 中为数字（Go `uint64`）；TTS `task_id` 为字符串。路由参数是字符串，需要检查正整数。MVP 的数字 ID 使用安全整数校验；若未来超过 JavaScript 安全整数范围，需讨论字符串契约，事后转字符串不能恢复已丢失的精度。
- 不存在刷新 Token、服务端退出登录、功能开关查询、模型列表、模型选择、会话改名、单条消息查询、任务列表或任务取消接口。

### 公共对象（字段全部按实际响应）

```ts
type User = { id: number; email: string; created_at: string }
type Conversation = { id: number; title: string; created_at: string }
type Message = {
  id: number
  role: 'user' | 'assistant'
  content: string
  created_at: string
}
type ChatResult = Message & {
  model: string
  input_tokens: number
  output_tokens: number
  total_tokens: number
}
type ApiErrorBody = { code: string; message: string }
```

这些只是文档中的字段说明，不代表已创建 TypeScript 文件。历史消息不含模型名或 token 用量，不能假定刷新后还能显示这些元信息。

## 3. 准确 API 契约

### 3.1 公开接口

**`GET /healthz`**

- 200：`{"message":"pong"}`。
- 仅存活检查，不代表 AI Provider 可用。

**`GET /readyz`**

- 200：`{"code":"OK","message":"service ready"}`。
- 503：`{"code":"SERVICE_UNAVAILABLE","message":"service not ready"}`。
- 不能由 readiness 成功推断邮箱验证或 TTS 已开启。

**`POST /api/v1/auth/register`**

- 请求：`{email: string, password: string, verification_code?: string}`。
- 201：`User`，注册成功不返回 Token，之后进入登录页。
- 邮箱会去首尾空白并转小写；密码按 UTF-8 **字节数**校验，范围 8–72，不能用 JS 字符串长度直接代替，不自动 trim 密码。
- 验证码仅在服务端开启邮箱验证时必需；是六位数字字符串，保留前导零。成功验证会消费验证码。
- 400：`EMAIL_OR_PASSWORD_INVALID`、`INVALID_VERIFICATION_CODE`，或 JSON 解析的 `INVALID_REQUEST`。
- 409：`EMAIL_ALREADY_EXISTS`；413：`INVALID_REQUEST`；429：`TOO_MANY_REQUESTS`。
- 500：`INTERNAL_SERVER_ERROR`；503：`SERVICE_UNAVAILABLE`；504：`TIMEOUT`。
- 请求体上限 16 KiB。Handler 共用的请求结构包含验证码字段，但登录不会使用它。

**`POST /api/v1/auth/login`**

- 请求：`{email: string, password: string}`。
- 200：`{access_token: string, token_type: "Bearer"}`。
- 400：`INVALID_REQUEST`；401：`INVALID_CREDENTIALS`；413、429、500、限流设施异常 503。
- 邮箱规范化与密码字节范围同注册。当前启动配置 Token 有效期为 1 小时，但响应没有 `expires_in`。
- 登录错误的 401 是表单错误，不能触发“会话过期→强制跳登录”的循环。

**`POST /api/v1/auth/email-verification-codes`**

- 请求：`{email: string}`；200 不是成功状态，成功为 202：`{"message":"verification code accepted"}`。
- 响应不会返回验证码或倒计时；请求体上限 16 KiB，邮箱校验上限 254 字节。
- 400：`INVALID_REQUEST`；413：`INVALID_REQUEST`；429：`TOO_MANY_REQUESTS`；500、503、504。
- 默认配置：验证码有效 10 分钟，同邮箱 60 秒后可重发，最多 5 次验证尝试；这些值可配置，不是接口下发值。
- 邮箱验证关闭时发送接口仍存在，返回 503。503 也可能表示 SMTP/Redis 等故障，不能用一次 503 推断“无需验证码”。
- 同邮箱重发过快的业务 429 **没有** `Retry-After`；IP 限流中间件的 429 有该头。

### 3.2 当前用户与会话

**`GET /api/v1/users/me`**

- 200：`User`。无效 Token 或用户不存在返回 401 `UNAUTHORIZED`；其他异常 500。

**`POST /api/v1/conversations`**

- 请求：`{title: string}`；201：`Conversation`。
- title 去首尾空白后非空，最多 200 个 Unicode 码点；不存在自动创建“临时会话”的专用 Chat 路由。
- 400 `INVALID_REQUEST`、401、500。前端可明确发送默认标题“新会话”，不是后端自动补标题。

**`GET /api/v1/conversations?page=1&page_size=20`**

- 200：`{items: Conversation[], page: number, page_size: number}`。
- page 默认 1、有效范围 1–10000；page_size 默认 20、范围 1–100。
- SQL 按 `created_at DESC, id DESC` 排序，空列表为 `[]`。没有 total、has_more、updated_at。
- 400 `INVALID_REQUEST`、401、500。后续聊天不会使创建时间改变，不能把排序解释成“最近活跃”。

**`GET /api/v1/conversations/:id`**

- 200：`Conversation`；400 `INVALID_REQUEST`、401、404 `NOT_FOUND`、500。
- 不存在或不属于当前用户的会话均不可访问。

**`DELETE /api/v1/conversations/:id`**

- 204，无响应体；400、401、404、500。不能对成功结果强制解析 JSON。
- 页面须确认删除，成功后移除列表项并清理该会话的本地状态。

### 3.3 消息与普通 Chat

**`GET /api/v1/conversations/:id/messages?page=1&page_size=20`**

- 200：`{items: Message[], conversation_id: number}`，不返回 page、page_size 或 total。
- 分页参数默认值与有效范围同会话列表。
- SQL 按 `created_at ASC, id ASC` 排序：**第一页是最早消息，不是最近消息**。
- 400 `INVALID_REQUEST`、401、404 `NOT_FOUND`、500。
- MVP 建议以 page_size=100 顺序加载到末页，展示加载进度；切换会话可中止。只有短页才能确定已到末尾，整页需要再查下一页。去重以消息 ID 为准。到 page=10000 仍满页时须提示达到接口可读范围，不宣称加载完整。
- 完成生成后同步历史也必须考虑超过一页的会话；分页响应不能直接覆盖所有已加载消息。

**`POST /api/v1/conversations/:id/messages`**

- 请求：`{content: string}`；201：`Message`，role 为 `user`。
- 只保存用户消息，不调用模型。第一版无需为它单独增加页面或按钮。
- content 去首尾空白后非空，最多 20000 个 Unicode 码点；JSON 请求体上限 256 KiB。
- 400 `INVALID_REQUEST`、401、404 `NOT_FOUND`、413 `INVALID_REQUEST`、500。

**`POST /api/v1/conversations/:id/chat`**

- 请求：`{content: string}`；200：`ChatResult`，返回的是 assistant 消息。
- content 约束与消息创建一致，请求体上限 256 KiB；后端处理时间上限 2 分钟。
- 400 `INVALID_REQUEST`、401、404 `NOT_FOUND`、413、429；500 `INTERNAL_SERVER_ERROR`、503 `SERVICE_UNAVAILABLE`、504 `TIMEOUT`。
- 后端内部保存用户消息，再调用模型并保存 assistant 消息。**不能先 POST messages 再 POST chat**，否则会重复写用户消息。
- 失败可能已保存用户消息，不能自动重发 POST；先刷新历史核实，用户主动再次发送才是新请求。
- `model` 为只读返回信息，不支持请求参数选择模型。RAG 与天气 MCP 均由后端自动处理。

### 3.4 SSE Chat

**`POST /api/v1/conversations/:id/chat/stream`**

- 请求：`{content: string}`，JWT Header；请求体及 content 约束同普通 Chat。
- 正常响应 `Content-Type: text/event-stream`；后端设置 no-cache，处理时间上限 2 分钟。
- 认证、限流、JSON 解析、路径解析等流开始前错误，可能是普通 HTTP JSON 错误（400、401、413、429、503 等，写入设置异常可能 500）。先检查 HTTP 状态与 Content-Type，再交给 SSE 解析器。
- 业务错误进入 SSE 后通过 `error` 事件表达，HTTP 可能仍是 200，不能只检查 `response.ok`。

事件与 data JSON：

```text
event: delta
data: {"delta":"增量文字"}

event: error
data: {"code":"RESPONSE_NOT_COMPLETED","message":"response is not completed"}

event: done
data: {"id":123,"role":"assistant","created_at":"...","model":"...","input_tokens":10,"output_tokens":20,"total_tokens":30}
```

- `delta` 逐段追加；`done` **没有 content**，用已累积文本显示，再同步持久化历史。
- `error` code 包括 `INVALID_REQUEST`、`NOT_FOUND`、`SERVICE_UNAVAILABLE`、`RESPONSE_NOT_COMPLETED`、`TIMEOUT`、`RESPONSE_FAILED`、`ONDELTA_MISSED`、`INTERNAL_SERVER_ERROR`。
- 正常结束靠 `done`，不是模板的 `[DONE]`。收到 error 后进入失败状态；没有 done/error 就 EOF，属于中断，不能当成生成成功。
- 协议未提供事件 ID、恢复游标或取消 API。使用 AbortController 关闭请求是客户端中止，不保证消息回滚。
- 不自动重连或重放 POST，避免重复保存消息、重复计费。失败时部分正文标记“未完成”，最终以历史为准。
- 必须正确处理 UTF-8 跨块、SSE 事件跨块、CRLF、多条事件同块、多行 data；不能按网络 chunk 当成一条消息。
- 天气工具规划期间可能暂时没有 delta；没有首段输出不等于请求已卡死。

### 3.5 异步 ChatJob

**`POST /api/v1/conversations/:id/chat-jobs`**

- 请求：`{content: string}`；202：`{id: number, status: "pending"}`。
- JSON 请求体上限 256 KiB；当前 Job Service 仅检查 trim 后非空，没有普通 Chat 的 20000 码点上限。前端建议三种发送模式统一限制 20000 码点，属于 UI 约束。
- 400 `INVALID_REQUEST`、401、404 `NOT_FOUND`、413、429、500；限流设施故障 503。
- 先入数据库、再发布 RabbitMQ，不是原子事务。请求失败时可能存在孤立 pending Job；前端不能修复或假装任务已取消。
- 不要额外调用 messages 或 chat。Worker 负责创建/复用用户消息、生成并保存回复。

**`GET /api/v1/chat-jobs/:id`**

- 200 的完整字段：

```ts
type ChatJob = {
  id: number
  status: 'pending' | 'processing' | 'completed' | 'failed'
  assistant_message_id: number | null
  error_code: string | null
  created_at: string
  started_at: string | null
  finished_at: string | null
}
```

- 400 `INVALID_REQUEST`、401、404 `NOT_FOUND`、500。
- completed 后根据已有 conversation ID 刷新消息历史，定位 `assistant_message_id`；查询结果没有回复正文，也没有 conversation_id，前端须保留任务与会话的对应关系。
- failed 的业务 error_code 包括 `chat_failed`、`invalid_chat_result`、`chat_timed_out`、`over_max_retry_time`、`complete_failed`，未知值保留兜底。
- Worker 重试可令 processing 回到 pending，不能把状态视作严格单向递增。
- 建议每次查询完成后再延迟 2 秒查询，避免 setInterval 堆叠；连续查询到前端等待上限后显示“等待较久，可继续查询”，不伪造 failed。只重试 GET，不自动重复创建 Job。
- 无任务列表；建议在当前用户的 sessionStorage 保留已知 Job ID 与会话对应关系，刷新可恢复查询，退出时清理。离开页面停止当前轮询；重新进入可继续。

### 3.6 RAG 文档上传

**`POST /api/v1/rag/documents`**

- `multipart/form-data`，文件字段必须为 `document`。
- 201：`{filename: string, size: number, chunks: number}`。size 是文件字节数，chunks 是分块数量，不返回文档 ID 或分块内容。
- 文件只支持 `.md`、`.txt`（扩展名不区分大小写），内容必须有效 UTF-8、非空且可分块，文件上限 5 MiB；整个 multipart 请求上限 6 MiB。
- 400 `INVALID_REQUEST`；文件超限 413 `REQUEST_TOO_LARGE`；请求体超限 413 `INVALID_REQUEST`；401、429、500，限流设施异常 503。
- 上传同步完成解析、Embedding、存储和激活，201 后才显示成功；不虚构后台索引任务和百分比进度。
- **每个用户仅一份当前生效文档**：成功的新上传替换旧版本，适用于该用户所有会话，与当前 conversation ID 无关。
- 普通 Chat、SSE、ChatJob 自动使用当前文档，无额外 RAG 开关；响应不提供可核实的引用来源元数据。
- 不支持文档列表、下载、删除、选择多个文档。刷新后无法查询当前文件名，应显示“本页暂无上传记录”，不能断言“服务器没有文档”。
- 页面需要说明新上传会替换已有资料；保留本次上传回执即可，不伪装成完整知识库管理。

### 3.7 图片识别

**`POST /api/v1/images/recognitions`**

- `multipart/form-data`，字段为 `image`；200：`{class_name: string}`。
- 实际支持 JPEG、PNG；单边最多 8192 像素，总像素不超过 25000000；整个请求体最多 10 MiB，包含 multipart 开销。
- 400 `INVALID_REQUEST`、401、413 `INVALID_REQUEST`、500。图片接口没有专用业务限流中间件。
- 前端可采取 9 MiB 文件上限，为 multipart 留余量，但须标明这是 UI 限制，不能把 10 MiB 请求体上限直接当作文件上限。
- 只展示类别名，不能虚构概率、Top-K、检测框或图片历史记录。
- 预览使用 object URL，替换图片/卸载组件时释放；不能上传结束就释放仍在使用的预览资源。

### 3.8 TTS

**`POST /api/v1/tts/tasks`**

- 请求仅 `{text: string}`；202：`{task_id: string, status: "running" | "succeeded" | "failed"}`。Handler 返回 Provider 的有效状态，不要把创建响应类型写死为 running。
- 文本须有效 UTF-8，trim 后非空，最多 100000 个 Unicode 码点；JSON 请求体上限 1 MiB。
- 400 `INVALID_REQUEST`（包含文本过长）；413 `INVALID_REQUEST`（请求体超限）；401、429、500、502 `PROVIDER_ERROR`、503 `SERVICE_UNAVAILABLE`、504 `TIMEOUT`。

**`GET /api/v1/tts/tasks/:id`**

- task_id 是字符串，作为路径段时要编码。200：

```ts
type TTSTask = {
  task_id: string
  status: 'running' | 'succeeded' | 'failed'
  audio_url: string | null
  error_code: string | null
}
```

- running 时 audio_url、error_code 均为 null；succeeded 时 audio_url 非空、error_code 为 null；failed 时 audio_url 为 null、error_code 非空。
- 错误映射同 TTS 创建中的业务错误；GET 不消耗创建额度。Handler 未定义单独的 404 任务不存在映射，不套用 ChatJob 的行为。
- Provider 未配置时，创建和查询均可能 503。前端不能自行判定为永久免费关闭，也不要求用户输入 Provider 密钥。
- 建议串行轮询，终态停止；等待超时只停止本地等待，保留 task_id 支持继续查询。
- 成功后用 audio_url 展示原生音频控件，处理自动播放被浏览器阻止、临时 URL 过期或媒体加载失败；不把 JWT 添加到 Provider 音频 URL。
- 入口放在 assistant 消息上，后端只接收文本，不需要新增音色、语速或独立 TTS 工作台。
- 已接受边界：后端不持久化用户与 task_id 的严格归属，前端不声称已经补足服务端鉴权。

## 4. 限流、错误与前端状态策略

### 限流范围

- 普通 Chat、SSE、ChatJob 创建共用用户 Chat 额度，默认 10 次/分钟。
- RAG 上传默认 3 次/小时/用户；TTS 创建默认 5 次/小时/用户；任务查询不占创建额度。
- 注册默认 10 次/10 分钟/IP；登录默认 20 次/5 分钟/IP；验证码默认 10 次/小时/IP。
- 上述均可配置，前端不写死剩余配额；公开接口 IP 取直连 RemoteAddr，不信任 X-Forwarded-For。通过单个 Vite/反向代理接入的用户可能共享匿名额度，演示与测试避免密集注册。
- 中间件 429 的 `Retry-After` 是向上取整的秒数字符串。客户端读取头后显示冷却时间；缺失时显示通用限流提示，不编造精确剩余时间。验证码发送成功后的本地 60 秒防重复倒计时只能作为默认 UX。

### 统一错误语义

- 400：就地提示输入问题，优先识别 code，不依赖英文 message 文本判断业务。
- 401：登录页显示凭据错误；受保护请求清理登录态、停止流与轮询，跳到登录页并保留合法站内返回地址，避免多个请求重复弹窗。
- 404：会话/任务失效，允许返回列表或刷新，不能展示旧对象冒充当前结果。
- 409：邮箱已注册，提示登录。
- 413：上传文件或请求过大，结合当前页面说明限制。
- 429：按 Retry-After 冷却；没有该头也要可恢复，不自动重放写请求。
- 500：服务异常；502：上游失败；503：暂不可用或功能未配置；504：处理超时。
- 网络断开、非 JSON、错误响应字段缺失、浏览器 abort、SSE error 单独区分。用户主动停止不弹“服务器出错”。
- HTTP 失败、SSE error、任务 status=failed 共用错误呈现基础设施，但三者的触发路径不同。

### 生命周期和一致性

- 单个会话发送期间禁止再次发送，三种模式互斥；发起请求时捕获 conversation ID，迟到的响应不能写到新切换的会话。
- 切换会话/卸载时中止对应读取和 SSE、停止当前轮询；异步 Job 仍可能在服务端继续运行，返回时可恢复查询。
- 退出登录清理用户信息、Token、任务映射、文档回执与会话缓存；旧请求回调不能污染下一位用户。
- 普通 Chat 与 SSE 成功或失败后以服务端历史重新对齐；乐观显示的用户消息用本地临时 ID，不冒充数据库 ID。
- POST 无幂等键，断网后结果未知不能静默重试。不要把“再试一次”实现成隐式自动重复提交。

## 5. 模板与当前能力的差距

已阅读 v2 的 package.json、代理、路由、请求封装及五个页面；v1 路由与依赖作为补充核对，未发现需要超出 v2 页面范围的依据。

- **认证协议不兼容**：模板 `/user/login` 读取 token 和 status_code；现在为 `/auth/login` 与 access_token。模板验证码用 `/user/captcha` 和 captcha；现在为 `/auth/email-verification-codes` 与 verification_code，且验证可选。
- **开发端口冲突**：模板前端监听 8080，代理到 9090 并改写路径；当前 8080 已被 Go 占用，新前端建议 5173，保留 `/api/v1` 路径代理到 8080。
- **会话流程不同**：模板通过 send-new-session 发送时建会话，用 sessionId、question；当前必须先创建会话，再向 URL 中的 id 发送 content；补齐分页与删除。
- **历史结构不同**：模板 POST history 并解析 is_user；现在 GET messages，role 为 user/assistant，有持久化消息 ID，且从旧到新分页。
- **SSE 协议不同**：模板手写解析 data、sessionId 和 `[DONE]`；现在命名事件 delta/error/done，结构化 JSON，须单独处理流内失败与提前断流。
- **模型选择不可保留**：模板 modelType 选择器没有对应当前 API；返回的 model 仅可作为回复详情。
- **ChatJob 是新增接入点**：模板没有可直接复用的 Job 状态流程，需要新增异步发送模式、任务状态与受控轮询。
- **RAG 上传要改字段与语义**：模板 `/file/upload` 的 file 改为 `/rag/documents` 的 document，明确单用户当前文档替换，而非多文档知识库。
- **图片页面范围可借鉴**：multipart image 和 class_name 的概念仍成立，但实际路径是 `/images/recognitions`，补尺寸、格式、请求大小与预览生命周期。
- **TTS 状态不同**：模板 `/AI/chat/tts/query?task_id=...` 使用 Success/Running/Created 与 task_result；现在路径含 task_id，状态小写，音频字段 audio_url。
- **Markdown 存在真实安全问题**：模板 AIChat.vue 用正则替换字符串，再交给 v-html，没有清理原始 HTML。新实现必须使用成熟 Markdown 库与 HTML sanitizer。
- **视觉与职责均需重做**：模板大面积紫色渐变、动画、大圆角及单个 AIChat 文件混合上传/流/会话/音频不符合本项目目标，只参考页面和功能范围。
- 根 README 中的多模型、旧框架、旧存储架构是项目背景，不转化为本轮额外前端功能。

## 6. 技术栈建议（待确认）

- Vue 3 + Vite + TypeScript，使用单文件组件与 `<script setup lang="ts">`。
- Vue Router 管理公开页与受保护工作区；Element Plus 负责表单、按钮、对话框、上传反馈，按钮配 `@element-plus/icons-vue` 图标。
- Axios 处理普通 JSON、multipart 和任务查询；SSE 使用原生 fetch + AbortController + `eventsource-parser`，复用同一 Token 获取与错误归一化函数。
- Vitest + Vue Test Utils 覆盖关键状态和交互；Playwright 做真实浏览器验收；ESLint 与 vue-tsc 负责静态检查。
- Markdown 使用 markdown-it，明确关闭原始 HTML；渲染结果经过 DOMPurify 后，仅在专用 MessageContent 组件中使用 v-html。用户输入普通文本用 Vue 插值显示。清理后不再拼接未经处理的 HTML。
- CSS 使用普通样式与少量颜色/间距变量；不引入 Tailwind、大型设计系统、SSR、Node 服务、复杂主题或微前端。
- 初期不引入 Pinia。登录态用一个小型响应式 composable 共享，页面状态就近保存。只有出现明确的跨页状态复杂度后再讨论。
- 建议使用 npm 和提交 package-lock.json。依赖版本在阶段 1 核对兼容性并锁定，不直接沿用模板的旧版本。

官方资料已核对：

- [Vite 入门与 Node 要求](https://vite.dev/guide/)：当前文档要求 Node 20.19+ / 22.12+；本机已有 Node，阶段 1 安装时确认所选依赖的 engines 和构建结果，不改全局运行时。
- [eventsource-parser](https://github.com/rexxars/eventsource-parser)：可逐块输入解析，区分 event 与 data；只负责解析，重试和连接生命周期由本项目控制。
- [markdown-it HTML 选项](https://markdown-it.github.io/markdown-it/interfaces/MarkdownItOptions.html) 与 [DOMPurify](https://github.com/cure53/DOMPurify)：明确关闭源 HTML，并清理最终输出。

### 登录态建议

- MVP 默认 sessionStorage 保存 access_token，内存保存 User；刷新后调用 `/users/me` 验证并恢复，关闭标签页后通常需重新登录。
- 这是降低演示复杂度的取舍，不具备 HttpOnly Cookie 的隔离性；仍须做好 Markdown 安全处理，且 Token 不进入 URL、日志或截图。
- 后端没有 refresh/logout API。退出为本地清理；过期后重新登录，不模拟刷新 Token。
- `/users/me` 网络失败或 503 与真正 401 分开处理，展示重试，不把所有失败当成登录失效。
- 邮箱验证通过前端公开配置 `VITE_EMAIL_VERIFICATION_ENABLED` 控制初始表单，与部署端开关保持一致；只放布尔开关，绝不放 SMTP/Provider/JWT 密钥。若注册仍收到 INVALID_VERIFICATION_CODE，展示验证码输入与提示，不自动反复提交。

### 接入与部署建议

- Vite 开发地址 `http://127.0.0.1:5173`；`/api/v1`、`/healthz`、`/readyz` 原路径代理至 `http://127.0.0.1:8080`，不做 `/api` 二次重写。
- 当前 Router 没有配置跨域中间件，浏览器统一请求同源代理即可接入，不需要为开发而改后端 CORS。
- 前端静态产物部署时也使用同源反向代理，API 路径不变；SSE 禁止响应缓冲，代理超时覆盖后端两分钟处理窗口；SPA 深层路由需 index.html fallback。
- 具体 Nginx/容器交付放到最终演示阶段，优先增加 frontend 内的配置或单独的前端编排文件，不提前改定版 compose.yaml。
- 创建依赖前补充 Git 忽略：frontend/node_modules、dist、coverage、测试报告与本地环境文件。另需让后端 Docker 构建上下文排除 frontend，防止 COPY 将 node_modules/报告带进后端镜像；属于仓库配置，阶段 1 说明后处理，不改 Go 行为。

## 7. 页面结构与组件数据流

第一版是工作型应用，登录后直接进入对话工作区，不需要营销页或独立大卡片菜单页。

建议路由：

```text
/login                   登录
/register                注册，可选邮箱验证码
/chat                    会话列表与未选会话状态
/chat/:conversationId    当前会话、历史、普通/流式/异步发送
/images                  图片识别
/                        根据登录态进入 /chat 或 /login
其他路径                  简洁的未找到页面
```

固定侧栏承载功能导航，在对话页内展示会话列表；顶部工具栏承载当前页面标题、当前用户菜单和退出；主内容区域展示消息或图片识别。移动端侧栏收为抽屉，顶部保留菜单按钮。

- **AppShell**：导航、顶部工具栏和 RouterView；不负责直接发 Chat 请求。
- **LoginView / RegisterView**：表单收集与校验，调用 auth API；登录成功写 Token、获取 User、进入安全的站内返回地址。
- **ChatView**：协调当前会话、历史与发送状态。ConversationList 发出选中/创建/删除意图；MessageList 展示消息；ChatComposer 发出文本与发送模式。
- **MessageContent**：唯一 Markdown 渲染边界；**MessageTTS** 负责某条回复的任务和音频状态。
- **RagUploadPanel**：从对话工具栏打开抽屉，说明替换语义并显示上传回执；不承担虚构的文档列表。
- **JobStatus**：显示 pending/processing/completed/failed 和继续查询操作，与会话绑定。
- **ImageRecognitionView**：文件选择、预览、请求状态与分类名，保持独立页面。
- **api/**：URL、请求 DTO、响应 DTO 和具体请求；**composables/**：有明确生命周期的页面行为；组件通过 props/emits 协作，避免所有逻辑堆在 ChatView。

概念数据流：

```text
用户操作 → 页面/组件 → composable（loading/取消/结果状态）
                       → api（Axios 或 fetch SSE）
                       → Vite 同源代理 → Go Handler
响应/事件 → DTO 与错误处理 → 页面状态 → Vue 更新视图
```

建议按阶段实际创建目录，不一次生成大量空文件：

```text
frontend/
  src/
    api/          # 普通请求、SSE、各业务 DTO 与函数
    components/   # 可复用展示组件
    composables/  # useAuth、useChatStream、useJobPolling 等，按需添加
    layouts/      # AppShell
    router/
    views/
    styles/
  tests/          # 测试夹具、跨模块测试与 e2e，单测也可贴近源码
  package.json
  package-lock.json
  vite.config.ts
  README.md
```

### 视觉与交互约束

- 中性底色、小面积强调色、结构分隔线；不做大面积紫色渐变、光球、悬浮卡片墙，卡片圆角不超过 8px。
- 长标题截断并可查看完整内容；长英文/链接可换行，代码块在自身区域横向滚动，不撑开页面。
- Loading 按钮保持尺寸，上传和轮询提供可扫描状态；错误保留用户输入，空状态给明确下一步。
- 输入区不覆盖最后一条消息；用户向上阅读时不强制滚到底部，有新内容时提供提示。
- 桌面优先，兼顾 1440×900、390×844、窄屏 320px 与 768px 平板；检查软键盘/视口变化下的输入区可达性。
- 图标按钮有名称或 tooltip，输入有 label；支持键盘与焦点恢复。中文输入法组合期间不把 Enter 当作发送。

## 8. 分阶段开发与验收

每阶段先讲页面定位、数据流、组件关系与大致写法，再实施。用户 review 并确认后才能进入下一阶段；业务代码须在用户明确“你来写”时直接修改。基础脚手架、测试与配置可以协助编写，但本轮仍按用户要求等待阶段 1 确认。

### 阶段 1：工程基础与布局骨架

目标：建立能启动、检查、构建、在浏览器访问的最小前端；本阶段不做完整登录、Chat 或上传。

- 创建 Vue/Vite/TypeScript 工程，接入 Router、Element Plus 与图标，配置 ESLint、Vitest、vue-tsc、Playwright。
- 配置同源代理、npm 脚本、锁文件、忽略规则与前端运行说明；普通 Axios 客户端与最小错误类型先落地，认证和 SSE 的业务逻辑后续增加。
- 创建 AppShell、桌面侧栏/顶部工具栏、移动抽屉和简单的工作区占位内容，不伪装成功能已完成；临时骨架入口后续接入鉴权。
- 通过代理调用 health/readiness 验证链路；状态放在简洁开发验收入口，不把基础设施细节堆入正式用户流程。
- 数据流：浏览器导航 → Router → AppShell → 占位视图；健康检查 → api/client → Vite → Go。
- 验收：lint、test、typecheck、build 全部通过；Vitest 检查真实错误归一化行为；Playwright 检查导航、代理返回、桌面/移动布局与控制台错误。不写仅验证框架存在的空洞测试。
- 完成后讲清 package.json、main.ts、Router、AppShell、api/client 与代理的关系，再交用户 review。

### 阶段 2：登录、注册与当前用户

- 完成登录/退出、sessionStorage、/users/me、路由鉴权与刷新恢复。
- 注册支持验证码开关、六位输入、发送状态与重发提示；注册成功进入登录页。
- 验收：错误密码 401、注册 409、验证码错误/不可用、受保护接口 401、多请求同时过期、页面刷新与退出清理；检查桌面和手机表单。
- 真实邮件发送需用户明确同意测试邮箱与发信动作；先用 Playwright mock 验证交互，不因页面测试自动向他人发送邮件。

### 阶段 3：会话与普通 Chat

- 实现会话创建、分页列表、切换、删除、完整历史加载与普通回复；为后续两种模式保留简洁入口。
- 完成安全 Markdown 展示、输入状态、消息失败与服务端历史对齐。
- 验收：空会话、超过一页历史、快速切换、删除当前会话、中文输入法、重复点击、失败后用户消息仍持久化、XSS 样例、长文本/代码块布局。

### 阶段 4：SSE 流式 Chat

- 实现 POST SSE、JWT、delta/error/done、停止接收、异常断流与切换隔离。
- 验收：真实增量输出、天气提问等待、流前 HTTP 错误、200 内 error、无 done EOF、跨 UTF-8/事件分块、401/429、取消后回调隔离；不自动重连。
- 网络异常与边界事件可用受控测试服务/浏览器路由夹具复现；真实后端验证增量确实逐步抵达，不能用最终一次性响应替代流验证。

### 阶段 5：异步 ChatJob

- 加入异步发送模式、任务状态、串行轮询、用户与会话隔离的任务映射、completed 后刷新消息。
- 验收：pending/processing/completed/failed、processing 回 pending、慢任务暂停与继续、刷新恢复、离页停止轮询、不重复创建 Job。
- 不实现已接受边界之外的队列修复、服务端取消或平台任务中心。

### 阶段 6：RAG 文档上传

- 对话工具栏增加上传抽屉，校验扩展名/UTF-8/大小，显示处理中与 filename/size/chunks 回执，明确替换影响全部会话。
- 验收：UTF-8 中英文文档、错误字段防回归、无效编码、空内容、400/413/429、上传后真实文档问答、新上传替换与跨会话作用。
- 使用专用演示账号与样例文档，避免替换用户已有资料；当前轮次不会执行上传。

### 阶段 7：图片识别

- 独立图片页，JPEG/PNG 选择、预览、上传、单分类结果与资源清理。
- 验收：真实有效图片、损坏/不支持文件、超尺寸/超请求大小、失败重选、预览替换、移动布局，不能显示不存在的置信度。

### 阶段 8：TTS 播放

- assistant 消息增加语音按钮，创建任务、轮询、音频控件及播放错误处理。
- 验收：running/succeeded/failed、503 未配置或不可用、502/504、429、主动离开清理、自动播放受限、临时 URL 失败及真实可听音频。
- 模型与语音调用使用少量验收请求，不循环消耗额度来模拟错误。

### 阶段 9：演示收尾与全链路验收

- 补齐整体错误/空状态/加载反馈，复核移动布局、键盘与长内容展示。
- 完成独立前端静态交付/同源代理说明、环境开关说明、演示步骤和必要截图，不改参考项目。
- 真实演示：登录 → 新建会话 → 普通/SSE/异步回复 → 文档问答 → 图片分类 → 语音播放 → 刷新历史 → 退出。
- Playwright 桌面/移动流程检查网络路径、Authorization、multipart 字段、SSE 终态、轮询停止、错误提示和控制台；mock 错误场景与真实后端验收分别记录，不把 mock 通过说成真实 Provider 通过。
- 前端所有检查通过后再交用户决定是否提交，避免把敏感配置、Token、真实邮箱或带凭据音频 URL 带入仓库和演示素材。

### 每阶段统一完成条件

计划中的 npm 命令为：

```sh
npm run lint
npm run test -- --run
npm run typecheck
npm run build
npm run test:e2e
```

阶段 1 将建立这些脚本。之后每阶段运行 lint、相关测试、类型检查、构建和适用的 Playwright 浏览器验证；有页面就检查桌面和移动端。有后端接入的阶段记录实际网络与响应，有失败说明具体阻碍，不能以构建成功代替浏览器验收。本轮只有文档变更，尚无 npm 工程，这些检查当前不适用。

## 9. 本次讨论点与后续协作

建议确认三个默认决定：

1. 采用 Vue 3/Vite/TypeScript/Router/Axios/Element Plus/Vitest/Playwright，SSE 用 fetch + eventsource-parser，暂不加 Pinia。
2. 登录后直接到对话工作区；图片识别独立页；RAG 用上传抽屉，TTS 放在回复上；三种 Chat 为同一输入框的发送模式。
3. 下一步仅做阶段 1 的脚手架、代理、基础请求处理与布局骨架；认证从阶段 2 开始，每阶段 review 后再继续。

确认后再编码。后续讲解保持中文，以后端开发者熟悉的“请求、响应、状态、职责”说明前端概念；简单问题简短回答，Review 先列有证据的行为问题并给文件与行号，不为追求工程化扩大需求。
