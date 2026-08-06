package chat

import (
	"context"
	"gopherai/internal/message"
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

	ListRecent(
		ctx context.Context,
		userID, conversationID uint64,
		limit int,
	) ([]*message.Message, error)
}
