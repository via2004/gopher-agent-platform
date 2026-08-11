package message

import "errors"

var (
	ErrInvalidContent        = errors.New("content is invalid")
	ErrInvalidConversationID = errors.New("conversation ID is invalid")
	ErrInvalidLimit          = errors.New("limit must be positive")
)
