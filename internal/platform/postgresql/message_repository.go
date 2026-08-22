package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"gopherai/internal/conversation"
	"gopherai/internal/message"
)

type MessageRepository struct {
	pool DBTX
}

func NewMessageRepository(pool DBTX) *MessageRepository {
	return &MessageRepository{pool: pool}
}

func (r *MessageRepository) Create(ctx context.Context,
	userID uint64, message *message.Message) error {
	err := r.pool.QueryRow(ctx, InsertMessageByConversationIDAndUserID,
		userID, message.ConversationID, message.Role, message.Content).
		Scan(&message.ID, &message.CreatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return conversation.ErrConversationNotFound
	case err != nil:
		return fmt.Errorf("insert message: %w", err)
	default:
		return nil
	}
}

func (r *MessageRepository) GetByID(ctx context.Context, userID, conversationID, messageID uint64) (*message.Message, error) {
	item := &message.Message{}
	err := r.pool.QueryRow(ctx, GetMessageByIDAndConversationIDAndUserID,
		messageID, conversationID, userID).Scan(
		&item.ID, &item.ConversationID, &item.Role,
		&item.Content, &item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, message.ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query message by id: %w", err)
	}
	return item, nil
}

func (r *MessageRepository) ListByConversationID(ctx context.Context,
	userID uint64, conversationID uint64, limit, offset int) ([]*message.Message, error) {
	if err := r.checkConversationExistence(ctx, userID, conversationID); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, QueryMessagesByConversationIDAndUserID,
		userID, conversationID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()

	result := make([]*message.Message, 0)
	for rows.Next() {
		message := &message.Message{}
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role,
			&message.Content, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}

		result = append(result, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	return result, nil
}

func (r *MessageRepository) ListRecentByConversationID(ctx context.Context, userID uint64,
	conversationID uint64, limit int) ([]*message.Message, error) {
	if err := r.checkConversationExistence(ctx, userID, conversationID); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, QueryRecentMessagesByConversationIDAndUserID,
		userID, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()

	result := make([]*message.Message, 0)
	for rows.Next() {
		message := &message.Message{}
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role,
			&message.Content, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}

		result = append(result, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	return result, nil
}

func (r *MessageRepository) checkConversationExistence(ctx context.Context, userID uint64, conversationID uint64) error {
	foundConversation := conversation.Conversation{}
	err := r.pool.QueryRow(ctx, GetConversationByIDAndUserID, conversationID, userID).
		Scan(&foundConversation.ID, &foundConversation.UserID, &foundConversation.Title,
			&foundConversation.CreatedAt, &foundConversation.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.ErrConversationNotFound
	}
	if err != nil {
		return fmt.Errorf("query conversation error: %w", err)
	}

	return nil
}
