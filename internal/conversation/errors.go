package conversation

import "errors"

var (
	ErrInvalidUserID         = errors.New("user ID is invalid")
	ErrInvalidPage           = errors.New("page is invalid")
	ErrInvalidPageSize       = errors.New("page size is invalid")
	ErrInvalidTitle          = errors.New("conversation title is invalid")
	ErrConversationNotFound  = errors.New("conversation not found")
	ErrInvalidConversationID = errors.New("conversation ID is invalid")
)
