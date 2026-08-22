package filesystem

import "errors"

var (
	ErrInvalidUserID = errors.New("user id is invalid")
	ErrEmptyDocument = errors.New("document is empty")
	ErrInvalidRoot   = errors.New("root is a invalid directory")
)
