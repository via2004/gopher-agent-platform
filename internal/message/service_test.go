package message

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gopherai/internal/conversation"
)

type fakeMessageRepository struct {
	createCalls int
	createCtx   context.Context
	createUser  uint64
	created     *Message
	createErr   error
	listCalls   int
	listCtx     context.Context
	listUser    uint64
	listConv    uint64
	listLimit   int
	listOffset  int
	listed      []*Message
	listErr     error
	recentCalls int
	recentCtx   context.Context
	recentUser  uint64
	recentConv  uint64
	recentLimit int
	recent      []*Message
	recentErr   error
}

func (f *fakeMessageRepository) Create(ctx context.Context, userID uint64, message *Message) error {
	f.createCalls++
	f.createCtx = ctx
	f.createUser = userID
	f.created = message
	if f.createErr == nil {
		message.ID = 42
		message.CreatedAt = time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	}
	return f.createErr
}

func (f *fakeMessageRepository) ListByConversationID(
	ctx context.Context,
	userID uint64,
	conversationID uint64,
	limit, offset int,
) ([]*Message, error) {
	f.listCalls++
	f.listCtx = ctx
	f.listUser = userID
	f.listConv = conversationID
	f.listLimit = limit
	f.listOffset = offset
	return f.listed, f.listErr
}
func (f *fakeMessageRepository) ListRecentByConversationID(ctx context.Context, userID uint64,
	conversationID uint64, limit int) ([]*Message, error) {
	f.recentCalls++
	f.recentCtx = ctx
	f.recentUser = userID
	f.recentConv = conversationID
	f.recentLimit = limit
	return f.recent, f.recentErr
}

func TestServiceCreateUserMessage(t *testing.T) {
	repo := &fakeMessageRepository{}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	created, err := service.CreateUserMessage(ctx, 7, 9, "  explain Go interfaces  ")
	if err != nil {
		t.Fatalf("CreateUserMessage() error = %v", err)
	}
	if created != repo.created {
		t.Fatal("CreateUserMessage() did not return the message passed to the repository")
	}
	if created.ID != 42 || created.ConversationID != 9 || created.Role != RoleUser || created.Content != "explain Go interfaces" || created.CreatedAt.IsZero() {
		t.Errorf("created message = %#v", created)
	}
	if repo.createCalls != 1 || repo.createUser != 7 {
		t.Fatalf("Create() = %d calls with user ID %d, want 1 call with user ID 7", repo.createCalls, repo.createUser)
	}
	if repo.createCtx != ctx {
		t.Fatal("CreateUserMessage() did not pass its context to the repository")
	}
}

func TestServiceCreateUserMessageAcceptsContentAtLimit(t *testing.T) {
	repo := &fakeMessageRepository{}
	service := NewService(repo)
	content := strings.Repeat("界", maxContentLength)

	created, err := service.CreateUserMessage(context.Background(), 7, 9, content)
	if err != nil {
		t.Fatalf("CreateUserMessage() error = %v", err)
	}
	if created.Content != content {
		t.Fatal("CreateUserMessage() changed valid Unicode content")
	}
}

func TestServiceCreateUserMessageRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name           string
		userID         uint64
		conversationID uint64
		content        string
		wantErr        error
	}{
		{name: "empty content", userID: 7, conversationID: 9, wantErr: ErrInvalidContent},
		{name: "whitespace content", userID: 7, conversationID: 9, content: "  \n\t", wantErr: ErrInvalidContent},
		{name: "content over limit", userID: 7, conversationID: 9, content: strings.Repeat("界", maxContentLength+1), wantErr: ErrInvalidContent},
		{name: "zero user ID", conversationID: 9, content: "hello", wantErr: conversation.ErrInvalidUserID},
		{name: "zero conversation ID", userID: 7, content: "hello", wantErr: ErrInvalidConversationID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeMessageRepository{}
			service := NewService(repo)

			created, err := service.CreateUserMessage(context.Background(), tt.userID, tt.conversationID, tt.content)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateUserMessage() error = %v, want %v", err, tt.wantErr)
			}
			if created != nil {
				t.Fatalf("CreateUserMessage() message = %#v, want nil", created)
			}
			if repo.createCalls != 0 {
				t.Fatalf("Create() calls = %d, want 0", repo.createCalls)
			}
		})
	}
}

func TestServiceCreateUserMessageReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("insert message")
	repo := &fakeMessageRepository{createErr: repoErr}
	service := NewService(repo)

	created, err := service.CreateUserMessage(context.Background(), 7, 9, "hello")
	if !errors.Is(err, repoErr) {
		t.Fatalf("CreateUserMessage() error = %v, want %v", err, repoErr)
	}
	if created != nil {
		t.Fatalf("CreateUserMessage() message = %#v, want nil", created)
	}
}

func TestServiceCreateAssistantMessage(t *testing.T) {
	repo := &fakeMessageRepository{}
	service := NewService(repo)

	created, err := service.CreateAssistantMessage(context.Background(), 7, 9, "  model response  ")
	if err != nil {
		t.Fatalf("CreateAssistantMessage() error = %v", err)
	}
	if created != repo.created || created.Role != RoleAssistant || created.Content != "model response" {
		t.Fatalf("created assistant message = %#v", created)
	}
}

func TestServiceList(t *testing.T) {
	want := []*Message{{ID: 1}, {ID: 2}}
	repo := &fakeMessageRepository{listed: want}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.List(ctx, 7, 9, 3, 20)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("List() messages = %#v, want %#v", got, want)
	}
	if repo.listCalls != 1 || repo.listUser != 7 || repo.listConv != 9 || repo.listLimit != 20 || repo.listOffset != 40 {
		t.Fatalf("ListByConversationID() = %d calls with user ID %d, conversation ID %d, limit %d, offset %d", repo.listCalls, repo.listUser, repo.listConv, repo.listLimit, repo.listOffset)
	}
	if repo.listCtx != ctx {
		t.Fatal("List() did not pass its context to the repository")
	}
}

func TestServiceListPageBoundary(t *testing.T) {
	repo := &fakeMessageRepository{}
	service := NewService(repo)

	if _, err := service.List(context.Background(), 7, 9, maxPage, maxPageSize); err != nil {
		t.Fatalf("List() at maximum page boundary error = %v", err)
	}
	if repo.listOffset != (maxPage-1)*maxPageSize {
		t.Errorf("offset = %d, want %d", repo.listOffset, (maxPage-1)*maxPageSize)
	}
}

func TestServiceListRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name           string
		userID         uint64
		conversationID uint64
		page           int
		pageSize       int
		wantErr        error
	}{
		{name: "zero user ID", conversationID: 9, page: 1, pageSize: 20, wantErr: conversation.ErrInvalidUserID},
		{name: "zero conversation ID", userID: 7, page: 1, pageSize: 20, wantErr: ErrInvalidConversationID},
		{name: "zero page", userID: 7, conversationID: 9, pageSize: 20, wantErr: conversation.ErrInvalidPage},
		{name: "page over limit", userID: 7, conversationID: 9, page: maxPage + 1, pageSize: 20, wantErr: conversation.ErrInvalidPage},
		{name: "zero page size", userID: 7, conversationID: 9, page: 1, wantErr: conversation.ErrInvalidPageSize},
		{name: "page size over limit", userID: 7, conversationID: 9, page: 1, pageSize: maxPageSize + 1, wantErr: conversation.ErrInvalidPageSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeMessageRepository{}
			service := NewService(repo)

			got, err := service.List(context.Background(), tt.userID, tt.conversationID, tt.page, tt.pageSize)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("List() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("List() messages = %#v, want nil", got)
			}
			if repo.listCalls != 0 {
				t.Fatalf("ListByConversationID() calls = %d, want 0", repo.listCalls)
			}
		})
	}
}

func TestServiceListReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("query messages")
	repo := &fakeMessageRepository{listErr: repoErr}
	service := NewService(repo)

	got, err := service.List(context.Background(), 7, 9, 1, 20)
	if !errors.Is(err, repoErr) {
		t.Fatalf("List() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("List() messages = %#v, want nil", got)
	}
}

func TestServiceListRecent(t *testing.T) {
	want := []*Message{{ID: 2}, {ID: 3}}
	repo := &fakeMessageRepository{recent: want}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.ListRecent(ctx, 7, 9, 40)
	if err != nil {
		t.Fatalf("ListRecent() error = %v", err)
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ListRecent() messages = %#v, want %#v", got, want)
	}
	if repo.recentCalls != 1 || repo.recentUser != 7 || repo.recentConv != 9 || repo.recentLimit != 40 {
		t.Fatalf("ListRecentByConversationID() = %d calls with user ID %d, conversation ID %d, limit %d", repo.recentCalls, repo.recentUser, repo.recentConv, repo.recentLimit)
	}
	if repo.recentCtx != ctx {
		t.Fatal("ListRecent() did not pass its context to the repository")
	}
}

func TestServiceListRecentRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name           string
		userID         uint64
		conversationID uint64
		limit          int
		wantErr        error
	}{
		{name: "zero user ID", conversationID: 9, limit: 40, wantErr: conversation.ErrInvalidUserID},
		{name: "zero conversation ID", userID: 7, limit: 40, wantErr: ErrInvalidConversationID},
		{name: "zero limit", userID: 7, conversationID: 9, wantErr: ErrInvalidLimit},
		{name: "negative limit", userID: 7, conversationID: 9, limit: -1, wantErr: ErrInvalidLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeMessageRepository{}
			service := NewService(repo)

			got, err := service.ListRecent(context.Background(), tt.userID, tt.conversationID, tt.limit)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListRecent() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("ListRecent() messages = %#v, want nil", got)
			}
			if repo.recentCalls != 0 {
				t.Fatalf("ListRecentByConversationID() calls = %d, want 0", repo.recentCalls)
			}
		})
	}
}

func TestServiceListRecentReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("query recent messages")
	repo := &fakeMessageRepository{recentErr: repoErr}
	service := NewService(repo)

	got, err := service.ListRecent(context.Background(), 7, 9, 40)
	if !errors.Is(err, repoErr) {
		t.Fatalf("ListRecent() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("ListRecent() messages = %#v, want nil", got)
	}
}
