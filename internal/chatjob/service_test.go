package chatjob

import (
	"context"
	"errors"
	"testing"
	"time"

	"gopherai/internal/chat"
	"gopherai/internal/conversation"
)

type fakeRepository struct {
	createCalls          int
	createCtx            context.Context
	createUserID         uint64
	createConversationID uint64
	createContent        string
	created              *Job
	createErr            error

	getCalls  int
	getCtx    context.Context
	getUserID uint64
	getJobID  uint64
	got       *Job
	getErr    error

	claimCalls int
	claimCtx   context.Context
	claimJobID uint64
	claimed    *Job
	claimUser  uint64
	claimErr   error

	completeCalls     int
	completeCtx       context.Context
	completeJobID     uint64
	completeMessageID uint64
	completeErr       error

	failCalls        int
	failCtx          context.Context
	failCtxErrAtCall error
	failJobID        uint64
	failErrorCode    string
	failErr          error
}

func (f *fakeRepository) Create(ctx context.Context, userID, conversationID uint64, content string) (*Job, error) {
	f.createCalls++
	f.createCtx = ctx
	f.createUserID = userID
	f.createConversationID = conversationID
	f.createContent = content
	return f.created, f.createErr
}

func (f *fakeRepository) GetByID(ctx context.Context, userID, jobID uint64) (*Job, error) {
	f.getCalls++
	f.getCtx = ctx
	f.getUserID = userID
	f.getJobID = jobID
	return f.got, f.getErr
}

func (f *fakeRepository) ClaimForProcessing(ctx context.Context, jobID uint64) (*Job, uint64, error) {
	f.claimCalls++
	f.claimCtx = ctx
	f.claimJobID = jobID
	return f.claimed, f.claimUser, f.claimErr
}

func (f *fakeRepository) Complete(ctx context.Context, jobID, assistantMessageID uint64) error {
	f.completeCalls++
	f.completeCtx = ctx
	f.completeJobID = jobID
	f.completeMessageID = assistantMessageID
	return f.completeErr
}

func (f *fakeRepository) Fail(ctx context.Context, jobID uint64, errorCode string) error {
	f.failCalls++
	f.failCtx = ctx
	f.failCtxErrAtCall = ctx.Err()
	f.failJobID = jobID
	f.failErrorCode = errorCode
	return f.failErr
}

type fakePublisher struct {
	calls int
	ctx   context.Context
	jobID uint64
	err   error
}

func (f *fakePublisher) PublishChatJob(ctx context.Context, jobID uint64) error {
	f.calls++
	f.ctx = ctx
	f.jobID = jobID
	return f.err
}

type fakeChatProcessor struct {
	calls          int
	ctx            context.Context
	userID         uint64
	conversationID uint64
	content        string
	result         *chat.Result
	err            error
}

func (f *fakeChatProcessor) ReceiveAndResponse(
	ctx context.Context,
	userID, conversationID uint64,
	content string,
) (*chat.Result, error) {
	f.calls++
	f.ctx = ctx
	f.userID = userID
	f.conversationID = conversationID
	f.content = content
	return f.result, f.err
}

func TestServiceCreate(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "request")
	created := &Job{ID: 41, ConversationID: 9, Content: "question", Status: StatusPending}
	repo := &fakeRepository{created: created}
	publisher := &fakePublisher{}
	service := NewService(repo, publisher, &fakeChatProcessor{})

	got, err := service.Create(ctx, 7, 9, "  question  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got != created {
		t.Fatalf("Create() job = %#v, want %#v", got, created)
	}
	if repo.createCalls != 1 || repo.createUserID != 7 || repo.createConversationID != 9 || repo.createContent != "question" {
		t.Fatalf("repository Create() = %d calls with user %d, conversation %d, content %q",
			repo.createCalls, repo.createUserID, repo.createConversationID, repo.createContent)
	}
	if repo.createCtx != ctx {
		t.Fatal("Create() did not pass its context to the repository")
	}
	if publisher.calls != 1 || publisher.jobID != created.ID || publisher.ctx != ctx {
		t.Fatalf("PublishChatJob() = %d calls with job ID %d", publisher.calls, publisher.jobID)
	}
}

func TestServiceCreateRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name           string
		userID         uint64
		conversationID uint64
		content        string
		wantErr        error
	}{
		{name: "invalid user", conversationID: 9, content: "question", wantErr: conversation.ErrInvalidUserID},
		{name: "invalid conversation", userID: 7, content: "question", wantErr: conversation.ErrInvalidConversationID},
		{name: "blank content", userID: 7, conversationID: 9, content: " \n\t ", wantErr: ErrInvalidContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			publisher := &fakePublisher{}
			service := NewService(repo, publisher, &fakeChatProcessor{})

			got, err := service.Create(context.Background(), tt.userID, tt.conversationID, tt.content)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("Create() job = %#v, want nil", got)
			}
			if repo.createCalls != 0 || publisher.calls != 0 {
				t.Fatalf("invalid Create() called repository %d times and publisher %d times", repo.createCalls, publisher.calls)
			}
		})
	}
}

func TestServiceCreateStopsAfterDependencyFailure(t *testing.T) {
	repoErr := errors.New("insert failed")
	publishErr := errors.New("publish failed")

	t.Run("repository", func(t *testing.T) {
		repo := &fakeRepository{createErr: repoErr}
		publisher := &fakePublisher{}
		service := NewService(repo, publisher, &fakeChatProcessor{})

		got, err := service.Create(context.Background(), 7, 9, "question")
		if !errors.Is(err, repoErr) || got != nil {
			t.Fatalf("Create() = (%#v, %v), want (nil, %v)", got, err, repoErr)
		}
		if publisher.calls != 0 {
			t.Fatalf("PublishChatJob() calls = %d, want 0", publisher.calls)
		}
	})

	t.Run("publisher", func(t *testing.T) {
		repo := &fakeRepository{created: &Job{ID: 41}}
		publisher := &fakePublisher{err: publishErr}
		service := NewService(repo, publisher, &fakeChatProcessor{})

		got, err := service.Create(context.Background(), 7, 9, "question")
		if !errors.Is(err, publishErr) || got != nil {
			t.Fatalf("Create() = (%#v, %v), want (nil, %v)", got, err, publishErr)
		}
	})
}

func TestServiceGetByID(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "request")
	want := &Job{ID: 41, ConversationID: 9, Status: StatusProcessing}
	repo := &fakeRepository{got: want}
	service := NewService(repo, &fakePublisher{}, &fakeChatProcessor{})

	got, err := service.GetByID(ctx, 7, 41)
	if err != nil || got != want {
		t.Fatalf("GetByID() = (%#v, %v), want (%#v, nil)", got, err, want)
	}
	if repo.getCalls != 1 || repo.getUserID != 7 || repo.getJobID != 41 || repo.getCtx != ctx {
		t.Fatalf("repository GetByID() = %d calls with user %d and job %d", repo.getCalls, repo.getUserID, repo.getJobID)
	}

	for _, tt := range []struct {
		name    string
		userID  uint64
		jobID   uint64
		wantErr error
	}{
		{name: "invalid user", jobID: 41, wantErr: conversation.ErrInvalidUserID},
		{name: "invalid job", userID: 7, wantErr: ErrInvalidJobID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo, &fakePublisher{}, &fakeChatProcessor{})
			if _, err := service.GetByID(context.Background(), tt.userID, tt.jobID); !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetByID() error = %v, want %v", err, tt.wantErr)
			}
			if repo.getCalls != 0 {
				t.Fatalf("repository GetByID() calls = %d, want 0", repo.getCalls)
			}
		})
	}
}

func TestServiceProcessCompletesJob(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "worker")
	repo := &fakeRepository{
		claimed:   &Job{ID: 41, ConversationID: 9, Content: "question", Status: StatusProcessing},
		claimUser: 7,
	}
	processor := &fakeChatProcessor{result: &chat.Result{ID: 52}}
	service := NewService(repo, &fakePublisher{}, processor)

	if err := service.Process(ctx, 41); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if repo.claimCalls != 1 || repo.claimJobID != 41 || repo.claimCtx != ctx {
		t.Fatalf("ClaimForProcessing() = %d calls with job ID %d", repo.claimCalls, repo.claimJobID)
	}
	if processor.calls != 1 || processor.ctx != ctx || processor.userID != 7 ||
		processor.conversationID != 9 || processor.content != "question" {
		t.Fatalf("ReceiveAndResponse() = %d calls with user %d, conversation %d, content %q",
			processor.calls, processor.userID, processor.conversationID, processor.content)
	}
	if repo.completeCalls != 1 || repo.completeJobID != 41 || repo.completeMessageID != 52 || repo.completeCtx != ctx {
		t.Fatalf("Complete() = %d calls with job %d and message %d", repo.completeCalls, repo.completeJobID, repo.completeMessageID)
	}
	if repo.failCalls != 0 {
		t.Fatalf("Fail() calls = %d, want 0", repo.failCalls)
	}
}

func TestServiceProcessStopsBeforeChat(t *testing.T) {
	tests := []struct {
		name      string
		jobID     uint64
		claimErr  error
		wantErr   error
		wantClaim int
	}{
		{name: "invalid job ID", wantErr: ErrInvalidJobID},
		{name: "duplicate delivery", jobID: 41, claimErr: ErrJobNotClaimable, wantClaim: 1},
		{name: "claim failure", jobID: 41, claimErr: errors.New("database unavailable"), wantClaim: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{claimErr: tt.claimErr}
			processor := &fakeChatProcessor{}
			service := NewService(repo, &fakePublisher{}, processor)

			err := service.Process(context.Background(), tt.jobID)
			switch tt.name {
			case "duplicate delivery":
				if err != nil {
					t.Fatalf("Process() duplicate error = %v, want nil", err)
				}
			case "claim failure":
				if !errors.Is(err, tt.claimErr) {
					t.Fatalf("Process() error = %v, want %v", err, tt.claimErr)
				}
			default:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Process() error = %v, want %v", err, tt.wantErr)
				}
			}
			if repo.claimCalls != tt.wantClaim || processor.calls != 0 || repo.completeCalls != 0 || repo.failCalls != 0 {
				t.Fatalf("calls after rejected Process(): claim=%d chat=%d complete=%d fail=%d",
					repo.claimCalls, processor.calls, repo.completeCalls, repo.failCalls)
			}
		})
	}
}

func TestServiceProcessRecordsChatFailure(t *testing.T) {
	tests := []struct {
		name          string
		chatErr       error
		wantErrorCode string
	}{
		{name: "chat failure", chatErr: errors.New("provider failed"), wantErrorCode: ErrorCodeChatFailed},
		{name: "timeout", chatErr: context.DeadlineExceeded, wantErrorCode: ErrorCodeChatTimedOut},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{claimed: &Job{ID: 41, ConversationID: 9, Content: "question"}, claimUser: 7}
			processor := &fakeChatProcessor{err: tt.chatErr}
			service := NewService(repo, &fakePublisher{}, processor)

			ctx, cancel := context.WithCancel(context.Background())
			if tt.name == "chat failure" {
				cancel()
			} else {
				defer cancel()
			}
			if err := service.Process(ctx, 41); err != nil {
				t.Fatalf("Process() error = %v, want nil after recording failure", err)
			}
			if repo.failCalls != 1 || repo.failJobID != 41 || repo.failErrorCode != tt.wantErrorCode {
				t.Fatalf("Fail() = %d calls with job %d and code %q", repo.failCalls, repo.failJobID, repo.failErrorCode)
			}
			if repo.failCtxErrAtCall != nil {
				t.Fatalf("Fail() cleanup context error = %v, want nil", repo.failCtxErrAtCall)
			}
			if repo.completeCalls != 0 {
				t.Fatalf("Complete() calls = %d, want 0", repo.completeCalls)
			}
		})
	}
}

func TestServiceProcessReturnsChatAndPersistenceFailures(t *testing.T) {
	chatErr := errors.New("provider failed")
	failErr := errors.New("record failure failed")
	repo := &fakeRepository{
		claimed:   &Job{ID: 41, ConversationID: 9, Content: "question"},
		claimUser: 7,
		failErr:   failErr,
	}
	service := NewService(repo, &fakePublisher{}, &fakeChatProcessor{err: chatErr})

	err := service.Process(context.Background(), 41)
	if !errors.Is(err, chatErr) || !errors.Is(err, failErr) {
		t.Fatalf("Process() error = %v, want both chat and persistence failures", err)
	}
}

func TestServiceProcessRejectsInvalidChatResult(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result *chat.Result
	}{
		{name: "nil result"},
		{name: "zero message ID", result: &chat.Result{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{claimed: &Job{ID: 41, ConversationID: 9, Content: "question"}, claimUser: 7}
			service := NewService(repo, &fakePublisher{}, &fakeChatProcessor{result: tt.result})

			if err := service.Process(context.Background(), 41); err != nil {
				t.Fatalf("Process() error = %v, want nil after recording invalid result", err)
			}
			if repo.failCalls != 1 || repo.failErrorCode != ErrorCodeInvalidChatResult || repo.failCtxErrAtCall != nil {
				t.Fatalf("Fail() = %d calls with code %q and context error %v",
					repo.failCalls, repo.failErrorCode, repo.failCtxErrAtCall)
			}
		})
	}
}

func TestServiceProcessReturnsInvalidResultAndPersistenceFailures(t *testing.T) {
	failErr := errors.New("record failure failed")
	repo := &fakeRepository{
		claimed:   &Job{ID: 41, ConversationID: 9, Content: "question"},
		claimUser: 7,
		failErr:   failErr,
	}
	service := NewService(repo, &fakePublisher{}, &fakeChatProcessor{})

	err := service.Process(context.Background(), 41)
	if !errors.Is(err, ErrInvalidChatResult) || !errors.Is(err, failErr) {
		t.Fatalf("Process() error = %v, want invalid result and persistence failures", err)
	}
}

func TestServiceProcessReturnsCompletionFailure(t *testing.T) {
	completeErr := errors.New("complete failed")
	repo := &fakeRepository{
		claimed:     &Job{ID: 41, ConversationID: 9, Content: "question"},
		claimUser:   7,
		completeErr: completeErr,
	}
	service := NewService(repo, &fakePublisher{}, &fakeChatProcessor{result: &chat.Result{ID: 52}})

	if err := service.Process(context.Background(), 41); !errors.Is(err, completeErr) {
		t.Fatalf("Process() error = %v, want %v", err, completeErr)
	}
	if repo.failCalls != 0 {
		t.Fatalf("Fail() calls = %d, want 0", repo.failCalls)
	}
}

func TestCleanupContextHasDeadline(t *testing.T) {
	repo := &fakeRepository{claimed: &Job{ID: 41, ConversationID: 9, Content: "question"}, claimUser: 7}
	service := NewService(repo, &fakePublisher{}, &fakeChatProcessor{err: context.DeadlineExceeded})

	if err := service.Process(context.Background(), 41); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	deadline, ok := repo.failCtx.Deadline()
	if !ok {
		t.Fatal("Fail() cleanup context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 5*time.Second {
		t.Fatalf("Fail() cleanup deadline remaining = %v, want within five seconds", remaining)
	}
}
