package conversation

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidUserID = errors.New("user ID is invalid")
	ErrInvalidTitle  = errors.New("conversation title is invalid")
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
