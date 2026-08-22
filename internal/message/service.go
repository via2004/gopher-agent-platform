package message

import (
	"context"
	"strings"

	"gopherai/internal/conversation"
)

const (
	maxContentLength = 20_000
	maxPage          = 10_000
	maxPageSize      = 100
)

type Service struct {
	messages MessageRepository
}

func NewService(messages MessageRepository) *Service {
	return &Service{messages: messages}
}

func (s *Service) CreateUserMessage(ctx context.Context, userID uint64,
	conversationID uint64, content string) (*Message, error) {
	return s.createMessage(ctx, userID, conversationID, content, RoleUser)
}

func (s *Service) CreateAssistantMessage(ctx context.Context, userID uint64,
	conversationID uint64, content string) (*Message, error) {
	return s.createMessage(ctx, userID, conversationID, content, RoleAssistant)
}

func (s *Service) GetByID(ctx context.Context, userID, conversationID, messageID uint64) (*Message, error) {
	if userID == 0 {
		return nil, conversation.ErrInvalidUserID
	}
	if conversationID == 0 {
		return nil, ErrInvalidConversationID
	}
	if messageID == 0 {
		return nil, ErrMessageNotFound
	}
	return s.messages.GetByID(ctx, userID, conversationID, messageID)
}

func (s *Service) List(ctx context.Context,
	userID, conversationID uint64,
	page, pageSize int) ([]*Message, error) {
	if userID == 0 {
		return nil, conversation.ErrInvalidUserID
	}
	if conversationID == 0 {
		return nil, ErrInvalidConversationID
	}
	if page <= 0 || page > maxPage {
		return nil, conversation.ErrInvalidPage
	}
	if pageSize <= 0 || pageSize > maxPageSize {
		return nil, conversation.ErrInvalidPageSize
	}

	offset := (page - 1) * pageSize

	messages, err := s.messages.ListByConversationID(ctx, userID, conversationID, pageSize, offset)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

func (s *Service) ListRecent(ctx context.Context, userID,
	conversationID uint64, limit int) ([]*Message, error) {
	if userID == 0 {
		return nil, conversation.ErrInvalidUserID
	}
	if conversationID == 0 {
		return nil, ErrInvalidConversationID
	}
	if limit <= 0 {
		return nil, ErrInvalidLimit
	}

	messages, err := s.messages.ListRecentByConversationID(ctx,
		userID, conversationID, limit)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

func (s *Service) createMessage(ctx context.Context, userID uint64,
	conversationID uint64, content string, role Role) (*Message, error) {
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > maxContentLength {
		return nil, ErrInvalidContent
	}
	if userID == 0 {
		return nil, conversation.ErrInvalidUserID
	}
	if conversationID == 0 {
		return nil, ErrInvalidConversationID
	}
	newMessage := &Message{
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
	}

	if err := s.messages.Create(ctx, userID, newMessage); err != nil {
		return nil, err
	}

	return newMessage, nil
}
