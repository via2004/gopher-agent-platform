package conversation

import (
	"context"
	"strings"
	"unicode/utf8"
)

const (
	maxPage     = 10_000
	maxPageSize = 100
)

type Service struct {
	conversations Repository
}

func NewService(conversations Repository) *Service {
	return &Service{conversations: conversations}
}

func (s *Service) Create(ctx context.Context, userID uint64, title string) (*Conversation, error) {
	if userID == 0 {
		return nil, ErrInvalidUserID
	}
	title = strings.TrimSpace(title)

	if title == "" || utf8.RuneCountInString(title) > 200 {
		return nil, ErrInvalidTitle
	}

	newConversation := &Conversation{
		UserID: userID,
		Title:  title,
	}

	if err := s.conversations.Create(ctx, newConversation); err != nil {
		return nil, err
	}

	return newConversation, nil
}

func (s *Service) List(ctx context.Context, userID uint64, page, pageSize int) ([]*Conversation, error) {
	if userID == 0 {
		return nil, ErrInvalidUserID
	}
	if page <= 0 || page > maxPage {
		return nil, ErrInvalidPage
	}
	if pageSize <= 0 || pageSize > maxPageSize {
		return nil, ErrInvalidPageSize
	}

	offset := (page - 1) * pageSize

	conversations, err := s.conversations.ListByUserID(ctx, userID, pageSize, offset)
	if err != nil {
		return nil, err
	}

	return conversations, nil
}

func (s *Service) GetByID(ctx context.Context, userID, conversationID uint64) (*Conversation, error) {
	if userID == 0 {
		return nil, ErrInvalidUserID
	}
	if conversationID == 0 {
		return nil, ErrInvalidConversationID
	}

	conversation, err := s.conversations.GetByIDAndUserID(ctx, userID, conversationID)

	if err != nil {
		return nil, err
	}

	return conversation, nil
}

func (s *Service) Delete(ctx context.Context, userID uint64, conversationID uint64) error {
	if userID == 0 {
		return ErrInvalidUserID
	}
	if conversationID == 0 {
		return ErrInvalidConversationID
	}

	return s.conversations.DeleteByIDAndUserID(ctx, userID, conversationID)
}
