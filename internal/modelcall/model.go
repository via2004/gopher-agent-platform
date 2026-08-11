package modelcall

import "time"

/*
初步定的字段：
id
conversation_id
request_message_id       NOT NULL
assistant_message_id     NULL
provider_response_id     NULL
requested_model          NULL
actual_model             NULL
status
input_tokens             NULL
output_tokens            NULL
total_tokens             NULL
error_code               NULL
started_at
finished_at              NULL
语义：
1. 调用记录是必须成功的业务数据，还是允许丢失的观测数据？
2. assistant message 和 model call 是否要放进同一个数据库事务？
3. 失败的模型调用是否也要记录？
4. 流式中途断开时记录什么状态和已消耗 Token？

1. 我认为成功和失败的数据都要记录，因为观测数据的记录本质上在message表中都完整记录了，这里也都记录一致性更好
2. 我觉得可以放，本质上assistant message和model call都在做同一个事情，模型调用也应该是assistant message的一部分
3. 可以记录，这里直接把user message和modelcall绑定起来感觉更合理，删除的时候直接把modelcall表的内容也同理删掉
4. 应该是failed，已消耗Token这里应该是流式任务中记录了多少token就写多少token吧，这里要改语义应该是改流式中的具体返回语义

user message 1 -> N model calls
model call  0 -> 1 assistant message

短事务 1：
保存 user message
创建 running model_call

事务外：
调用 LLM

短事务 2（成功）：
保存 assistant message
把 model_call 更新为 completed 并关联 assistant message

短事务 2（失败）：
把 model_call 更新为 failed/cancelled/timed_out

| 场景 | 状态 |
|---|---|
| 调用中 | `running` |
| 正常完成 | `completed` |
| Provider 失败 | `failed` |
| 客户端断开 | `cancelled` |
| Deadline 到期 | `timed_out` |
| Provider 返回不完整结果 | `incomplete` |
*/

type Status string

const (
	StatusRunning    Status = "running"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
	StatusTimedOut   Status = "timed_out"
	StatusIncomplete Status = "incomplete"
)

func (s Status) IsFailureTerminal() bool {
	switch s {
	case StatusFailed, StatusCancelled, StatusTimedOut, StatusIncomplete:
		return true
	default:
		return false
	}
}

type Model struct {
	ID                 uint64
	ConversationID     uint64
	RequestMessageID   uint64
	AssistantMessageID *uint64

	Provider           string
	ProviderResponseID *string
	RequestedModel     *string
	ActualModel        *string

	Status       Status
	InputTokens  *int64
	OutputTokens *int64
	TotalTokens  *int64
	ErrorCode    *string

	StartedAt  time.Time
	FinishedAt *time.Time
}
