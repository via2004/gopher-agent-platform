package chatjob

import (
	"context"
	"gopherai/internal/chat"
)

type Repository interface {
	// 验证conversation属于user,并创建Pending的任务,但这里并不创建用户消息
	Create(ctx context.Context, userID, conversationID uint64, content string) (*Job, error)
	// 用户查询自己的任务状态，防止越权
	GetByID(ctx context.Context, userID, jobID uint64) (*Job, error)
	// 根据jobID获取内容和userID,根据job_id conversationID userID定位到一条job,
	// 原子更新它的status为processing, 设置started_at, 返回Job和userID
	ClaimForProcessing(ctx context.Context, jobID uint64) (*Job, uint64, error)
	Retry(ctx context.Context, jobID uint64) error
	// 一次性生成该任务的用户消息，并在后续调用时返回相同的消息 ID。
	EnsureRequestMessage(ctx context.Context, userID, jobID uint64) (uint64, error)
	FindCompletedAssistantID(ctx context.Context, userID, jobID uint64) (uint64, bool, error)

	Complete(ctx context.Context, jobID, assistantMessageID uint64) error
	Fail(ctx context.Context, jobID uint64, errorCode string) error
}

/*
状态机：
Create
-> pending

Worker ClaimForProcessing
pending -> processing

Chat 成功
processing -> Complete
processing -> completed + assistant_message_id

Chat 失败
processing -> Fail
processing -> failed + error_code
*/

type Publisher interface {
	PublishChatJob(ctx context.Context, jobID uint64) error
}

type ChatProcessor interface {
	ChatFromExistingMessage(ctx context.Context, userID uint64,
		conversationID uint64, requestMessageID uint64) (*chat.Result, error)
}
