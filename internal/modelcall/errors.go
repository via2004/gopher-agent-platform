package modelcall

import "errors"

var (
	ErrInvalidRequestMessageID   = errors.New("request message ID is invalid")
	ErrInvalidModelCallID        = errors.New("model call ID is invalid")
	ErrInvalidModelProvider      = errors.New("model provider is invalid")
	ErrInvalidAssistantMessageID = errors.New("assistant message is invalid")
	ErrInvalidModelCallStatus    = errors.New("model call status is invalid")
	ErrModelCallIsEmpty          = errors.New("model call is nil")
	ErrModelCallRepository       = errors.New("model call failed")
	ErrModelCallNotFound         = errors.New("model not found")
)
