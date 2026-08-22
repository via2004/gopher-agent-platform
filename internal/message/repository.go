package message

import "context"

type MessageRepository interface {
	Create(ctx context.Context, userID uint64, message *Message) error
	GetByID(ctx context.Context, userID, conversationID, messageID uint64) (*Message, error)
	ListByConversationID(
		ctx context.Context,
		userID uint64,
		conversationID uint64,
		limit, offset int,
	) ([]*Message, error)
	ListRecentByConversationID(ctx context.Context, userID uint64,
		conversationID uint64, limit int) ([]*Message, error)
}
