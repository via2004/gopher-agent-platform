package modelcall

import (
	"context"
	"errors"
	"testing"
	"time"

	"gopherai/internal/conversation"
)

type fakeRepository struct {
	createCalls int
	createCtx   context.Context
	createUser  uint64
	created     *Model
	createErr   error

	completeCalls int
	completeCtx   context.Context
	completeUser  uint64
	completed     *Model
	completeErr   error

	finishCalls int
	finishCtx   context.Context
	finishUser  uint64
	finished    *Model
	finishErr   error

	getCalls int
	getCtx   context.Context
	getID    uint64
	getUser  uint64
	got      *Model
	getErr   error
}

func (f *fakeRepository) Create(ctx context.Context, userID uint64, modelCall *Model) error {
	f.createCalls++
	f.createCtx = ctx
	f.createUser = userID
	f.created = modelCall
	if f.createErr == nil {
		modelCall.ID = 101
		modelCall.StartedAt = time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC)
	}
	return f.createErr
}

func (f *fakeRepository) CompleteModelCall(ctx context.Context, userID uint64, modelCall *Model) error {
	f.completeCalls++
	f.completeCtx = ctx
	f.completeUser = userID
	f.completed = modelCall
	if f.completeErr == nil {
		finishedAt := time.Date(2026, time.August, 11, 10, 0, 1, 0, time.UTC)
		modelCall.Status = StatusCompleted
		modelCall.FinishedAt = &finishedAt
	}
	return f.completeErr
}

func (f *fakeRepository) FinishModelCall(ctx context.Context, userID uint64, modelCall *Model) error {
	f.finishCalls++
	f.finishCtx = ctx
	f.finishUser = userID
	f.finished = modelCall
	if f.finishErr == nil {
		finishedAt := time.Date(2026, time.August, 11, 10, 0, 1, 0, time.UTC)
		modelCall.FinishedAt = &finishedAt
	}
	return f.finishErr
}

func (f *fakeRepository) GetModelCall(ctx context.Context, modelCallID, userID uint64) (*Model, error) {
	f.getCalls++
	f.getCtx = ctx
	f.getID = modelCallID
	f.getUser = userID
	return f.got, f.getErr
}

func TestServiceStart(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	call := &Model{
		ConversationID:   9,
		RequestMessageID: 11,
		Provider:         "  OpenAI  ",
	}

	if err := service.Start(ctx, 7, call); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if repo.createCalls != 1 || repo.created != call || repo.createUser != 7 {
		t.Fatalf("Create() = %d calls with model call %#v and user ID %d", repo.createCalls, repo.created, repo.createUser)
	}
	if repo.createCtx != ctx {
		t.Fatal("Start() did not pass its context to the repository")
	}
	if call.ID != 101 || call.StartedAt.IsZero() || call.Status != StatusRunning || call.Provider != "OpenAI" {
		t.Fatalf("model call after Start() = %#v", call)
	}
}

func TestServiceStartRejectsInvalidInput(t *testing.T) {
	valid := func() *Model {
		return &Model{ConversationID: 9, RequestMessageID: 11, Provider: "OpenAI"}
	}
	tests := []struct {
		name      string
		userID    uint64
		modelCall *Model
		wantErr   error
	}{
		{name: "zero user ID", modelCall: valid(), wantErr: conversation.ErrInvalidUserID},
		{name: "nil model call", userID: 7, wantErr: ErrModelCallIsEmpty},
		{name: "zero conversation ID", userID: 7, modelCall: &Model{RequestMessageID: 11, Provider: "OpenAI"}, wantErr: conversation.ErrInvalidConversationID},
		{name: "zero request message ID", userID: 7, modelCall: &Model{ConversationID: 9, Provider: "OpenAI"}, wantErr: ErrInvalidRequestMessageID},
		{name: "empty provider", userID: 7, modelCall: &Model{ConversationID: 9, RequestMessageID: 11}, wantErr: ErrInvalidModelProvider},
		{name: "whitespace provider", userID: 7, modelCall: &Model{ConversationID: 9, RequestMessageID: 11, Provider: " \n\t "}, wantErr: ErrInvalidModelProvider},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			err := service.Start(context.Background(), tt.userID, tt.modelCall)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Start() error = %v, want %v", err, tt.wantErr)
			}
			if repo.createCalls != 0 {
				t.Fatalf("Create() calls = %d, want 0", repo.createCalls)
			}
		})
	}
}

func TestServiceStartReturnsRepositoryErrorWithoutSettingRunning(t *testing.T) {
	repoErr := errors.New("create model call")
	repo := &fakeRepository{createErr: repoErr}
	service := NewService(repo)
	call := &Model{ConversationID: 9, RequestMessageID: 11, Provider: "OpenAI"}

	err := service.Start(context.Background(), 7, call)
	if !errors.Is(err, repoErr) {
		t.Fatalf("Start() error = %v, want %v", err, repoErr)
	}
	if call.Status == StatusRunning {
		t.Fatal("Start() set running status after repository failure")
	}
}

func TestServiceComplete(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	assistantID := uint64(13)
	call := &Model{ID: 101, AssistantMessageID: &assistantID}

	if err := service.Complete(ctx, 7, call); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if repo.completeCalls != 1 || repo.completed != call || repo.completeUser != 7 {
		t.Fatalf("CompleteModelCall() = %d calls with model call %#v and user ID %d", repo.completeCalls, repo.completed, repo.completeUser)
	}
	if repo.completeCtx != ctx {
		t.Fatal("Complete() did not pass its context to the repository")
	}
	if call.Status != StatusCompleted || call.FinishedAt == nil {
		t.Fatalf("model call after Complete() = %#v", call)
	}
}

func TestServiceCompleteRejectsInvalidInput(t *testing.T) {
	validAssistantID := uint64(13)
	zeroAssistantID := uint64(0)
	tests := []struct {
		name      string
		userID    uint64
		modelCall *Model
		wantErr   error
	}{
		{name: "zero user ID", modelCall: &Model{ID: 101, AssistantMessageID: &validAssistantID}, wantErr: conversation.ErrInvalidUserID},
		{name: "nil model call", userID: 7, wantErr: ErrModelCallIsEmpty},
		{name: "zero model call ID", userID: 7, modelCall: &Model{AssistantMessageID: &validAssistantID}, wantErr: ErrInvalidModelCallID},
		{name: "nil assistant message ID", userID: 7, modelCall: &Model{ID: 101}, wantErr: ErrInvalidAssistantMessageID},
		{name: "zero assistant message ID", userID: 7, modelCall: &Model{ID: 101, AssistantMessageID: &zeroAssistantID}, wantErr: ErrInvalidAssistantMessageID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			err := service.Complete(context.Background(), tt.userID, tt.modelCall)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete() error = %v, want %v", err, tt.wantErr)
			}
			if repo.completeCalls != 0 {
				t.Fatalf("CompleteModelCall() calls = %d, want 0", repo.completeCalls)
			}
		})
	}
}

func TestServiceCompleteReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("complete model call")
	repo := &fakeRepository{completeErr: repoErr}
	service := NewService(repo)
	assistantID := uint64(13)

	err := service.Complete(context.Background(), 7, &Model{ID: 101, AssistantMessageID: &assistantID})
	if !errors.Is(err, repoErr) {
		t.Fatalf("Complete() error = %v, want %v", err, repoErr)
	}
}

func TestServiceFinishAcceptsFailureTerminalStatuses(t *testing.T) {
	statuses := []Status{StatusFailed, StatusCancelled, StatusTimedOut, StatusIncomplete}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)
			call := &Model{ID: 101, Status: status}

			if err := service.Finish(context.Background(), 7, call); err != nil {
				t.Fatalf("Finish() error = %v", err)
			}
			if repo.finishCalls != 1 || repo.finished != call || repo.finishUser != 7 {
				t.Fatalf("FinishModelCall() = %d calls with model call %#v and user ID %d", repo.finishCalls, repo.finished, repo.finishUser)
			}
			if call.FinishedAt == nil {
				t.Fatal("Finish() result has nil FinishedAt")
			}
		})
	}
}

func TestServiceFinishRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		userID    uint64
		modelCall *Model
		wantErr   error
	}{
		{name: "zero user ID", modelCall: &Model{ID: 101, Status: StatusFailed}, wantErr: conversation.ErrInvalidUserID},
		{name: "nil model call", userID: 7, wantErr: ErrModelCallIsEmpty},
		{name: "zero model call ID", userID: 7, modelCall: &Model{Status: StatusFailed}, wantErr: ErrInvalidModelCallID},
		{name: "empty status", userID: 7, modelCall: &Model{ID: 101}, wantErr: ErrInvalidModelCallStatus},
		{name: "running status", userID: 7, modelCall: &Model{ID: 101, Status: StatusRunning}, wantErr: ErrInvalidModelCallStatus},
		{name: "completed status", userID: 7, modelCall: &Model{ID: 101, Status: StatusCompleted}, wantErr: ErrInvalidModelCallStatus},
		{name: "unknown status", userID: 7, modelCall: &Model{ID: 101, Status: Status("unknown")}, wantErr: ErrInvalidModelCallStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			err := service.Finish(context.Background(), tt.userID, tt.modelCall)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Finish() error = %v, want %v", err, tt.wantErr)
			}
			if repo.finishCalls != 0 {
				t.Fatalf("FinishModelCall() calls = %d, want 0", repo.finishCalls)
			}
		})
	}
}

func TestServiceFinishReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("finish model call")
	repo := &fakeRepository{finishErr: repoErr}
	service := NewService(repo)

	err := service.Finish(context.Background(), 7, &Model{ID: 101, Status: StatusFailed})
	if !errors.Is(err, repoErr) {
		t.Fatalf("Finish() error = %v, want %v", err, repoErr)
	}
}

func TestServiceGet(t *testing.T) {
	want := &Model{ID: 101, ConversationID: 9, Status: StatusCompleted}
	repo := &fakeRepository{got: want}
	service := NewService(repo)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.Get(ctx, 7, 101)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != want {
		t.Fatalf("Get() = %#v, want %#v", got, want)
	}
	if repo.getCalls != 1 || repo.getID != 101 || repo.getUser != 7 || repo.getCtx != ctx {
		t.Fatalf("GetModelCall() = %d calls with ID %d and user ID %d", repo.getCalls, repo.getID, repo.getUser)
	}
}

func TestServiceGetRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		userID  uint64
		callID  uint64
		wantErr error
	}{
		{name: "zero user ID", callID: 101, wantErr: conversation.ErrInvalidUserID},
		{name: "zero model call ID", userID: 7, wantErr: ErrInvalidModelCallID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)

			got, err := service.Get(context.Background(), tt.userID, tt.callID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("Get() = %#v, want nil", got)
			}
			if repo.getCalls != 0 {
				t.Fatalf("GetModelCall() calls = %d, want 0", repo.getCalls)
			}
		})
	}
}

func TestServiceGetReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("get model call")
	repo := &fakeRepository{getErr: repoErr}
	service := NewService(repo)

	got, err := service.Get(context.Background(), 7, 101)
	if !errors.Is(err, repoErr) {
		t.Fatalf("Get() error = %v, want %v", err, repoErr)
	}
	if got != nil {
		t.Fatalf("Get() = %#v, want nil", got)
	}
}
