package user

import "errors"

var (
	ErrInvalidEmail       = errors.New("email is invalid")
	ErrInvalidPassword    = errors.New("password is invalid")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrPasswordHash       = errors.New("password hash error")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("email or password is incorrect")
)
