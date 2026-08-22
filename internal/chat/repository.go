package chat

import (
	"context"
	"gopherai/internal/message"
	"gopherai/internal/modelcall"
	"gopherai/internal/rag"
)

type MessageService interface {
	CreateUserMessage(
		ctx context.Context,
		userID, conversationID uint64,
		content string,
	) (*message.Message, error)

	CreateAssistantMessage(
		ctx context.Context,
		userID, conversationID uint64,
		content string,
	) (*message.Message, error)
	GetByID(ctx context.Context, userID, conversationID, messageID uint64) (*message.Message, error)

	ListRecent(
		ctx context.Context,
		userID, conversationID uint64,
		limit int,
	) ([]*message.Message, error)
}

type ModelCallService interface {
	Start(
		ctx context.Context,
		userID uint64,
		modelCall *modelcall.Model,
	) error

	Complete(
		ctx context.Context,
		userID uint64,
		modelCall *modelcall.Model,
	) error

	Finish(
		ctx context.Context,
		userID uint64,
		modelCall *modelcall.Model,
	) error
}

type UnitOfWork interface {
	WithinTx(
		ctx context.Context,
		fn func(
			messages MessageService,
			modelCalls ModelCallService,
		) error,
	) error
}

type Retriever interface {
	Retrieve(ctx context.Context,
		userID uint64, query string,
		topK int) ([]rag.Chunk, error)
}
