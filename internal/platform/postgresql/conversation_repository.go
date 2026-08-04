package platform

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"gopherai/internal/conversation"
)

type ConversationRepository struct {
	pool *pgxpool.Pool
}

func NewConversationRepository(pool *pgxpool.Pool) *ConversationRepository {
	return &ConversationRepository{pool: pool}
}

func (r *ConversationRepository) Create(ctx context.Context, newConversation *conversation.Conversation) error {
	err := r.pool.QueryRow(
		ctx,
		InsertConversation,
		newConversation.UserID,
		newConversation.Title,
	).Scan(
		&newConversation.ID,
		&newConversation.CreatedAt,
		&newConversation.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("insert conversation: %w", err)
	}

	return nil
}
