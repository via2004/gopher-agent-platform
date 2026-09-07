# GopherAI TTS 模块开发计划

## 目标

为当前 Backend 增加一个最小、完整的文本转语音（Text-to-Speech，TTS）能力：

```text
用户提交文本
  -> Backend 调用百度长文本语音合成 API
  -> 百度返回 task_id
  -> 用户使用 task_id 查询任务
  -> 合成完成后得到临时音频 URL
```

本轮只实现 GopherAI-v2 已有的异步文本转语音场景，不扩展成音频工作流、音色管理平台或语音对话系统。

## 模板依据与当前差距

GopherAI-v2 已经存在：

- 创建 TTS 任务接口。
- 查询 TTS 任务接口。
- 百度智能云 Access Token 获取。
- 百度长文本语音合成任务创建与查询。

当前 Backend 已经存在完整的认证、Handler -> Service -> Platform 分层和环境变量配置，但尚无 TTS 相关代码与路由。

模板只用于确认功能范围，不直接复制其实现。模板创建任务时把 `text` 发送为字符串；百度当前官方长文本 API 要求 `text` 为字符串数组，因此本项目按官方协议实现。

## 模块主要作用

TTS 的输入是文本，输出是可以播放的音频。它与当前其他 AI 能力的区别如下：

```text
Chat:  文本 -> 文本
Image: 图片 -> 分类标签
RAG:   文档 + 问题 -> 相关上下文
TTS:   文本 -> 音频
```

长文本合成不是立即返回音频，而是一个异步 Provider 任务：

```text
POST 创建任务 -> task_id + running
GET 查询任务  -> running / succeeded / failed
succeeded     -> audio_url
```

这里的异步由百度服务提供，不经过本项目 RabbitMQ。客户端提交任务后不需要保持 HTTP 连接，只需轮询查询接口。

## Provider 与协议决策

第一版使用百度智能云长文本在线合成 API，与 GopherAI-v2 保持一致：

```text
nei rongOAuth Token: POST https://aip.baidubce.com/oauth/2.0/token
创建任务:    POST https://aip.baidubce.com/rpc/2.0/tts/v1/create
查询任务:    POST https://aip.baidubce.com/rpc/2.0/tts/v1/query
```

使用 Go 标准库 `net/http` 和 `encoding/json`，不额外引入百度 SDK。

固定合成参数：

```text
lang=zh
format=mp3-16k
voice=4194
speed=5
pitch=5
volume=5
enable_subtitle=0
```

HTTP API 第一版只让用户传入文本，不开放音色、格式、音调等参数，避免把 Provider 参数直接泄漏到业务接口。

## 冻结的功能范围



### 本轮实现

- 使用 API Key 和 Secret Key 获取百度 Access Token。
- 缓存 Access Token，并在接近过期时重新获取。
- 创建一个长文本语音合成任务。
- 查询单个任务状态。
- 将百度状态映射成项目自己的稳定状态。
- 合成成功后返回临时音频 URL。
- TTS 未配置时不影响 Backend 启动，对接口返回 503。
- Handler、Service、Provider Client、配置和测试完整闭环。
- Docker Compose 透传 TTS 配置。
- 使用独立 Redis 配额限制用户创建 TTS 任务，不占用 Chat 或 RAG 配额。



### 本轮不实现

- 不把 TTS 任务再次放入 RabbitMQ。
- 不新增 `tts_tasks` 数据库表或 migration。
- 不在本地保存任务历史。
- 不下载、代理或永久保存音频文件。
- 不提供音频流式播放。
- 不提供音色列表、试听或用户自定义音色。
- 不实现语音识别（ASR）或语音对话。
- 不自动把每条 Chat 回复转成语音。
- 不提供批量任务查询接口。
- 不抽象成动态多 TTS Provider 平台。



## HTTP 接口

两个接口都需要 JWT 鉴权。

### 创建任务

```http
POST /api/v1/tts/tasks
Content-Type: application/json
Authorization: Bearer <token>

{
  "text": "欢迎使用 GopherAI"
}
```

成功响应：

```http
HTTP/1.1 202 Accepted

{
  "task_id": "provider-task-id",
  "status": "running"
}
```

输入规则：

- 去掉首尾空白后不能为空。
- 必须是合法 UTF-8 文本。
- 最多 100000 个 Unicode 字符，与 Provider 官方上限一致。
- Handler 请求体最大 1 MiB。



### 查询任务

```http
GET /api/v1/tts/tasks/:id
Authorization: Bearer <token>
```

合成中：

```json
{
  "task_id": "provider-task-id",
  "status": "running",
  "audio_url": null,
  "error_code": null
}
```

合成成功：

```json
{
  "task_id": "provider-task-id",
  "status": "succeeded",
  "audio_url": "https://example.com/result.mp3",
  "error_code": null
}
```

合成失败：

```json
{
  "task_id": "provider-task-id",
  "status": "failed",
  "audio_url": null,
  "error_code": "provider_task_failed"
}
```

百度返回的音频 URL 只有有限有效期；第一版原样返回，不承诺永久可用。

## 状态模型

项目只暴露三个状态：

```go
type Status string

const (
    StatusRunning   Status = "running"
    StatusSucceeded Status = "succeeded"
    StatusFailed    Status = "failed"
)
```

Provider 状态映射：

```text
Baidu Running -> running
Baidu Success -> succeeded
Baidu Failure -> failed
未知状态      -> Provider 响应无效
```

这不是本地持久化状态机。每次查询都以百度返回的状态为准，因此本轮不需要数据库事务或 migration。

## 目标架构

```text
HTTP Client
    |
    v
TTS Handler
    |
    v
tts.Service
    |
    v
tts.Provider interface
    |
    v
platform/baidutts.Client
    |                  |
    v                  v
OAuth Token API    Long TTS API
```

预计代码结构：

```text
internal/tts/
    model.go       项目自己的 Task 和 Status
    errors.go      业务错误
    repository.go  Provider 接口
    service.go     输入校验与业务编排

internal/platform/baidutts/
    client.go      Token、创建任务、查询任务
    errors.go      Provider 适配错误

internal/httpapi/
    tts_handler.go
```

`tts.Provider` 由业务使用方定义，百度 Client 实现它。第一版不为 Token、JSON 编解码等内部细节继续拆 interface。

## 核心接口草案

```go
type Provider interface {
    Create(context.Context, string) (*Task, error)
    Get(context.Context, string) (*Task, error)
}

type Service struct {
    provider Provider
}

func (s *Service) Create(ctx context.Context, text string) (*Task, error)
func (s *Service) Get(ctx context.Context, taskID string) (*Task, error)
```

用户身份由认证中间件校验，但本轮不保存 TTS 任务归属。`task_id` 是 Provider 返回的不可预测标识，客户端需要自行保存。任务列表和严格的用户归属校验需要本地持久化，不属于模板现有范围。

## 配置

新增环境变量：

```env
BAIDU_TTS_API_KEY=
BAIDU_TTS_SECRET_KEY=
BAIDU_TTS_BASE_URL=https://aip.baidubce.com
BAIDU_TTS_TIMEOUT_SECONDS=10
TTS_RATE_LIMIT=5
TTS_RATE_WINDOW_SECONDS=3600
```

行为规则：

- API Key 和 Secret Key 都为空：TTS 禁用，Backend 仍正常启动，TTS 接口返回 503。
- 只配置其中一个：启动失败，提示配置不完整。
- 两者都配置：初始化百度 TTS Client。
- Base URL 主要用于测试和兼容代理，默认使用百度官方地址。
- TTS 限流只作用于创建任务接口；查询轮询不消耗创建额度。



## Access Token 管理

百度 API Key 和 Secret Key 不直接用于每次合成请求，而是先交换 Access Token：

```text
API Key + Secret Key
  -> OAuth Token API
  -> access_token + expires_in
  -> TTS create/query 请求携带 access_token
```

Client 在内存中缓存 Token 与过期时间：

- Token 未过期时直接复用。
- 接近过期时重新获取。
- 并发刷新由互斥锁收敛，避免同时请求大量 Token。
- 进程重启后重新获取，不把 Token 写入数据库或 Redis。
- 日志不得打印 API Key、Secret Key、Access Token 或带 Token 的完整 URL。



## 错误边界

业务错误：

```text
ErrInvalidText
ErrTextTooLong
ErrInvalidTaskID
ErrNotConfigured
```

Provider 适配错误：

```text
Token 获取失败
Provider HTTP 请求失败
Provider 返回非 2xx
Provider JSON 无效
Provider 返回业务错误码
Provider 返回未知任务状态
```

Handler 映射：

```text
输入无效                         -> 400 Bad Request
TTS 未配置                       -> 503 Service Unavailable
context deadline exceeded        -> 504 Gateway Timeout
百度鉴权、限流或服务调用失败      -> 502 Bad Gateway
其他内部错误                     -> 500 Internal Server Error
```

任务本身进入 `failed` 是一次成功的查询响应，返回 HTTP 200 和 `status=failed`，不映射成 5xx。

## 测试策略



### Service 单元测试

- 空文本、超长文本和合法文本。
- 空 task ID。
- Provider 错误原样传递。
- Provider 返回 nil 或无效 Task 时拒绝结果。



### Provider Client 测试

使用 `httptest.Server`，不访问真实百度服务：

- OAuth 请求参数正确。
- Token 会被缓存复用。
- 创建请求的 `text` 是数组，固定参数正确。
- Running、Success、Failure 状态映射正确。
- 非 2xx、错误码、无效 JSON、响应过大和超时。
- 请求取消能够传播到 Provider。
- 错误信息不包含 Secret Key 或 Access Token。



### Handler 与 Router 测试

- 创建接口返回 202。
- 查询三个状态的响应字段正确。
- 400、502、503、504 错误映射正确。
- 未登录用户返回 401。
- Router 正确注册两个接口。



### 真实验收

配置百度凭据后：

```text
提交一段中文文本
  -> 获得 task_id
  -> 轮询状态
  -> succeeded
  -> audio_url 可以下载或播放
```

没有百度凭据时，只进行 `httptest` 自动化测试，并验证 Backend 可以正常启动、TTS 接口返回 503。

## 开发阶段



### 阶段 0：范围与接口确认

状态：`completed`

- 确认百度长文本异步合成与模板功能一致。
- 冻结两个 HTTP 接口、三个业务状态和固定合成参数。
- 确认不使用 RabbitMQ、不新增 migration、不保存音频。



### 阶段 1：领域模型与 Service

状态：`completed`

完成记录：

- 新增 `internal/tts` 的 Task、Status、Provider 和 Service。
- 未配置 Provider 时统一返回 `ErrNotConfigured`，不影响 Backend 其他功能。
- 创建任务会校验 UTF-8、空文本和 100000 字符上限，查询会校验 task ID。
- Service 会拒绝空结果、未知状态和字段组合不合法的 Provider Task。
- 单元测试覆盖合法状态、输入边界、错误传播、Context 取消和异常 Provider 结果。
- 新增 `internal/tts`。
- 定义 Task、Status、Provider 和业务错误。
- 完成输入校验和 Service 单元测试。



### 阶段 2：百度 Provider Client

状态：`completed`

完成记录：

- 新增 `internal/platform/baidutts.Client`，实现 OAuth Token、创建任务和查询任务。
- Access Token 在内存中缓存，接近过期时刷新，并发首次调用只执行一次 Token 请求。
- 创建请求按官方协议发送 `text` 数组和冻结的语音参数。
- Provider 的 Running、Success、Failure 已映射成领域 Task。
- 所有请求支持 Context 取消、独立超时和 1 MiB 响应上限。
- 错误不会包含 API Key、Secret Key 或 Access Token。
- `httptest.Server` 测试覆盖协议字段、缓存并发、刷新、状态映射与异常响应。
- 实现 OAuth Token 获取与内存缓存。
- 实现创建和查询请求。
- 实现响应大小限制、超时、取消和错误转换。
- 使用 `httptest.Server` 补齐测试。



### 阶段 3：Handler 与 Router

状态：`completed`

完成记录：

- 新增创建与查询 TTS 任务的 Handler，并注册到 JWT 鉴权路由组。
- 创建接口限制 1 MiB JSON 请求体，成功返回 `202 Accepted`。
- 查询接口稳定返回 `audio_url` 和 `error_code`，未产生的字段为 `null`。
- 业务输入、未配置、超时、Provider 和内部错误已统一映射为 HTTP 错误。
- Handler 和 Router 测试覆盖鉴权、请求解析、三种状态、错误映射和 nil 结果。

- 实现创建和查询 Handler。
- 完成 HTTP 错误映射。
- 注册鉴权路由并补测试。



### 阶段 4：Bootstrap 与配置

状态：`completed`

完成记录：

- 新增可选 `ttsFeature`，统一组装 Provider、Service、Handler 和 Redis Limiter。
- API Key 与 Secret Key 都为空时禁用 TTS，但 Backend 保持可启动。
- 只配置一个百度凭据时启动失败，避免以错误配置运行。
- 完整凭据会创建百度 TTS Client，并读取 Base URL 与请求超时。
- 本地和 Compose 配置示例已增加百度凭据、超时和 TTS 限流参数。
- `compose.yaml` 已通过现有 `env_file: .env.compose` 向 Backend 透传这些配置。
- 未设置 TTS 限流配置时默认每个用户每小时最多创建 5 个任务。

- 构造可选 TTS Feature。
- 更新 `.env.example`、`.env.compose.example` 和 Compose。
- 验证无凭据时不影响现有功能。



### 阶段 5：真实 E2E 与收尾

状态：`completed`

完成记录：

- 使用真实百度凭据成功获取 OAuth Access Token，凭据具备语音合成权限。
- 真实创建接口返回 `Created`；Client 已兼容 `Created` 和官方示例中的 `Running`，统一映射为 `running`。
- Backend 创建接口返回 `202 Accepted`，查询从 `running` 进入 `succeeded`。
- 成功响应包含音频 URL，实际下载返回 18909 字节的 16 kHz 单声道 MP3。
- `go test ./...`、`go vet ./...`、竞态测试和 `git diff --check` 通过。
- Backend README 已增加 TTS 配置和接口使用说明。

- 运行 `go test ./...` 和 `go vet ./...`。
- 有凭据时完成真实创建、轮询和音频访问。
- 更新 Backend README 的 TTS 使用说明。
- 做一次模块级 code review 后提交。



## 官方参考

- [百度长文本在线合成 API](https://cloud.baidu.com/doc/SPEECH/s/ulbxh8rbu)
- [百度语音技术鉴权认证](https://cloud.baidu.com/doc/SPEECH/s/cm8sn2bii)
- [百度语音技术错误码](https://cloud.baidu.com/doc/SPEECH/s/Zlbxew2qk)
