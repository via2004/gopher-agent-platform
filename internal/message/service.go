package message

import (
	"context"
	"errors"
	"strings"

	"gopherai/internal/conversation"
)

const (
	maxContentLength = 20_000
	maxPage          = 10_000
	maxPageSize      = 100
)

var (
	ErrInvalidContent        = errors.New("content is invalid")
	ErrInvalidConversationID = errors.New("conversation ID is invalid")
)

type Service struct {
	messages MessageRepository
}

func NewService(messages MessageRepository) *Service {
	return &Service{messages: messages}
}

func (s *Service) CreateUserMessage(ctx context.Context, userID uint64,
	conversationID uint64, content string) (*Message, error) {
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
		Role:           RoleUser,
		Content:        content,
	}

	if err := s.messages.Create(ctx, userID, newMessage); err != nil {
		return nil, err
	}

	return newMessage, nil
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
