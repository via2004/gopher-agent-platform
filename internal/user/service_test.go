package user

import (
	"context"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"testing"
)

type fakeUserRepository struct {
	createCalls     int
	createdUser     *User
	ctx             context.Context
	err             error
	getByEmailCalls int
	queriedEmail    string
	queryCtx        context.Context
	queryUser       *User
	queryErr        error
	getByIDCalls    int
	queriedUserID   uint64
	getByIDCtx      context.Context
	userByID        *User
	getByIDErr      error
}

type fakeEmailVerifier struct {
	calls       int
	ctx         context.Context
	email       string
	code        string
	err         error
	createCalls *int
}

func (f *fakeEmailVerifier) VerifyAndConsume(ctx context.Context, email, code string) error {
	f.calls++
	f.ctx = ctx
	f.email = email
	f.code = code
	if f.createCalls != nil && *f.createCalls != 0 {
		return errors.New("user was created before verification")
	}
	return f.err
}

func (r *fakeUserRepository) Create(ctx context.Context, user *User) error {
	r.createCalls++
	r.createdUser = user
	r.ctx = ctx
	return r.err
}

func (r *fakeUserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	r.getByEmailCalls++
	r.queriedEmail = email
	r.queryCtx = ctx
	return r.queryUser, r.queryErr
}

func (r *fakeUserRepository) GetByID(ctx context.Context, userID uint64) (*User, error) {
	r.getByIDCalls++
	r.queriedUserID = userID
	r.getByIDCtx = ctx
	return r.userByID, r.getByIDErr
}

func TestServiceRegister(t *testing.T) {
	repo := &fakeUserRepository{}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.Register(ctx, " User@Example.COM ", "password123", "")
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

			got, err := service.Register(context.Background(), tt.email, tt.password, "")
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

	got, err := service.Register(context.Background(), "user@example.com", "password123", "")
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

func TestServiceRegisterVerifiesEmailBeforeCreatingUser(t *testing.T) {
	repo := &fakeUserRepository{}
	verifier := &fakeEmailVerifier{createCalls: &repo.createCalls}
	service := NewServiceWithEmailVerifier(repo, verifier)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.Register(ctx, " User@Example.COM ", "password123", " 123456 ")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got == nil || repo.createCalls != 1 {
		t.Fatalf("Register() user = %#v, Create() calls = %d", got, repo.createCalls)
	}
	if verifier.calls != 1 || verifier.email != "user@example.com" || verifier.code != " 123456 " || verifier.ctx != ctx {
		t.Fatalf("VerifyAndConsume() = %d calls with email %q and code %q", verifier.calls, verifier.email, verifier.code)
	}
}

func TestServiceRegisterStopsWhenEmailVerificationFails(t *testing.T) {
	wantErr := errors.New("verification failed")
	repo := &fakeUserRepository{}
	verifier := &fakeEmailVerifier{err: wantErr, createCalls: &repo.createCalls}
	service := NewServiceWithEmailVerifier(repo, verifier)

	got, err := service.Register(context.Background(), "user@example.com", "password123", "123456")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Register() error = %v, want %v", err, wantErr)
	}
	if got != nil || repo.createCalls != 0 || verifier.calls != 1 {
		t.Fatalf("Register() user = %#v, Create calls = %d, Verify calls = %d", got, repo.createCalls, verifier.calls)
	}
}

func TestServiceRegisterValidatesInputBeforeEmailVerification(t *testing.T) {
	repo := &fakeUserRepository{}
	verifier := &fakeEmailVerifier{}
	service := NewServiceWithEmailVerifier(repo, verifier)

	if _, err := service.Register(context.Background(), "invalid", "password123", "123456"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("Register() error = %v, want %v", err, ErrInvalidEmail)
	}
	if _, err := service.Register(context.Background(), "user@example.com", "short", "123456"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Register() error = %v, want %v", err, ErrInvalidPassword)
	}
	if verifier.calls != 0 || repo.createCalls != 0 {
		t.Fatalf("calls = Verify %d, Create %d; want both 0", verifier.calls, repo.createCalls)
	}
}

func TestServiceRegisterWithoutVerifierKeepsExistingBehavior(t *testing.T) {
	repo := &fakeUserRepository{}
	got, err := NewService(repo).Register(context.Background(), "user@example.com", "password123", "not-checked")
	if err != nil || got == nil || repo.createCalls != 1 {
		t.Fatalf("Register() = %#v, %v; Create calls = %d", got, err, repo.createCalls)
	}
}

func TestServiceLogin(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate password hash: %v", err)
	}
	wantUser := &User{
		ID:           42,
		Email:        "user@example.com",
		PasswordHash: string(passwordHash),
	}
	repo := &fakeUserRepository{queryUser: wantUser}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.Login(ctx, " User@Example.COM ", "password123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got != wantUser {
		t.Fatalf("Login() user = %#v, want %#v", got, wantUser)
	}
	if repo.getByEmailCalls != 1 {
		t.Fatalf("GetByEmail() calls = %d, want 1", repo.getByEmailCalls)
	}
	if repo.queriedEmail != "user@example.com" {
		t.Errorf("GetByEmail() email = %q, want %q", repo.queriedEmail, "user@example.com")
	}
	if repo.queryCtx != ctx {
		t.Fatal("Login() did not pass its context to GetByEmail")
	}
}

func TestServiceLoginRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{name: "empty email", email: "", password: "password123", wantErr: ErrInvalidEmail},
		{name: "whitespace email", email: "   ", password: "password123", wantErr: ErrInvalidEmail},
		{name: "invalid email", email: "not-an-email", password: "password123", wantErr: ErrInvalidEmail},
		{name: "empty password", email: "user@example.com", password: "", wantErr: ErrInvalidPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeUserRepository{}
			service := NewService(repo)

			got, err := service.Login(context.Background(), tt.email, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Login() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("Login() user = %#v, want nil", got)
			}
			if repo.getByEmailCalls != 0 {
				t.Fatalf("GetByEmail() calls = %d, want 0", repo.getByEmailCalls)
			}
		})
	}
}

func TestServiceLoginReturnsInvalidCredentials(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate password hash: %v", err)
	}

	tests := []struct {
		name string
		repo *fakeUserRepository
	}{
		{
			name: "user not found",
			repo: &fakeUserRepository{queryErr: ErrUserNotFound},
		},
		{
			name: "wrong password",
			repo: &fakeUserRepository{queryUser: &User{PasswordHash: string(passwordHash)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(tt.repo)
			got, err := service.Login(context.Background(), "user@example.com", "wrong-password")
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want %v", err, ErrInvalidCredentials)
			}
			if got != nil {
				t.Fatalf("Login() user = %#v, want nil", got)
			}
		})
	}
}

func TestServiceLoginReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("query user")
	repo := &fakeUserRepository{queryErr: repoErr}
	service := NewService(repo)

	got, err := service.Login(context.Background(), "user@example.com", "password123")
	if !errors.Is(err, repoErr) {
		t.Fatalf("Login() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("Login() user = %#v, want nil", got)
	}
}

func TestServiceLoginReportsInvalidStoredHash(t *testing.T) {
	repo := &fakeUserRepository{queryUser: &User{PasswordHash: "not-a-bcrypt-hash"}}
	service := NewService(repo)

	got, err := service.Login(context.Background(), "user@example.com", "password123")
	if err == nil {
		t.Fatal("Login() error = nil, want stored hash error")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, should not be invalid credentials", err)
	}
	if got != nil {
		t.Fatalf("Login() user = %#v, want nil", got)
	}
}

func TestDummyBcryptHashIsValid(t *testing.T) {
	err := bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte("definitely-not-the-dummy-password"))
	if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("dummy bcrypt hash error = %v, want password mismatch", err)
	}
}

func TestServiceGetByID(t *testing.T) {
	wantUser := &User{ID: 42, Email: "user@example.com"}
	repo := &fakeUserRepository{userByID: wantUser}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.GetByID(ctx, 42)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got != wantUser {
		t.Fatalf("GetByID() user = %#v, want %#v", got, wantUser)
	}
	if repo.getByIDCalls != 1 || repo.queriedUserID != 42 {
		t.Fatalf("repository GetByID() = %d calls with user ID %d, want 1 call with user ID 42", repo.getByIDCalls, repo.queriedUserID)
	}
	if repo.getByIDCtx != ctx {
		t.Fatal("GetByID() did not pass its context to the repository")
	}
}

func TestServiceGetByIDReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("query user by ID")
	repo := &fakeUserRepository{getByIDErr: repoErr}
	service := NewService(repo)

	got, err := service.GetByID(context.Background(), 42)
	if !errors.Is(err, repoErr) {
		t.Fatalf("GetByID() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("GetByID() user = %#v, want nil", got)
	}
}
