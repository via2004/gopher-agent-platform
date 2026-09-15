package user

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"net/mail"
	"strings"
)

const dummyBcryptHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

type Service struct {
	users    UserRepository
	verifier EmailVerifier
}

func NewService(users UserRepository) *Service {
	return &Service{users: users}
}

// NewServiceWithEmailVerifier 创建注册时必须验证邮箱验证码的用户服务。
func NewServiceWithEmailVerifier(users UserRepository, verifier EmailVerifier) *Service {
	return &Service{users: users, verifier: verifier}
}

func valid(email string) bool {
	addr, err := mail.ParseAddress(email)

	return err == nil && addr.Address == email
}

func (s *Service) Register(ctx context.Context, email, password, verificationCode string) (*User, error) {
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
	if s.verifier != nil {
		if err := s.verifier.VerifyAndConsume(ctx, email, verificationCode); err != nil {
			return nil, err
		}
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

func (s *Service) Login(ctx context.Context, email string, password string) (*User, error) {
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if password == "" {
		return nil, ErrInvalidPassword
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if !valid(email) {
		return nil, ErrInvalidEmail
	}

	queryUser, err := s.users.GetByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrUserNotFound):
		bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
		return nil, ErrInvalidCredentials
	case err != nil:
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(queryUser.PasswordHash), []byte(password))
	switch {
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return nil, ErrInvalidCredentials
	case err != nil:
		return nil, fmt.Errorf("compare password hash: %w", err)
	}

	return queryUser, nil
}

func (s *Service) GetByID(ctx context.Context, userID uint64) (*User, error) {
	queryUser, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return queryUser, nil
}
