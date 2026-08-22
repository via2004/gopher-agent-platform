package chat

import "errors"

var (
	ErrInvalidHistoryMessage = errors.New("history message is invalid")
	ErrInvalidRAGMessage     = errors.New("rag message is invalid")
	ErrInvalidRetriever      = errors.New("retriever is invalid")
)
