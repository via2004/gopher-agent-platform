package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRepository struct {
	createCalls int
	ctx         context.Context
	created     *Conversation
	err         error
}

func (f *fakeRepository) Create(ctx context.Context, conversation *Conversation) error {
	f.createCalls++
	f.ctx = ctx
	f.created = conversation
	if f.err == nil {
		conversation.ID = 42
	}
	return f.err
}

func TestServiceCreate(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	created, err := service.Create(ctx, 7, "  Go and AI  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created != repo.created {
		t.Fatal("Create() did not return the conversation passed to the repository")
	}
	if created.ID != 42 || created.UserID != 7 || created.Title != "Go and AI" {
		t.Errorf("created conversation = %#v", created)
	}
	if repo.createCalls != 1 {
		t.Fatalf("repository Create() calls = %d, want 1", repo.createCalls)
	}
	if repo.ctx != ctx {
		t.Fatal("Create() did not pass its context to the repository")
	}
}

func TestServiceCreateAcceptsTwoHundredUnicodeCharacters(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	title := strings.Repeat("界", 200)

	created, err := service.Create(context.Background(), 7, title)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Title != title {
		t.Errorf("title = %q, want %q", created.Title, title)
	}
}

func TestServiceCreateRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		userID  uint64
		title   string
		wantErr error
	}{
		{name: "zero user ID", title: "Go and AI", wantErr: ErrInvalidUserID},
		{name: "empty title", userID: 7, wantErr: ErrInvalidTitle},
		{name: "whitespace title", userID: 7, title: "   ", wantErr: ErrInvalidTitle},
		{name: "title over limit", userID: 7, title: strings.Repeat("界", 201), wantErr: ErrInvalidTitle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			created, err := service.Create(context.Background(), tt.userID, tt.title)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, tt.wantErr)
			}
			if created != nil {
				t.Fatalf("Create() conversation = %#v, want nil", created)
			}
			if repo.createCalls != 0 {
				t.Fatalf("repository Create() calls = %d, want 0", repo.createCalls)
			}
		})
	}
}

func TestServiceCreateReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("insert conversation")
	repo := &fakeRepository{err: repoErr}
	service := NewService(repo)

	created, err := service.Create(context.Background(), 7, "Go and AI")
	if !errors.Is(err, repoErr) {
		t.Fatalf("Create() error = %v, want %v", err, repoErr)
	}
	if created != nil {
		t.Fatalf("Create() conversation = %#v, want nil", created)
	}
}
