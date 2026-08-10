package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"gopherai/internal/llm"
	"gopherai/internal/message"
)

type fakeMessageService struct {
	calls          *[]string
	createUserCtx  context.Context
	userID         uint64
	conversationID uint64
	userContent    string
	userMessage    *message.Message
	userErr        error
	recentLimit    int
	recentCtx      context.Context
	recent         []*message.Message
	recentErr      error
	assistantCtx   context.Context
	assistantText  string
	assistant      *message.Message
	assistantErr   error
}

func (f *fakeMessageService) CreateUserMessage(
	ctx context.Context,
	userID, conversationID uint64,
	content string,
) (*message.Message, error) {
	*f.calls = append(*f.calls, "user")
	f.createUserCtx = ctx
	f.userID = userID
	f.conversationID = conversationID
	f.userContent = content
	return f.userMessage, f.userErr
}

func (f *fakeMessageService) CreateAssistantMessage(
	ctx context.Context,
	userID, conversationID uint64,
	content string,
) (*message.Message, error) {
	*f.calls = append(*f.calls, "assistant")
	f.assistantCtx = ctx
	f.userID = userID
	f.conversationID = conversationID
	f.assistantText = content
	return f.assistant, f.assistantErr
}

func (f *fakeMessageService) ListRecent(
	ctx context.Context,
	userID, conversationID uint64,
	limit int,
) ([]*message.Message, error) {
	*f.calls = append(*f.calls, "recent")
	f.recentCtx = ctx
	f.userID = userID
	f.conversationID = conversationID
	f.recentLimit = limit
	return f.recent, f.recentErr
}

type fakeLLMClient struct {
	calls    *[]string
	ctx      context.Context
	messages []llm.Message
	response string
	err      error
}

func (f *fakeLLMClient) Generate(ctx context.Context, messages []llm.Message) (string, error) {
	*f.calls = append(*f.calls, "llm")
	f.ctx = ctx
	f.messages = messages
	return f.response, f.err
}

type fakeStreamingLLMClient struct {
	calls    *[]string
	ctx      context.Context
	messages []llm.Message
	deltas   []string
	response string
	err      error
}

func (f *fakeStreamingLLMClient) GenerateStream(
	ctx context.Context,
	messages []llm.Message,
	onDelta func(string) error,
) (string, error) {
	*f.calls = append(*f.calls, "llm-stream")
	f.ctx = ctx
	f.messages = messages
	for _, delta := range f.deltas {
		if err := onDelta(delta); err != nil {
			return "", err
		}
	}
	return f.response, f.err
}

func TestServiceReceiveAndResponse(t *testing.T) {
	calls := make([]string, 0, 4)
	createdAt := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	assistant := &message.Message{
		ID:             3,
		ConversationID: 9,
		Role:           message.RoleAssistant,
		Content:        "An interface describes behavior.",
		CreatedAt:      createdAt,
	}
	messages := &fakeMessageService{
		calls: &calls,
		recent: []*message.Message{
			{ID: 1, ConversationID: 9, Role: message.RoleAssistant, Content: "Earlier answer"},
			nil,
			{ID: 2, ConversationID: 9, Role: message.RoleUser, Content: "What is an interface?"},
		},
		assistant: assistant,
	}
	model := &fakeLLMClient{calls: &calls, response: assistant.Content}
	service := NewService(messages, model, nil)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.ReceiveAndResponse(ctx, 7, 9, "What is an interface?")
	if err != nil {
		t.Fatalf("ReceiveAndResponse() error = %v", err)
	}
	if got != assistant {
		t.Fatalf("ReceiveAndResponse() message = %#v, want %#v", got, assistant)
	}
	if want := []string{"user", "recent", "llm", "assistant"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	if messages.userID != 7 || messages.conversationID != 9 || messages.userContent != "What is an interface?" || messages.recentLimit != defaultMessageLimit {
		t.Fatalf("message service received user ID %d, conversation ID %d, content %q, limit %d", messages.userID, messages.conversationID, messages.userContent, messages.recentLimit)
	}
	if messages.createUserCtx != ctx || messages.assistantCtx != ctx || model.ctx != ctx {
		t.Fatal("ReceiveAndResponse() did not pass its context through the workflow")
	}
	if messages.assistantText != model.response {
		t.Fatalf("assistant content = %q, want %q", messages.assistantText, model.response)
	}
	if len(model.messages) != 2 {
		t.Fatalf("LLM messages = %#v, want 2 non-nil messages", model.messages)
	}
	if model.messages[0].Role != string(message.RoleAssistant) || model.messages[0].Content != "Earlier answer" ||
		model.messages[1].Role != string(message.RoleUser) || model.messages[1].Content != "What is an interface?" {
		t.Errorf("LLM messages = %#v", model.messages)
	}
}

func TestServiceReceiveAndResponseStopsAfterFailure(t *testing.T) {
	userErr := errors.New("create user message")
	recentErr := errors.New("list recent messages")
	modelErr := errors.New("generate response")
	assistantErr := errors.New("create assistant message")
	tests := []struct {
		name         string
		userErr      error
		recentErr    error
		modelErr     error
		assistantErr error
		wantErr      error
		wantCalls    []string
	}{
		{name: "user message", userErr: userErr, wantErr: userErr, wantCalls: []string{"user"}},
		{name: "recent messages", recentErr: recentErr, wantErr: recentErr, wantCalls: []string{"user", "recent"}},
		{name: "LLM", modelErr: modelErr, wantErr: modelErr, wantCalls: []string{"user", "recent", "llm"}},
		{name: "assistant message", assistantErr: assistantErr, wantErr: assistantErr, wantCalls: []string{"user", "recent", "llm", "assistant"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := make([]string, 0, 4)
			messages := &fakeMessageService{
				calls:        &calls,
				userErr:      tt.userErr,
				recentErr:    tt.recentErr,
				assistantErr: tt.assistantErr,
				assistant:    &message.Message{ID: 3},
			}
			model := &fakeLLMClient{calls: &calls, response: "answer", err: tt.modelErr}
			service := NewService(messages, model, nil)

			got, err := service.ReceiveAndResponse(context.Background(), 7, 9, "question")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReceiveAndResponse() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("ReceiveAndResponse() message = %#v, want nil", got)
			}
			if !equalStrings(calls, tt.wantCalls) {
				t.Fatalf("call order = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestServiceChatStreaming(t *testing.T) {
	calls := make([]string, 0, 4)
	assistant := &message.Message{
		ID:             3,
		ConversationID: 9,
		Role:           message.RoleAssistant,
		Content:        "An interface describes behavior.",
	}
	messages := &fakeMessageService{
		calls: &calls,
		recent: []*message.Message{
			{ID: 1, ConversationID: 9, Role: message.RoleUser, Content: "What is an interface?"},
		},
		assistant: assistant,
	}
	streaming := &fakeStreamingLLMClient{
		calls:    &calls,
		deltas:   []string{"An interface", " describes behavior."},
		response: assistant.Content,
	}
	service := NewService(messages, nil, streaming)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	var gotDeltas []string

	got, err := service.ChatStreaming(ctx, 7, 9, "What is an interface?", func(delta string) error {
		gotDeltas = append(gotDeltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStreaming() error = %v", err)
	}
	if got != assistant {
		t.Fatalf("ChatStreaming() message = %#v, want %#v", got, assistant)
	}
	if want := []string{"user", "recent", "llm-stream", "assistant"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	if !equalStrings(gotDeltas, streaming.deltas) {
		t.Fatalf("deltas = %v, want %v", gotDeltas, streaming.deltas)
	}
	if messages.userID != 7 || messages.conversationID != 9 || messages.userContent != "What is an interface?" || messages.recentLimit != defaultMessageLimit {
		t.Fatalf("message service received user ID %d, conversation ID %d, content %q, limit %d", messages.userID, messages.conversationID, messages.userContent, messages.recentLimit)
	}
	if messages.createUserCtx != ctx || messages.recentCtx != ctx || streaming.ctx != ctx || messages.assistantCtx != ctx {
		t.Fatal("ChatStreaming() did not pass its context through the workflow")
	}
	if messages.assistantText != streaming.response {
		t.Fatalf("assistant content = %q, want %q", messages.assistantText, streaming.response)
	}
	if len(streaming.messages) != 1 || streaming.messages[0].Role != string(message.RoleUser) || streaming.messages[0].Content != "What is an interface?" {
		t.Fatalf("LLM messages = %#v", streaming.messages)
	}
}

func TestServiceChatStreamingStopsAfterFailure(t *testing.T) {
	userErr := errors.New("create user message")
	recentErr := errors.New("list recent messages")
	streamErr := errors.New("stream response")
	callbackErr := errors.New("send delta")
	assistantErr := errors.New("create assistant message")
	tests := []struct {
		name         string
		userErr      error
		recentErr    error
		streamErr    error
		assistantErr error
		onDelta      func(string) error
		wantErr      error
		wantCalls    []string
	}{
		{name: "user message", userErr: userErr, wantErr: userErr, wantCalls: []string{"user"}},
		{name: "recent messages", recentErr: recentErr, wantErr: recentErr, wantCalls: []string{"user", "recent"}},
		{name: "LLM stream", streamErr: streamErr, wantErr: streamErr, wantCalls: []string{"user", "recent", "llm-stream"}},
		{name: "delta callback", onDelta: func(string) error { return callbackErr }, wantErr: callbackErr, wantCalls: []string{"user", "recent", "llm-stream"}},
		{name: "assistant message", assistantErr: assistantErr, wantErr: assistantErr, wantCalls: []string{"user", "recent", "llm-stream", "assistant"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := make([]string, 0, 4)
			messages := &fakeMessageService{
				calls:        &calls,
				userErr:      tt.userErr,
				recentErr:    tt.recentErr,
				assistantErr: tt.assistantErr,
				assistant:    &message.Message{ID: 3},
			}
			streaming := &fakeStreamingLLMClient{
				calls:    &calls,
				deltas:   []string{"answer"},
				response: "answer",
				err:      tt.streamErr,
			}
			service := NewService(messages, nil, streaming)
			onDelta := tt.onDelta
			if onDelta == nil {
				onDelta = func(string) error { return nil }
			}

			got, err := service.ChatStreaming(context.Background(), 7, 9, "question", onDelta)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ChatStreaming() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("ChatStreaming() message = %#v, want nil", got)
			}
			if !equalStrings(calls, tt.wantCalls) {
				t.Fatalf("call order = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestUnavailableClientReturnsNotConfigured(t *testing.T) {
	_, err := (llm.UnavailableClient{}).Generate(context.Background(), []llm.Message{{Role: "user", Content: "hello"}})
	if !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("Generate() error = %v, want %v", err, llm.ErrNotConfigured)
	}
	_, err = (llm.UnavailableClient{}).GenerateStream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, func(string) error { return nil })
	if !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("GenerateStream() error = %v, want %v", err, llm.ErrNotConfigured)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
