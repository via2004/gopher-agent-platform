package tts

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeProvider struct {
	createCalls int
	createCtx   context.Context
	createdText string
	createTask  *Task
	createErr   error
	getCalls    int
	getCtx      context.Context
	gotTaskID   string
	getTask     *Task
	getErr      error
}

func (f *fakeProvider) Create(ctx context.Context, text string) (*Task, error) {
	f.createCalls++
	f.createCtx = ctx
	f.createdText = text
	return f.createTask, f.createErr
}

func (f *fakeProvider) Get(ctx context.Context, taskID string) (*Task, error) {
	f.getCalls++
	f.getCtx = ctx
	f.gotTaskID = taskID
	return f.getTask, f.getErr
}

func TestServiceCreate(t *testing.T) {
	want := &Task{ID: "task-1", Status: StatusRunning}
	provider := &fakeProvider{createTask: want}
	service := NewService(provider)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.Create(ctx, "  welcome to GopherAI  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got != want {
		t.Fatalf("Create() task = %#v, want %#v", got, want)
	}
	if provider.createCalls != 1 || provider.createdText != "welcome to GopherAI" {
		t.Fatalf("Provider.Create() = %d calls with text %q", provider.createCalls, provider.createdText)
	}
	if provider.createCtx != ctx {
		t.Fatal("Create() did not pass its context to the provider")
	}
}

func TestServiceCreateAcceptsMaximumUnicodeText(t *testing.T) {
	text := strings.Repeat("界", maxTextRunes)
	provider := &fakeProvider{createTask: &Task{ID: "task-1", Status: StatusRunning}}

	if _, err := NewService(provider).Create(context.Background(), text); err != nil {
		t.Fatalf("Create() at maximum text length error = %v", err)
	}
	if provider.createdText != text {
		t.Fatal("Create() changed valid text")
	}
}

func TestServiceCreateRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr error
	}{
		{name: "empty", wantErr: ErrInvalidText},
		{name: "whitespace", text: "  \n\t ", wantErr: ErrInvalidText},
		{name: "invalid UTF-8", text: string([]byte{0xff}), wantErr: ErrInvalidText},
		{name: "over limit", text: strings.Repeat("界", maxTextRunes+1), wantErr: ErrTextTooLong},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{}
			got, err := NewService(provider).Create(context.Background(), test.text)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, test.wantErr)
			}
			if got != nil || provider.createCalls != 0 {
				t.Fatalf("Create() task = %#v, provider calls = %d", got, provider.createCalls)
			}
		})
	}
}

func TestServiceGet(t *testing.T) {
	want := &Task{ID: "task-1", Status: StatusRunning}
	provider := &fakeProvider{getTask: want}
	service := NewService(provider)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.Get(ctx, "  task-1  ")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != want {
		t.Fatalf("Get() task = %#v, want %#v", got, want)
	}
	if provider.getCalls != 1 || provider.gotTaskID != "task-1" {
		t.Fatalf("Provider.Get() = %d calls with task ID %q", provider.getCalls, provider.gotTaskID)
	}
	if provider.getCtx != ctx {
		t.Fatal("Get() did not pass its context to the provider")
	}
}

func TestServiceRejectsUnavailableProvider(t *testing.T) {
	service := NewService(nil)
	if _, err := service.Create(context.Background(), "hello"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Create() error = %v, want %v", err, ErrNotConfigured)
	}
	if _, err := service.Get(context.Background(), "task-1"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Get() error = %v, want %v", err, ErrNotConfigured)
	}
}

func TestServiceGetRejectsInvalidTaskID(t *testing.T) {
	provider := &fakeProvider{}
	got, err := NewService(provider).Get(context.Background(), " \n ")
	if !errors.Is(err, ErrInvalidTaskID) {
		t.Fatalf("Get() error = %v, want %v", err, ErrInvalidTaskID)
	}
	if got != nil || provider.getCalls != 0 {
		t.Fatalf("Get() task = %#v, provider calls = %d", got, provider.getCalls)
	}
}

func TestServicePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := &fakeProvider{}
	if _, err := NewService(provider).Create(ctx, "hello"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context canceled", err)
	}
	if _, err := NewService(provider).Get(ctx, "task-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get() error = %v, want context canceled", err)
	}
	if provider.createCalls != 0 || provider.getCalls != 0 {
		t.Fatalf("provider calls = create %d, get %d", provider.createCalls, provider.getCalls)
	}
}

func TestServicePropagatesProviderErrors(t *testing.T) {
	wantErr := errors.New("provider unavailable")
	provider := &fakeProvider{createErr: wantErr, getErr: wantErr}
	service := NewService(provider)

	if task, err := service.Create(context.Background(), "hello"); task != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Create() = %#v, %v; want nil, %v", task, err, wantErr)
	}
	if task, err := service.Get(context.Background(), "task-1"); task != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Get() = %#v, %v; want nil, %v", task, err, wantErr)
	}
}

func TestServiceRejectsInvalidProviderResults(t *testing.T) {
	audioURL := "https://example.com/audio.mp3"
	empty := " "
	errorCode := "provider_task_failed"
	tests := []struct {
		name string
		task *Task
	}{
		{name: "nil"},
		{name: "empty ID", task: &Task{Status: StatusRunning}},
		{name: "ID with surrounding whitespace", task: &Task{ID: " task-1 ", Status: StatusRunning}},
		{name: "unknown status", task: &Task{ID: "task-1", Status: "unknown"}},
		{name: "running with audio", task: &Task{ID: "task-1", Status: StatusRunning, AudioURL: &audioURL}},
		{name: "running with error", task: &Task{ID: "task-1", Status: StatusRunning, ErrorCode: &errorCode}},
		{name: "succeeded without audio", task: &Task{ID: "task-1", Status: StatusSucceeded}},
		{name: "succeeded with empty audio", task: &Task{ID: "task-1", Status: StatusSucceeded, AudioURL: &empty}},
		{name: "succeeded with error", task: &Task{ID: "task-1", Status: StatusSucceeded, AudioURL: &audioURL, ErrorCode: &errorCode}},
		{name: "failed without error", task: &Task{ID: "task-1", Status: StatusFailed}},
		{name: "failed with empty error", task: &Task{ID: "task-1", Status: StatusFailed, ErrorCode: &empty}},
		{name: "failed with audio", task: &Task{ID: "task-1", Status: StatusFailed, AudioURL: &audioURL, ErrorCode: &errorCode}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{createTask: test.task}
			got, err := NewService(provider).Create(context.Background(), "hello")
			if !errors.Is(err, ErrInvalidProviderResult) {
				t.Fatalf("Create() error = %v, want %v", err, ErrInvalidProviderResult)
			}
			if got != nil {
				t.Fatalf("Create() task = %#v, want nil", got)
			}
		})
	}
}

func TestServiceAcceptsProviderTaskStates(t *testing.T) {
	audioURL := "https://example.com/audio.mp3"
	errorCode := "provider_task_failed"
	tasks := []*Task{
		{ID: "task-1", Status: StatusRunning},
		{ID: "task-1", Status: StatusSucceeded, AudioURL: &audioURL},
		{ID: "task-1", Status: StatusFailed, ErrorCode: &errorCode},
	}

	for _, task := range tasks {
		t.Run(string(task.Status), func(t *testing.T) {
			provider := &fakeProvider{getTask: task}
			got, err := NewService(provider).Get(context.Background(), "task-1")
			if err != nil || got != task {
				t.Fatalf("Get() = %#v, %v", got, err)
			}
		})
	}
}

func TestServiceGetRejectsMismatchedProviderTaskID(t *testing.T) {
	provider := &fakeProvider{getTask: &Task{ID: "task-2", Status: StatusRunning}}
	got, err := NewService(provider).Get(context.Background(), "task-1")
	if !errors.Is(err, ErrInvalidProviderResult) {
		t.Fatalf("Get() error = %v, want %v", err, ErrInvalidProviderResult)
	}
	if got != nil {
		t.Fatalf("Get() task = %#v, want nil", got)
	}
}
