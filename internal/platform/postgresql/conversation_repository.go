package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

func (r *ConversationRepository) ListByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*conversation.Conversation, error) {
	rows, err := r.pool.Query(ctx, QueryConversationByUserID, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]*conversation.Conversation, 0)

	for rows.Next() {
		item := &conversation.Conversation{}
		if err := rows.Scan(&item.ID, &item.UserID, &item.Title,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		conversations = append(conversations, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}

	return conversations, nil
}

func (r *ConversationRepository) GetByIDAndUserID(ctx context.Context, userID uint64, conversationID uint64) (*conversation.Conversation, error) {
	foundConversation := &conversation.Conversation{}
	err := r.pool.QueryRow(ctx, GetConversationByIDAndUserID, conversationID, userID).
		Scan(&foundConversation.ID, &foundConversation.UserID, &foundConversation.Title,
			&foundConversation.CreatedAt, &foundConversation.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, conversation.ErrConversationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("query conversation error: %w", err)
	}

	return foundConversation, nil
}

func (r *ConversationRepository) DeleteByIDAndUserID(ctx context.Context, userID uint64, conversationID uint64) error {
	var deletedID uint64

	err := r.pool.QueryRow(
		ctx,
		DeleteConversationByIDAndUserID,
		conversationID,
		userID,
	).Scan(&deletedID)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return conversation.ErrConversationNotFound
	case err != nil:
		return fmt.Errorf("delete conversation: %w", err)
	default:
		return nil
	}
}
