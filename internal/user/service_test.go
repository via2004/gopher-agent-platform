package user

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type fakeUserRepository struct {
	createCalls int
	createdUser *User
	ctx         context.Context
	err         error
}

func (r *fakeUserRepository) Create(ctx context.Context, user *User) error {
	r.createCalls++
	r.createdUser = user
	r.ctx = ctx
	return r.err
}

func TestServiceRegister(t *testing.T) {
	repo := &fakeUserRepository{}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.Register(ctx, " User@Example.COM ", "password123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got == nil {
		t.Fatal("Register() user = nil")
	}
	if repo.createCalls != 1 {
		t.Fatalf("repository Create() calls = %d, want 1", repo.createCalls)
	}
	if repo.createdUser != got {
		t.Fatal("repository did not receive the returned user")
	}
	if repo.ctx != ctx {
		t.Fatal("Register() did not pass its context to the repository")
	}
	if got.Email != "user@example.com" {
		t.Errorf("user email = %q, want %q", got.Email, "user@example.com")
	}
	if got.PasswordHash == "password123" {
		t.Fatal("password was stored as plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("password123")); err != nil {
		t.Fatalf("stored password hash does not match password: %v", err)
	}
}

func TestServiceRegisterRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{name: "empty email", email: "", password: "password123", wantErr: ErrInvalidEmail},
		{name: "whitespace email", email: "   ", password: "password123", wantErr: ErrInvalidEmail},
		{name: "invalid email", email: "not-an-email", password: "password123", wantErr: ErrInvalidEmail},
		{name: "display name email", email: "Alice <alice@example.com>", password: "password123", wantErr: ErrInvalidEmail},
		{name: "empty password", email: "user@example.com", password: "", wantErr: ErrInvalidPassword},
		{name: "short password", email: "user@example.com", password: "1234567", wantErr: ErrInvalidPassword},
		{name: "password over bcrypt limit", email: "user@example.com", password: strings.Repeat("a", 73), wantErr: ErrInvalidPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeUserRepository{}
			service := NewService(repo)

			got, err := service.Register(context.Background(), tt.email, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("Register() user = %#v, want nil", got)
			}
			if repo.createCalls != 0 {
				t.Fatalf("repository Create() calls = %d, want 0", repo.createCalls)
			}
		})
	}
}

func TestServiceRegisterReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("create user")
	repo := &fakeUserRepository{err: repoErr}
	service := NewService(repo)

	got, err := service.Register(context.Background(), "user@example.com", "password123")
	if !errors.Is(err, repoErr) {
		t.Fatalf("Register() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("Register() user = %#v, want nil", got)
	}
	if repo.createCalls != 1 {
		t.Fatalf("repository Create() calls = %d, want 1", repo.createCalls)
	}
}
