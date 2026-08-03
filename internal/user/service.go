package user

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"net/mail"
	"strings"
)

var (
	ErrInvalidEmail       = errors.New("email is invalid")
	ErrInvalidPassword    = errors.New("password is invalid")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrPasswordHash       = errors.New("password hash error")
)

type Service struct {
	users UserRepository
}

func NewService(users UserRepository) *Service {
	return &Service{users: users}
}

func valid(email string) bool {
	addr, err := mail.ParseAddress(email)

	return err == nil && addr.Address == email
}

func (s *Service) Register(ctx context.Context, email string, password string) (*User, error) {
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if password == "" {
		return nil, ErrInvalidPassword
	}

	// limit the password length
	if len := len(password); len < 8 || len > 72 {
		return nil, ErrInvalidPassword
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if !valid(email) {
		return nil, ErrInvalidEmail
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPasswordHash, err)
	}
	user := &User{
		Email:        email,
		PasswordHash: string(passwordHash),
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}
