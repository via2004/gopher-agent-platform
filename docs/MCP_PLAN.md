# GopherAI MCP 工具调用实施计划

## 目标

在现有 Chat、SSE 和 ChatJob 调用链上增加一个最小、完整的 MCP Tool Use 能力：

```text
用户询问天气
  -> 模型判断需要调用 get_weather
  -> Backend 通过 MCP Client 调用 MCP Server
  -> MCP Server 查询天气 API
  -> Backend 将工具结果交回模型
  -> 模型生成最终回答
```

本轮只实现 GopherAI-v2 已有的天气工具场景，不把项目扩展成通用工具平台、多 Agent 平台或工作流系统。

## 模板依据与当前差距

GopherAI-v2 中已经存在：

- 独立 MCP HTTP Server。
- `get_weather` 工具。
- MCP Client。
- 模型判断工具调用、执行工具、再次调用模型生成最终回答的流程。
- 普通和流式 MCP 对话。

当前 backend 已经存在：

- OpenAI Responses API 普通和流式调用。
- Chat、SSE 和 ChatJob 共用的 `chat.Service`。
- RAG prompt 注入。
- `model_calls` 状态和 token usage 记录。
- Docker Compose 部署。

当前缺少：

- MCP Server 和 MCP Client。
- 模型可读取的工具定义。
- Responses API 结构化 function call 解析。
- 工具执行结果回填模型的 Agent Loop。

## 协议与依赖决策

使用官方 Go SDK：

```text
github.com/modelcontextprotocol/go-sdk v1.7.0
```

该版本支持 MCP `2026-07-28`，同时由 SDK 处理协议协商和兼容细节。

传输方式使用 Streamable HTTP：

```text
http://mcp-server:8081/mcp
```

GopherAI-v2 使用的是旧版会话协议，会显式执行 `initialize/initialized`。MCP `2026-07-28` 已使用无会话的请求/响应核心，因此模板只作为功能参考，不复制它的连接生命周期和手写 JSON Tool Call 解析。

## 核心概念边界

### OpenAI Tool Calling

负责模型与 Backend 之间的结构化协商：

```text
Backend 把工具定义发给模型
模型返回工具名称和参数
```

### MCP

负责 Backend 与工具服务之间的标准通信：

```text
Backend MCP Client
  -> tools/list
  -> tools/call
MCP Server
```

模型不会直接连接 MCP Server，实际网络请求和权限控制始终由 Backend 负责。

## 冻结的功能范围

### 本轮实现

- 一个 MCP Server。
- 一个只读工具：`get_weather`。
- 一个 MCP Client/Tool Executor。
- 模型自动决定是否调用天气工具。
- 最多一轮工具调用。
- 普通 Chat、SSE 和 ChatJob 共用同一 Agent Loop。
- 两次模型请求的 token usage 汇总到当前逻辑 `model_call`。
- Compose 中增加 MCP Server。

### 本轮不实现

- 动态工具注册和数据库工具表。
- 工具市场或插件系统。
- 多 MCP Server 管理。
- 多 Agent 协作。
- 工作流引擎。
- 并行工具调用。
- 连续多轮工具调用。
- 写文件、发邮件、执行 Shell 等有副作用的工具。
- OAuth、用户级 MCP 凭证和工具授权页面。
- MCP Resources、Prompts、Tasks、Apps。
- Kubernetes 或独立微服务拆分。

## 冻结的行为规则

1. `MCP_SERVER_URL` 为空时不启用 Tool Use，现有 Chat 行为保持不变。
2. `MCP_SERVER_URL` 有值时，Backend 只允许调用 `get_weather`。
3. 工具参数只接受一个非空 `city` 字符串，并限制长度。
4. 每次用户请求最多执行一个工具调用；模型返回多个调用时直接失败。
5. 模型返回未知工具名时直接失败，不把它转发给 MCP Server。
6. 天气 API 或 MCP 调用失败时，当前 Chat 请求失败，并按现有逻辑结束 `model_call`。
7. 工具调用只保存最终 assistant 消息，不额外保存内部 function call 和 tool output。
8. 一次用户请求仍对应一条 `model_call`；发生工具调用时汇总规划请求和最终回答请求的 token。
9. RAG 仍在模型调用前完成 prompt 注入，MCP 不修改 RAG 文档和 Redis 数据。
10. Chat、SSE 和 ChatJob 的 HTTP 接口保持不变，不增加 `model_type` 字段。

## 目标架构

```text
HTTP Handler / ChatJob Worker
            |
            v
       chat.Service
            |
            v
  agent.Client (实现 llm.ModelClient)
       |                 |
       v                 v
OpenAI Tool Model    MCP Tool Executor
       |                 |
       v                 v
Responses API       MCP Server
                         |
                         v
                    Weather API
```

`chat.Service` 仍然只依赖 `llm.ModelClient`。Tool Loop 收在 `agent.Client` 内，避免普通 Chat、SSE 和 ChatJob 分别实现工具逻辑。

## 预计代码边界

```text
cmd/mcpserver/
    MCP Server 入口和 HTTP Server 生命周期

internal/agent/
    单轮 Tool Loop
    实现现有 llm.ModelClient

internal/llm/
    通用 ToolDefinition、ToolCall、ToolOutput
    ToolModel 接口

internal/platform/openai/
    Responses API 工具定义、function call 和 function_call_output 适配

internal/platform/mcp/
    MCP Client、tools/list、tools/call
    get_weather MCP Server 注册

internal/weather/
    天气数据模型和 HTTP Client
```

接口由使用方定义，只在需要测试替身或隔离外部边界时抽象，不为每个结构机械增加 interface。

## Agent Loop

### 不需要工具

```text
messages + tools
  -> Responses API
  -> 普通文本结果
  -> 返回 chat.Service
```

### 需要工具

```text
messages + tools
  -> Responses API
  -> function_call(get_weather, {"city":"上海"})
  -> 校验工具名和参数
  -> MCP tools/call
  -> weather result
  -> function_call_output
  -> Responses API
  -> 最终文本结果
  -> 返回 chat.Service
```

不使用模板中的“要求模型输出自定义 JSON，再手工猜测和解析”的方案。工具调用必须来自 Responses API 的结构化输出项。

## Token 与模型调用记录

无工具调用时：

```text
model_call tokens = 第一次模型请求 usage
```

发生工具调用时：

```text
input_tokens  = planning.input_tokens + final.input_tokens
output_tokens = planning.output_tokens + final.output_tokens
total_tokens  = planning.total_tokens + final.total_tokens
```

`model_calls` 不增加新状态，不修改 ChatJob 冻结状态机，也不新增 migration。

## 错误边界

Agent 层需要区分：

```text
工具定义无效
工具调用数量超限
未知工具
工具参数无效
MCP Server 不可用
MCP tools/call 失败
天气 Provider 失败
模型 planning 失败
模型 final response 失败
流式响应未完成
context canceled / deadline exceeded
```

这些错误最终复用当前 `chat.Service.finishFailedModelCall`，映射成现有 failed、timed_out、cancelled 或 incomplete 状态，不新增状态机分支。

## 安全边界

- MCP Server 在 Compose 网络内运行，第一版不映射宿主机端口。
- Backend 使用固定工具 allowlist，不执行 MCP Server 返回的任意未知工具。
- `get_weather` 标记为只读工具。
- 城市参数需要类型、非空和长度校验。
- Weather HTTP Client 和 MCP 调用都必须使用 context 与明确超时。
- 不允许模型提供目标 URL、Header、Shell 命令或文件路径。
- MCP Server 返回内容按不可信外部数据处理，只作为工具结果交给模型。

## 阶段 0：范围与协议确认

状态：`completed`

完成内容：

- 对照 GopherAI-v2 确认天气 MCP 功能属于模板范围。
- 冻结为单 Server、单只读工具、单轮 Tool Loop。
- 选择官方 Go SDK v1.7.0 和 Streamable HTTP。
- 确认不修改现有 HTTP API、数据库 schema 和 ChatJob 状态机。

## 阶段 1：MCP SDK 最小闭环

状态：`completed`

完成内容：

- 引入官方 MCP Go SDK v1.7.0。
- 使用 `httptest` 和 Streamable HTTP 跑通无会话 Client/Server。
- 确认协商协议版本为 `2026-07-28`。
- 使用泛型 `AddTool` 自动生成并校验 echo 工具 Schema。
- 覆盖 tools/list、tools/call、非法参数拦截和 Context 取消传播。
- 测试不依赖真实天气 API、模型或外部 MCP Server。

任务：

- 引入官方 MCP Go SDK。
- 使用 SDK 创建最小内存或 httptest Server/Client。
- 注册一个测试工具。
- 完成工具发现和调用。
- 确认 SDK 实际协商的协议版本和 Streamable HTTP 写法。

验收：

```text
测试能够 list tool；
测试能够 call tool 并得到结构化结果；
context 取消能够终止调用；
不依赖真实天气 API 和模型。
```

## 阶段 2：Weather Client 与 get_weather Server

状态：`completed`

完成内容：

- 新增可配置 BaseURL、HTTP Client 和请求超时的 Weather Client。
- 校验城市输入、Provider HTTP 状态、响应体大小和天气字段。
- 将 Provider 响应转换为项目自己的稳定 `weather.Result` 模型。
- 使用 MCP SDK 泛型 `AddTool` 注册只读 `get_weather` 工具。
- 新增独立 `cmd/mcpserver`，提供 `/mcp` 和 `/healthz`。
- 增加天气 Provider、Context 取消、工具成功调用和 Provider 错误测试。
- 独立 MCP Server 二进制实际启动、健康检查和 SIGTERM 优雅退出验证通过。

任务：

- 定义 Weather 结果模型。
- 实现带超时、状态码检查和 JSON 校验的 Weather HTTP Client。
- 注册 `get_weather` MCP Tool。
- 新增 `cmd/mcpserver`。
- 提供 `/healthz`。

验收：

```text
合法城市返回结构化天气；
空城市、超长城市和 Provider 错误正确返回；
单元测试使用 httptest，不依赖公网；
MCP Server 可以独立启动和优雅退出。
```

## 阶段 3：Backend MCP Client / Tool Executor

状态：`completed`

完成内容：

- 新增不依赖 MCP SDK 的内部 `llm.ToolDefinition`。
- 实现 Streamable HTTP MCP Client，在启动时发现并缓存 `get_weather`。
- 工具定义只暴露固定 allowlist，并通过深拷贝保护内部缓存。
- `Call` 在本地校验工具名和 JSON object 参数，再执行 MCP `tools/call`。
- 将 MCP `StructuredContent` 转成稳定 JSON，供后续 OpenAI Tool Calling 使用。
- 区分连接、工具发现、Tool Error、调用超时和非法工具结果。
- 使用真实 `httptest` Streamable HTTP 覆盖正常调用、拦截、错误和超时。

任务：

- 连接 Streamable HTTP MCP Server。
- 获取并缓存工具定义。
- 将 MCP Tool 转换为内部 `llm.ToolDefinition`。
- 实现固定 allowlist 的 `get_weather` 调用。
- 增加调用 timeout 和错误映射。

验收：

```text
能够获取 get_weather 定义；
能够执行并返回工具结果；
未知工具不会被调用；
Server 不可用、超时和非法结果有明确错误。
```

## 阶段 4：OpenAI Responses Tool Calling

状态：`completed`

完成内容：

- 增加模型无关的 `ToolCall`、`ToolModelResult` 和 `ToolModel` 接口。
- 将内部 `ToolDefinition` 转换为 Responses API `function` tool。
- 解析 `response.function_call` 为结构化 ToolCall，不解析模型生成的自定义 JSON 文本。
- 使用 SDK 构造 `function_call` 和 `function_call_output` 输入项。
- 支持普通文本结果、工具调用结果和 token usage 读取。
- 校验工具定义、ToolCall 字段和 function_call_output 参数。
- 使用 fake HTTP transport 覆盖工具定义、结构化调用、结果回填和 Provider 错误。

任务：

- 增加通用 ToolModel 输入输出类型。
- 将 MCP 工具 schema 转换为 Responses API function tool。
- 解析结构化 function call。
- 构造 function_call_output 并请求最终回答。
- 汇总两次请求的 token usage。

验收：

```text
无工具场景返回普通回答；
工具场景返回 ToolCall；
工具结果可以生成最终回答；
所有 OpenAI 测试使用 fake HTTP transport；
不解析模型生成的自定义 JSON 文本。
```

## 阶段 5：Agent Client 与普通 Chat/ChatJob

状态：`pending`

任务：

- 新增实现 `llm.ModelClient` 的 Agent Client。
- 完成最多一轮的 Tool Loop。
- 在 bootstrap 中用 Agent Client 包装当前 OpenAI Client。
- 保持 `chat.Service`、Handler 和 ChatJob API 不变。
- 覆盖普通回答、天气调用和各类失败测试。

验收：

```text
普通 Chat 可以自动调用天气工具；
不需要工具的问题保持原行为；
ChatJob 复用同一 Tool Loop；
最终 assistant 消息和聚合 token 正确保存。
```

## 阶段 6：SSE Tool Calling

状态：`pending`

任务：

- 解析流式 Responses function call 事件。
- 工具调用期间不向用户输出内部协议内容。
- MCP 调用完成后流式输出最终自然语言回答。
- 正确处理断连、取消、未完成响应和第二次模型调用失败。

验收：

```text
SSE 客户端只看到最终回答文本和 DONE；
不会看到 function call 参数或原始工具结果；
断开连接会取消 MCP/模型调用；
model_call 状态与 token usage 正确。
```

## 阶段 7：配置与 Compose

状态：`pending`

新增配置：

```env
MCP_HTTP_ADDR=127.0.0.1:8081
MCP_SERVER_URL=
MCP_CALL_TIMEOUT_SECONDS=10
WEATHER_API_BASE_URL=https://wttr.in
WEATHER_API_TIMEOUT_SECONDS=5
```

任务：

- Dockerfile 同时构建 backend 和 mcpserver 二进制。
- Compose 增加 `mcp-server` service。
- MCP Server 只加入内部网络，不映射宿主机端口。
- Backend 在 URL 为空时禁用 Tool Use，在 URL 有值时启用。
- 补启动顺序、有限连接重试和优雅退出。

验收：

```text
本地未配置 MCP 时普通 Chat 仍可运行；
Compose 启动后 Backend 能连接 MCP Server；
MCP Server 重启后调用错误可诊断；
镜像中不包含密钥。
```

## 阶段 8：真实 E2E 与收尾

状态：`pending`

验收问题：

```text
普通问题：请用一句话介绍 Go。
工具问题：上海现在天气怎么样？
```

需要验证：

- 普通 Chat 不调用工具。
- 天气问题实际到达 MCP Server。
- 普通 Chat、SSE 和 ChatJob 都得到最终回答。
- RAG 与 MCP 同时启用时不破坏当前 RAG 行为。
- Compose 冷启动和重启通过。
- `go test ./...`、`go vet ./...` 和 Docker E2E 通过。
- README 增加 MCP 配置和运行说明。

## 暂不改变的现有设计

- Handler -> Service -> Repository 分层。
- Conversation 和 Message 数据模型。
- `model_calls` 状态机。
- ChatJob 冻结状态机和全局 attempt_count。
- RAG current/version 发布模型。
- RabbitMQ ACK/Nack 行为。
- SSE 对外事件格式。
