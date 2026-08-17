package platform

import "errors"

var (
	ErrTxBeginFailed             = errors.New("tx begin failed")
	ErrTxCommitFailed            = errors.New("tx commit failed")
	ErrInsertChatJobFailed       = errors.New("insert chat job failed")
	ErrGetChatJobsFailed         = errors.New("get chat jobs failed")
	ErrClaimPendingChatJobFailed = errors.New("claim pending jobs failed")
	ErrCompleteChatJobFailed     = errors.New("complete chat job failed")
	ErrFailChatJobFailed         = errors.New("fail chat job failed")
)
