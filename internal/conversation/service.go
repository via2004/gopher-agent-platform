package conversation

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxPage     = 10_000
	maxPageSize = 100
)

var (
	ErrInvalidUserID   = errors.New("user ID is invalid")
	ErrInvalidPage     = errors.New("page is invalid")
	ErrInvalidPageSize = errors.New("page size is invalid")
	ErrInvalidTitle    = errors.New("conversation title is invalid")
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
