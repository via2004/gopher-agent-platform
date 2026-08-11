package platform

import "errors"

var (
	ErrTxBeginFailed  = errors.New("tx begin failed")
	ErrTxCommitFailed = errors.New("tx commit failed")
)
