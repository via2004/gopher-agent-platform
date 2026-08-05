package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRepository struct {
	createCalls   int
	ctx           context.Context
	created       *Conversation
	err           error
	listCalls     int
	listCtx       context.Context
	listedUserID  uint64
	listedLimit   int
	listedOffset  int
	conversations []*Conversation
	listErr       error
	getCalls      int
	getCtx        context.Context
	getUserID     uint64
	getID         uint64
	found         *Conversation
	getErr        error
	deleteCalls   int
	deleteCtx     context.Context
	deleteUserID  uint64
	deleteID      uint64
	deleteErr     error
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

func (f *fakeRepository) ListByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*Conversation, error) {
	f.listCalls++
	f.listCtx = ctx
	f.listedUserID = userID
	f.listedLimit = limit
	f.listedOffset = offset
	return f.conversations, f.listErr
}
func (f *fakeRepository) GetByIDAndUserID(ctx context.Context, userID uint64, conversationID uint64) (*Conversation, error) {
	f.getCalls++
	f.getCtx = ctx
	f.getUserID = userID
	f.getID = conversationID
	return f.found, f.getErr
}

func (f *fakeRepository) DeleteByIDAndUserID(ctx context.Context, userID uint64, conversationID uint64) error {
	f.deleteCalls++
	f.deleteCtx = ctx
	f.deleteUserID = userID
	f.deleteID = conversationID
	return f.deleteErr
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

func TestServiceList(t *testing.T) {
	want := []*Conversation{{ID: 2}, {ID: 1}}
	repo := &fakeRepository{conversations: want}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.List(ctx, 7, 3, 20)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("List() conversations = %#v, want %#v", got, want)
	}
	if repo.listCalls != 1 || repo.listedUserID != 7 || repo.listedLimit != 20 || repo.listedOffset != 40 {
		t.Fatalf("ListByUserID() = %d calls with user ID %d, limit %d, offset %d", repo.listCalls, repo.listedUserID, repo.listedLimit, repo.listedOffset)
	}
	if repo.listCtx != ctx {
		t.Fatal("List() did not pass its context to the repository")
	}
}

func TestServiceListPageBoundary(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)

	if _, err := service.List(context.Background(), 7, maxPage, maxPageSize); err != nil {
		t.Fatalf("List() at maximum page boundary error = %v", err)
	}
	if repo.listedOffset != (maxPage-1)*maxPageSize {
		t.Errorf("offset = %d, want %d", repo.listedOffset, (maxPage-1)*maxPageSize)
	}
}

func TestServiceListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		userID   uint64
		page     int
		pageSize int
		wantErr  error
	}{
		{name: "zero user ID", page: 1, pageSize: 20, wantErr: ErrInvalidUserID},
		{name: "zero page", userID: 7, pageSize: 20, wantErr: ErrInvalidPage},
		{name: "page over limit", userID: 7, page: maxPage + 1, pageSize: 20, wantErr: ErrInvalidPage},
		{name: "zero page size", userID: 7, page: 1, wantErr: ErrInvalidPageSize},
		{name: "page size over limit", userID: 7, page: 1, pageSize: maxPageSize + 1, wantErr: ErrInvalidPageSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			got, err := service.List(context.Background(), tt.userID, tt.page, tt.pageSize)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("List() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("List() conversations = %#v, want nil", got)
			}
			if repo.listCalls != 0 {
				t.Fatalf("ListByUserID() calls = %d, want 0", repo.listCalls)
			}
		})
	}
}

func TestServiceListReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("list conversations")
	repo := &fakeRepository{listErr: repoErr}
	service := NewService(repo)

	got, err := service.List(context.Background(), 7, 1, 20)
	if !errors.Is(err, repoErr) {
		t.Fatalf("List() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("List() conversations = %#v, want nil", got)
	}
}

func TestServiceGetByID(t *testing.T) {
	want := &Conversation{ID: 42, UserID: 7, Title: "Go and AI"}
	repo := &fakeRepository{found: want}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.GetByID(ctx, 7, 42)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got != want {
		t.Fatalf("GetByID() conversation = %#v, want %#v", got, want)
	}
	if repo.getCalls != 1 || repo.getUserID != 7 || repo.getID != 42 {
		t.Fatalf("GetByIDAndUserID() = %d calls with user ID %d and conversation ID %d", repo.getCalls, repo.getUserID, repo.getID)
	}
	if repo.getCtx != ctx {
		t.Fatal("GetByID() did not pass its context to the repository")
	}
}

func TestServiceGetByIDRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name           string
		userID         uint64
		conversationID uint64
		wantErr        error
	}{
		{name: "zero user ID", conversationID: 42, wantErr: ErrInvalidUserID},
		{name: "zero conversation ID", userID: 7, wantErr: ErrInvalidConversationID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			got, err := service.GetByID(context.Background(), tt.userID, tt.conversationID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetByID() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("GetByID() conversation = %#v, want nil", got)
			}
			if repo.getCalls != 0 {
				t.Fatalf("GetByIDAndUserID() calls = %d, want 0", repo.getCalls)
			}
		})
	}
}

func TestServiceGetByIDReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("query conversation")
	repo := &fakeRepository{getErr: repoErr}
	service := NewService(repo)

	got, err := service.GetByID(context.Background(), 7, 42)
	if !errors.Is(err, repoErr) {
		t.Fatalf("GetByID() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("GetByID() conversation = %#v, want nil", got)
	}
}

func TestServiceDelete(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	if err := service.Delete(ctx, 7, 42); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if repo.deleteCalls != 1 || repo.deleteUserID != 7 || repo.deleteID != 42 {
		t.Fatalf("DeleteByIDAndUserID() = %d calls with user ID %d and conversation ID %d", repo.deleteCalls, repo.deleteUserID, repo.deleteID)
	}
	if repo.deleteCtx != ctx {
		t.Fatal("Delete() did not pass its context to the repository")
	}
}

func TestServiceDeleteRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name           string
		userID         uint64
		conversationID uint64
		wantErr        error
	}{
		{name: "zero user ID", conversationID: 42, wantErr: ErrInvalidUserID},
		{name: "zero conversation ID", userID: 7, wantErr: ErrInvalidConversationID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			err := service.Delete(context.Background(), tt.userID, tt.conversationID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Delete() error = %v, want %v", err, tt.wantErr)
			}
			if repo.deleteCalls != 0 {
				t.Fatalf("DeleteByIDAndUserID() calls = %d, want 0", repo.deleteCalls)
			}
		})
	}
}

func TestServiceDeleteReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("delete conversation")
	repo := &fakeRepository{deleteErr: repoErr}
	service := NewService(repo)

	if err := service.Delete(context.Background(), 7, 42); !errors.Is(err, repoErr) {
		t.Fatalf("Delete() error = %v, want %v", err, repoErr)
	}
}
