# RAG 收敛计划

## 目标

在不扩大产品范围、不修改 ChatJob 状态机的前提下，把当前 RAG 功能收敛成一个可稳定演示、易于继续维护的小型 MVP。

当前主链路已经存在：

```text
上传 .md/.txt
  -> 文本切块
  -> OpenAI Embedding
  -> Redis 保存向量
  -> 本地保存原文档

Chat / SSE / ChatJob
  -> 精确查询 request message
  -> 检索 Top-K chunks
  -> 构造 RAG Prompt
  -> 调用模型
```

本轮只处理已经确认的可靠性问题和局部复杂度，不新增产品功能。

## 工作规则

- 同一时间只进行一个阶段。
- 每个阶段完成后运行测试、进行 review，并单独提交。
- 未达到当前阶段的验收条件前，不进入下一阶段。
- 不修改冻结的 ChatJob 状态机。
- 不引入 Redis Search、pgvector、工作流引擎或复杂分布式锁。
- 不在本轮加入 TTS、MCP、多模型选择、验证码、前端或 Docker。

## 阶段 0：RAG 基线验收

状态：`pending`

目的：先确认当前代码的真实行为，避免在未验证的链路上重构。

任务：

- 上传一个 `.md` 或 `.txt` 文档。
- 确认文件写入 `RAG_STORAGE_ROOT/{userID}`。
- 确认 Redis 存在 `gopherai:rag:chunks:{userID}`。
- 验证普通 Chat 能使用文档内容。
- 验证 SSE Chat 能使用文档内容。
- 验证 ChatJob 能使用文档内容。
- 重启服务后再次验证检索。
- 验证未上传文档的用户仍走普通 Chat。

验收条件：

```text
上传、普通 Chat、SSE、ChatJob、重启恢复全部通过；
失败现象和必要命令有明确记录。
```

## 阶段 1：Embedding 批处理

状态：`completed`

完成记录：

- OpenAI Embedder 内部按每批最多 64 条文本请求。
- 保持 `rag.Embedder` 接口不变。
- 按批次偏移和响应 `Index` 恢复全局输入顺序。
- 后续批次失败时整体返回 `nil, error`。
- 增加 130 条输入、乱序响应、后续批次失败和跨批维度不一致测试。

问题：当前 `Embed` 会将所有 chunks 放入一次请求。接近 5 MiB 的文档可能产生大量 chunks，超过上游单次请求限制。

边界：

- `rag.Embedder` 接口保持不变。
- 批处理只在 OpenAI 适配器内部完成。
- 第一版固定每批最多 64 条文本。
- 任意一批失败时整体返回错误，不返回部分结果。
- 最终向量顺序必须与输入文本顺序一致。

任务：

- 将输入按 64 条分批。
- 每批调用 Embeddings API。
- 按批次和响应 `Index` 合并结果。
- 保留空向量、重复 Index、越界 Index、维度不一致校验。
- 增加跨批次顺序、末尾不足一批、第二批失败测试。

验收条件：

```text
130 条输入被拆成 64 + 64 + 2；
输出数量和顺序正确；
任何批次失败时不返回部分向量；
现有 Embedding 和 RAG 测试通过。
```

## 阶段 2：文档与索引一致性

状态：`completed`

完成记录：

- 每次上传生成 UUID version，本地文件和 Redis chunks 使用同一 version。
- Redis 使用独立 current 指针发布生效版本。
- 新 chunks 写入时带 24 小时 staging TTL，激活前崩溃不会永久残留。
- Activate 使用 Lua 原子执行：`PERSIST` 新版本、切换 current、给旧版本设置 24 小时 TTL。
- Redis 旧版本不立即删除，避免并发读取在切换窗口内找不到数据。
- 本地旧文件在激活成功后 best effort 删除。
- 激活响应不确定时保留新版本，避免 current 指向已删除数据。
- 已覆盖首次激活、缺失版本、失败清理、替换和并发激活测试。

问题：当前上传先替换 Redis，再保存本地文件。文件保存失败时，API 返回失败，但聊天已经使用新索引；并发上传还可能让文件和索引来自不同请求。

采用简单版本切换，不引入数据库事务：

```text
生成唯一 version
  -> 写入 version 对应的本地文件
  -> 写入 version 对应的 Redis chunks
  -> 原子切换 current_version
  -> 尽力清理旧 version
```

必要状态：

```text
Redis:
  gopherai:rag:chunks:{userID}:current -> version
  gopherai:rag:chunks:{userID}:{version} -> chunks JSON

Filesystem:
  {RAG_STORAGE_ROOT}/{userID}/{version}.txt|md
```

状态规则：

- 切换 `current_version` 前失败：旧版本继续生效。
- 文件和 Redis 新版本都写成功后，才能切换 current。
- 切换成功后，新版本立即生效。
- 旧版本清理失败不回滚新版本，只记录错误。
- Retrieve 始终先读取 current version，再读取对应 chunks。
- Redis staging/旧版本使用 24 小时 TTL，不增加后台清理任务。

开始编码前，先单独 review 接口变化和上述状态规则。

验收条件：

```text
模拟文件写入失败、Redis 写入失败、current 切换失败时，旧版本仍可检索；
成功替换后文件与 chunks 属于同一 version；
两次并发替换的最终 current 指向一套完整数据。
```

## 阶段 3：RAG 上传限流

状态：`completed`

完成记录：

- Chat 与 RAG 上传复用同一个固定窗口 Lua 限流算法。
- RAG 上传使用独立 prefix：`gopherai:rate_limit:rag_upload:user`。
- 增加独立的 limit/window 环境配置和本地开发配置。
- `/rag/documents` 在 Handler 前执行独立限流 middleware。
- 超限继续复用 429 与 `Retry-After`，Redis 错误返回 503。
- 已验证同一用户的 Chat 与 RAG 上传计数互不影响，路由不会误用 Chat limiter。

问题：上传会产生 Embedding 成本，但当前只鉴权，没有频率限制。

边界：

- 复用现有 Redis 固定窗口限流思路。
- RAG 上传使用独立 key prefix 和独立配置。
- 不与 Chat 共用计数器。

建议配置：

```env
RAG_UPLOAD_RATE_LIMIT=3
RAG_UPLOAD_RATE_WINDOW_SECONDS=3600
```

建议 key：

```text
gopherai:rate_limit:rag_upload:user:{userID}
```

验收条件：

```text
限制内上传正常；
超限返回 429 和 Retry-After；
不同用户互不影响；
Redis 故障返回 503；
Chat 限流行为不变。
```

## 阶段 4：收敛 main 组装

状态：`completed`

完成记录：

- 新增 `cmd/server/bootstrap.go`，集中放置特性组装和基础设施连接 helper。
- `buildImageFeature` 管理 ONNX 配置、Image Service/Handler 和关闭资源。
- `buildRAGFeature` 组装 FileStore、Redis chunks、Embedder、RAG Service/Handler 和上传限流器。
- `buildChatFeature` 组装 Message、model_calls、UnitOfWork、Chat、ChatJob 和 RabbitMQ Client。
- Chat Feature 暴露 Message/Chat/ChatJob Handler，并统一管理 Consumer 启动与 RabbitMQ 关闭。
- `buildRateLimiter` 统一解析正整数 limit/window 配置。
- `connectPostgreSQL`、`connectRedis` 统一创建连接、启动探活和失败清理。
- `startChatJobConsumer` 封装 Consumer goroutine 与单 Job 超时。
- 简单的用户、会话、消息组装继续保留在 `run()`，没有引入依赖注入容器。
- 所有配置和依赖构造完成后才启动 RabbitMQ Consumer。
- Router 使用 `RouterHandlers` 和 `RouterMiddleware` 两个命名结构替代位置参数列表。
- Router 注册拆分为公开路由和鉴权路由两个私有函数。

目的：降低 `run()` 的阅读负担，不改变依赖关系和启动行为。

实际抽取以下构造函数：

```go
buildImageFeature()
buildRAGFeature(redisClient)
buildRateLimiter(...)
connectPostgreSQL(...)
connectRedis(...)
startChatJobConsumer(...)
```

约束：

- 环境变量仍只在 `cmd/server` 读取。
- 平台包不直接读取环境变量。
- 不引入依赖注入框架。
- 不把所有依赖塞入一个巨型容器。
- 启动失败仍然立即返回明确错误。

验收条件：

```text
run() 主要表达启动顺序和生命周期；
所有 main 测试通过；
启动和关闭行为不变。
```

## 阶段 5：收敛 Chat model_call 失败收尾

状态：`pending`

问题：`history_load`、`prepare_model_message`、`llm` 等错误路径重复执行：

```text
markModelCallFailure
context.WithoutCancel + timeout
modelCall.Finish
errors.Join
```

目标：抽取一个私有 helper，统一失败状态和清理超时，不改变任何状态语义。

约束：

- `operation` 字符串和现有 error code 保持不变。
- `context.Canceled`、`DeadlineExceeded`、`Incomplete` 映射保持不变。
- ChatJob 重试逻辑和状态机保持不变。

验收条件：

```text
现有 Chat、SSE、ChatJob 失败测试全部通过；
每个失败阶段仍会完成 model_call 收尾；
重复代码明显减少。
```

## 阶段 6：最终验收与下一步决策

状态：`pending`

任务：

- 重新执行阶段 0 的全部真实验收。
- 运行 `go test ./...`。
- 运行 `go vet ./...`。
- 编译 PostgreSQL integration 测试。
- 确认工作树干净并推送。
- 更新本文件的阶段状态和已知限制。

完成本轮后再决定下一项产品工作：

1. Docker 部署和运行说明；或
2. 显式普通/RAG 模式选择；或
3. 模板中的 MCP/TTS。

不在本轮提前选择或实现。

## 已知但不在本轮处理

- ChatJob Claim 持续失败时可能无限重投。
- Retry/Fail 写数据库失败时 Job 可能停在 processing。
- RabbitMQ 没有显式 delivery limit 或 DLQ。
- 没有 lease。
- RAG 第一版每个用户只保留一份当前文档。
- 异常退出可能留下本地孤儿版本文件，需要后续手动清理。
- 向量相似度在 Go 进程中计算，不使用 Redis Search 或 pgvector。
- 图片推理 Session 当前串行复用 Tensor。
