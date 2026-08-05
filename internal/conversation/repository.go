package conversation

import "context"

type Repository interface {
	Create(ctx context.Context, conversation *Conversation) error
	ListByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*Conversation, error)
	GetByIDAndUserID(ctx context.Context, userID uint64, conversationID uint64) (*Conversation, error)
	DeleteByIDAndUserID(ctx context.Context, userID uint64, conversationID uint64) error
}
