package message

import "context"

type MessageRepository interface {
	Create(ctx context.Context, userID uint64, message *Message) error
	ListByConversationID(
		ctx context.Context,
		userID uint64,
		conversationID uint64,
		limit, offset int,
	) ([]*Message, error)
	ListRecentByConversationID(ctx context.Context, userID uint64,
		conversationID uint64, limit int) ([]*Message, error)
}
