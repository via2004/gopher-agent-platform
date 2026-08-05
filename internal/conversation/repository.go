package conversation

import "context"

type Repository interface {
	Create(ctx context.Context, conversation *Conversation) error
	ListByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*Conversation, error)
}
