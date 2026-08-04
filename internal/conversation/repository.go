package conversation

import "context"

type Repository interface {
	Create(ctx context.Context, conversation *Conversation) error
}
