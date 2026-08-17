package chatjob

import "errors"

var (
	ErrJobNotFound       = errors.New("job not found")
	ErrJobNotClaimable   = errors.New("job not claimable")
	ErrJobNotCompletable = errors.New("job not completable")
	ErrJobNotProcessing  = errors.New("job not processing")
	ErrInvalidJobID      = errors.New("job id is invalid")
	ErrInvalidContent    = errors.New("content is invalid")
	ErrInvalidChatResult = errors.New("invalid chat result")
)
