# ChatJob 遗留 processing 恢复方案

状态：暂缓实施（2026-09-20）。先梳理清楚现有后端，保留本方案供以后评估；不新增接管机制或状态写入重试逻辑，未实现或执行数据库迁移。

## 1. 要修的问题和接入位置

当前 `chatjob.Service.Process` 开头调用 `ClaimForProcessing`，后者只领取 pending。
任何未领取结果都合并为 ErrJobNotClaimable，Service 返回 nil，消费者 Ack。
如果 Retry/Fail 更新失败，任务仍在 processing，下一次投递就可能被误 Ack，永久遗留。

接管接在已有的 `Process → ClaimForProcessing`，由 RabbitMQ 每次投递触发。
不增加公开 API、定时扫描器、独立 Worker 或新的队列。普通 Chat/SSE 继续原流程。

修复目标：只要消息仍可投递，数据库恢复后，任务能够重新执行或完成状态收尾；
已失去处理权的旧执行者不能再保存回复或改写 Job。

范围之外：已经误 Ack 的历史任务、Job 写库成功但发布失败且消息未入队、模型端重复计费。
这些不能靠消息驱动的接管自动解决。本方案不宣称 exactly-once。

## 2. 数据库：增加处理有效期和领取版本

新增 `000008_add_chat_job_lease.up.sql` / `.down.sql`：

```sql
ALTER TABLE chat_jobs
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN claim_version BIGINT NOT NULL DEFAULT 0
        CHECK (claim_version >= 0);
```

- `lease_expires_at`：本次处理权到期时间。
- `claim_version`：每次成功领取/接管都加一，旧执行者持有旧版本，无法再写入。
- processing 必须有 lease；其他状态 lease 必须为 NULL，新增 CHECK 约束。
- 迁移先给已有 processing 回填 `started_at + interval '3 minutes'`，再加约束。
- 迁移部署需停止旧消费者后再启用新消费者；旧二进制不知道版本校验，不能混跑。
- down 删除新增约束和字段；只在停止新消费者后执行，不随意回退在途任务语义。
- Job 内部模型增加对应字段，HTTP DTO 保持原字段。

默认 Job 执行上下文仍为两分钟，lease 固定三分钟，暂不做续租。
时间判定使用 PostgreSQL 时间，避免多个进程本地时钟不一致。
三分钟只是恢复等待窗口，防止旧执行者写入依靠 claim_version 和事务内校验。

## 3. Repository：领取时区分状态，执行次数单独扣除

修改 `internal/chatjob/repository.go` 与 PostgreSQL 实现，增加以下明确的接口语义：

```text
ClaimForProcessing(ctx, jobID, leaseDuration) → Job、userID、error
StartAttempt(ctx, jobID, claimVersion, maxAttempts) → attemptCount、error
```

### ClaimForProcessing

使用短事务，按 jobID 锁定任务行（SELECT ... FOR UPDATE），读状态与归属。
锁定成功后再检查数据库当前时间与有效期；不要在持有行锁期间调用模型。

分支：

1. 不存在：ErrJobNotFound。
2. completed/failed：ErrJobTerminal。
3. processing 且 lease 未到期：ErrJobBusy，不更新计数或状态。
4. pending 或 lease 已到期的 processing：设置 processing、更新 started_at、
   设置 lease_expires_at、claim_version 加一，返回领取后的记录。
5. 数据库/提交错误：原样包装返回，不能映射成“任务已经完成”。

这里不增加 attempt_count。claim_version 才是领取版本，两者不能混用。
按行锁串行判断，避免“UPDATE 未命中再单独 SELECT”之间的状态变化被误判。

### StartAttempt

在确认没有现成回复、准备实际执行本次处理时调用。条件 UPDATE 必须同时检查：

```text
id 匹配
status = processing
claim_version 匹配
lease 仍有效
attempt_count < maxAttempts
```

成功才执行 `attempt_count = attempt_count + 1`，返回新值。
未更新时要区分 ErrAttemptLimitReached 与 ErrLeaseLost；不能吞掉数据库错误。

attempt_count 表示实际开始的处理尝试，仍包含首次执行；准备用户消息等步骤失败也计入。
领取后发现已有回复，仅补写 completed，不消耗新的执行额度。
达到上限仍可领取处理权执行收尾，但不允许再执行 StartAttempt 或调用模型。

### 其他 Repository 写操作

`EnsureRequestMessage`、`Retry`、`Complete`、`Fail` 增加 claimVersion 参数：

- 检查 processing、版本匹配和有效期，防止旧执行者操作。
- Retry 清空 started_at、lease_expires_at，保留次数和 request_message_id。
- Complete/Fail 清空 lease_expires_at，保留 started_at，填写 finished_at。
- 零行更新或归属变化要明确返回 ErrLeaseLost/对应状态错误，不伪装成功。
- 多语句状态判断采用短事务和行锁；只以条件写入成功为准。

`FindCompletedAssistantID` 在 EnsureRequestMessage 之前调用：没有 request_message_id 时
返回未找到即可；已有 message/model_call 的关联查询继续复用。
查询本身不消耗额度，后续 Complete/StartAttempt 仍要验证当前处理权。

## 4. Service.Process 的最终顺序

修改 `internal/chatjob/service.go`，按以下顺序实现：

```text
ClaimForProcessing
  不存在 / 已终态 → nil，消费者 Ack
  Busy / 数据库错误 → 返回 error，消费者稍后重新投递
  领取成功 → 保存 claimVersion

FindCompletedAssistantID
  查询失败 → 返回 error，保留恢复机会，不能当作“没有答案”
  找到回复 → Complete（带 claimVersion），成功才返回 nil

StartAttempt
  已达上限 → Fail(attempts_exhausted)，写成功才返回 nil
  处理权丢失 / 数据库错误 → 返回 error
  成功 → 得到本次 attemptCount

EnsureRequestMessage（带 claimVersion）
ChatFromExistingMessage（携带本次处理权信息）
Complete（带 claimVersion）
```

处理错误时：

- 失去处理权：停止旧执行者，不尝试用旧版本 Retry/Fail；返回错误保留投递机会。
- 未达执行上限：尝试 Retry → 返回处理错误 → 消费者 Nack。
- 已达执行上限且生成/准备失败：尝试 Fail → 成功返回 nil，否则返回错误。
- 完整回复已经持久化，但 Complete 失败：返回错误等待重新领取后补写 Complete；
  不能因为额度耗尽把已有成功回复的任务直接写成 failed。
- Fail/Retry 没写成功且仍处于 processing：重投递先得到 Busy，过期后再次接管。
- 写入实际已提交但客户端没收到确认：重新领取会看到真实状态；终态 Ack，pending 可重新领取。

无论接管多少次，StartAttempt 都不能超过 maxAttempts；状态收尾重试与生成次数分开。
超限恢复没有原始错误细节时记录 `attempts_exhausted`，不猜测上一次具体失败原因。

## 5. Chat 保存回复的事务也要验证处理权

只给 Job 的 Complete/Fail 加版本条件不够：旧执行者仍可能在 chat.Service 中插入重复回复。
需要在现有 Chat 短事务中加入可选的 Job 处理权验证。

具体接法：

- 在 `internal/chat` 定义小型值对象 `JobExecution{JobID, ClaimVersion}`。
  chatjob 已依赖 chat，可以传入该对象；chat 包不反向 import chatjob，避免循环依赖。
- `ChatFromExistingMessage` 新增必需的 JobExecution 参数，由 Process 显式传入。
- 将处理权沿 `startModelCallForExistingMessage → respond → saveAssistantAndComplete` 传递。
  普通 Chat/SSE 使用无 Job 处理权的路径。不要通过 context.Value 隐式传入它。
- `chat.UnitOfWork` 新增 `WithinJobTx(ctx, userID, conversationID, execution, fn)`；
  现有 WithinTx 保持原语义。
- PostgreSQL ChatUnitOfWork 在 WithinJobTx 中先锁定对应 Job 行，验证 user/conversation、
  processing、claim_version 和 lease。验证成功才执行传入的消息/model_call 操作。
- 为已有提问创建 model_call 时用 WithinJobTx；保存 AI 回复和 completed model_call 时
  也用 WithinJobTx。锁只覆盖这些短事务，不覆盖历史读取、RAG 或 LLM 网络请求。
- 检查和保存必须共用同一个 pgx.Tx。禁止先在独立连接里检查，再开另一个事务保存。
- 失去处理权返回 chat.ErrJobExecutionLost，chatjob 将它归类为可重新投递的控制错误。
- 失败收尾仍可更新旧尝试自己的 model_call 为失败终态，但不能修改新执行者的 Job。

同一行锁使接管与回复提交有确定先后：旧回复事务先成功提交，新执行者会查到已有回复；
接管先成功提交，旧版本便无法通过验证。防止重复落库，不承诺撤销已发出的 Provider 请求。

## 6. Consumer 接什么逻辑

`ConsumeChatJobs` 保留消费循环，handler 仍然只调用 Process：

```text
Process nil → Ack
Process error → 可取消地等待 2 秒 → Nack(requeue=true)
```

- 使用 Timer + select ctx.Done，不使用不可取消的 Sleep。
- 等待期间保留本条消息未确认；退出时关闭 channel，由 Broker 重投递未确认消息。
- 两秒退避同时作用于 Busy 和数据库故障，避免立即 Nack 的紧循环。
- Ack/Nack/Reject 返回错误时退出消费循环并上报，不能继续静默运行。
- 不把 AMQP redelivered 当成“旧执行者已死”的证据。
- 当前 prefetch=1，等待两秒会短暂占住唯一消费槽，这是 MVP 接受的吞吐取舍；
  每次等待后重新入队，不在同一条消息上阻塞整个三分钟有效期。
- 本轮不增加延迟队列、死信队列或新的 Broker 拓扑。

## 7. 按两个可 review 的阶段实施

### 阶段 A：领取与恢复状态机

文件：迁移、chatjob/model.go、errors.go、repository.go、service.go，
PostgreSQL chat_jobs_repository.go/chat_jobs_sql.go，以及对应测试。

先实现 lease、claimVersion、StartAttempt、状态区分及 Process 恢复顺序。
阶段 A 用测试验证，不部署到实际消费者；必须与阶段 B 合并后才算修复完成。

### 阶段 B：保护回复事务并接入消费者

文件：chat/repository.go、service.go、errors.go、chat_unit_of_work.go，
chatjob 的 ChatProcessor 接口、rabbitmq/consumer.go、启动配置与对应测试。

接入 JobExecution 与 WithinJobTx，完成旧执行者写入隔离、消费退避、确认错误处理。
更新 BACKEND_OVERVIEW/README 的恢复能力说明，保留尚未解决的发布非原子边界。

## 8. 必须验证的行为

1. pending 领取成功；同时领取只有一个成功，另一个 Busy。
2. 未过期 processing 不可接管；到期后版本加一、可接管。
3. 旧版本无法 Ensure/Retry/Complete/Fail，也无法插入回复或完成 model_call。
4. 写 failed 失败 → 重投递 → Busy → 到期接管 → 成功写 failed → Ack。
5. Retry 更新失败 → 到期接管 → 有额度才重新执行，用户消息 ID 保持不变。
6. 回复已提交、Job Complete 失败 → 恢复只补状态，不再调用模型，即使次数已满。
7. 每次领取版本递增，但只有 StartAttempt 消耗额度；恢复收尾不突破执行上限。
8. 数据库错误与终态/不存在可区分；确认响应丢失后按真实数据库状态恢复。
9. 事务 B 与接管并发，用两个数据库连接及同步点验证只允许一个有效结果写入。
10. 消费者 nil→Ack、error→退避再Nack；退出可打断等待；确认错误会向上报告。
11. 模拟进程在领取后退出，重投递在有效期后可以恢复；不依赖内存记录。

验证方式：单元测试控制时钟/定时等待，不用真实等待三分钟；独立 PostgreSQL integration
测试检查 SQL 原子性和事务隔离；独立队列验证重投递和退出行为，不能污染演示业务队列。
运行 go test ./...、go test -race ./...、go vet ./... 及相关集成测试。
不以 mock 验证结果代替真实 SQL 并发与回滚验证。

## 9. 明确不做的事情

- 不把所有 processing 无条件改回 pending。
- 不仅将 ErrJobNotClaimable 从 nil 改为 error。
- 不把三分钟超时当成执行者绝对停止的证明。
- 不在调用模型期间持有数据库事务或行锁。
- 不自动补发从未进入队列的 pending Job，不自动修改已经误 Ack 的历史任务。
- 不宣称可以保证模型端只调用一次；本次保证的是恢复路径和数据库写入的处理权。
