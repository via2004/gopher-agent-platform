package chatjob

import (
	"context"
	"gopherai/internal/chat"
)

type Repository interface {
	// 验证conversation属于user,并创建Pending的任务
	Create(ctx context.Context, userID, conversationID uint64, content string) (*Job, error)
	// 用户查询自己的任务状态，防止越权
	GetByID(ctx context.Context, userID, jobID uint64) (*Job, error)
	/*
		Worker 根据 jobID 获取内容和 conversation 对应的 userID。
		Worker 收到RabbitMQ的job_id后，查询任务内容，conversationID和对应userID
		Claim同时完成：
		检查status=pending
		原子更新为processing
		设置started_at
		返回Job和userID
	*/
	ClaimForProcessing(ctx context.Context, jobID uint64) (*Job, uint64, error)
	Retry(ctx context.Context, jobID uint64) error
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
	RespondToMessage(ctx context.Context, userID uint64,
		conversationID uint64, requestMessageID uint64) (*chat.Result, error)
}
