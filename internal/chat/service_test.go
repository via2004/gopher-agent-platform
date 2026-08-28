package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gopherai/internal/llm"
	"gopherai/internal/message"
	"gopherai/internal/modelcall"
	"gopherai/internal/rag"
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

func (f *fakeMessageService) GetByID(_ context.Context, _, conversationID, messageID uint64) (*message.Message, error) {
	if f.userMessage != nil && f.userMessage.ID == messageID && f.userMessage.ConversationID == conversationID {
		return f.userMessage, nil
	}
	for _, item := range f.recent {
		if item != nil && item.ID == messageID && item.ConversationID == conversationID {
			return item, nil
		}
	}
	return nil, message.ErrMessageNotFound
}

type noDocumentRetriever struct{}

func (noDocumentRetriever) Retrieve(context.Context, uint64, string, int) ([]rag.Chunk, error) {
	return nil, rag.ErrDocumentNotFound
}

type fakeRetriever struct {
	query  string
	topK   int
	chunks []rag.Chunk
	err    error
}

func (f *fakeRetriever) Retrieve(_ context.Context, _ uint64, query string, topK int) ([]rag.Chunk, error) {
	f.query = query
	f.topK = topK
	return f.chunks, f.err
}

func TestPrepareModelMessagesAugmentsOnlyRequestMessage(t *testing.T) {
	calls := make([]string, 0)
	history := []*message.Message{
		{ID: 1, ConversationID: 9, Role: message.RoleUser, Content: "earlier"},
		{ID: 2, ConversationID: 9, Role: message.RoleAssistant, Content: "answer"},
		{ID: 3, ConversationID: 9, Role: message.RoleUser, Content: "current question"},
	}
	messages := &fakeMessageService{calls: &calls, recent: history}
	retriever := &fakeRetriever{chunks: []rag.Chunk{{Content: "reference one"}, {Content: "reference two"}}}
	service := NewService(messages, nil, nil, nil, retriever)

	got, err := service.prepareModelMessages(context.Background(), 7, 3, history)
	if err != nil {
		t.Fatalf("prepareModelMessages() error = %v", err)
	}
	if got[0].Content != "earlier" || got[1].Content != "answer" {
		t.Fatalf("earlier messages changed: %#v", got)
	}
	for _, want := range []string{"reference one", "reference two", "current question"} {
		if !strings.Contains(got[2].Content, want) {
			t.Fatalf("RAG prompt = %q, want %q", got[2].Content, want)
		}
	}
	if retriever.query != "current question" || retriever.topK != defaultRAGTopK {
		t.Fatalf("Retrieve() query = %q, topK = %d", retriever.query, retriever.topK)
	}
}

func TestPrepareModelMessagesWithoutDocumentKeepsOriginalMessages(t *testing.T) {
	calls := make([]string, 0)
	history := []*message.Message{{ID: 3, ConversationID: 9, Role: message.RoleUser, Content: "question"}}
	messages := &fakeMessageService{calls: &calls, recent: history}
	service := NewService(messages, nil, nil, nil, noDocumentRetriever{})
	got, err := service.prepareModelMessages(context.Background(), 7, 3, history)
	if err != nil || len(got) != 1 || got[0].Content != "question" {
		t.Fatalf("prepareModelMessages() = %#v, %v", got, err)
	}
}

func TestReceiveAndResponseFinishesModelCallWhenRAGPreparationFails(t *testing.T) {
	calls := make([]string, 0)
	history := []*message.Message{{ID: 2, ConversationID: 9, Role: message.RoleUser, Content: "question"}}
	messages := &fakeMessageService{calls: &calls, recent: history}
	model := &fakeLLMClient{calls: &calls, result: &llm.Result{Content: "must not run"}}
	modelCalls := &fakeModelCallService{calls: &calls}
	transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
	wantErr := errors.New("embedding unavailable")
	service := NewService(messages, model, modelCalls, transactions, &fakeRetriever{err: wantErr})

	result, err := service.ReceiveAndResponse(context.Background(), 7, 9, "question")
	if !errors.Is(err, wantErr) || result != nil {
		t.Fatalf("ReceiveAndResponse() = %#v, %v", result, err)
	}
	if modelCalls.finished == nil || modelCalls.finished.ErrorCode == nil || *modelCalls.finished.ErrorCode != "prepare_model_message_failed" {
		t.Fatalf("finished model call = %#v", modelCalls.finished)
	}
	for _, call := range calls {
		if call == "llm" {
			t.Fatalf("model was called after RAG failure: %v", calls)
		}
	}
}

func TestBuildRAGContextSkipsBlankChunks(t *testing.T) {
	got := buildRAGContext([]rag.Chunk{{Content: "  facts  "}, {Content: "   "}}, " question ")
	for _, want := range []string{"[1]", "facts", "用户问题：\nquestion", "不要编造"} {
		if !strings.Contains(got, want) {
			t.Fatalf("buildRAGContext() = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "[2]") {
		t.Fatalf("blank chunk was included: %q", got)
	}
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
	if f.userErr == nil && f.userMessage == nil {
		for _, item := range f.recent {
			if item != nil && item.ConversationID == conversationID && item.Role == message.RoleUser && item.Content == content {
				f.userMessage = item
				break
			}
		}
		if f.userMessage == nil {
			f.userMessage = &message.Message{
				ID:             2,
				ConversationID: conversationID,
				Role:           message.RoleUser,
				Content:        content,
			}
		}
	}
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
	for _, item := range f.recent {
		if item != nil && item.ConversationID == 0 {
			item.ConversationID = conversationID
		}
	}
	if f.recentErr == nil && f.userMessage != nil {
		found := false
		for _, item := range f.recent {
			if item != nil && item.ID == f.userMessage.ID {
				found = true
				break
			}
		}
		if !found {
			f.recent = append(f.recent, f.userMessage)
		}
	}
	return f.recent, f.recentErr
}

type fakeLLMClient struct {
	calls    *[]string
	ctx      context.Context
	messages []llm.Message
	result   *llm.Result
	err      error
}

func (f *fakeLLMClient) Generate(ctx context.Context, messages []llm.Message) (*llm.Result, error) {
	*f.calls = append(*f.calls, "llm")
	f.ctx = ctx
	f.messages = messages
	return f.result, f.err
}

func (f *fakeLLMClient) GenerateStream(
	ctx context.Context,
	messages []llm.Message,
	onDelta func(string) error,
) (*llm.Result, error) {
	return nil, errors.New("unexpected streaming call")
}

func (f *fakeLLMClient) Info() llm.ModelInfo {
	return llm.ModelInfo{Provider: "test", Model: "gpt-test-requested"}
}

type fakeStreamingLLMClient struct {
	calls    *[]string
	ctx      context.Context
	messages []llm.Message
	deltas   []string
	result   *llm.Result
	err      error
}

func (f *fakeStreamingLLMClient) GenerateStream(
	ctx context.Context,
	messages []llm.Message,
	onDelta func(string) error,
) (*llm.Result, error) {
	*f.calls = append(*f.calls, "llm-stream")
	f.ctx = ctx
	f.messages = messages
	for _, delta := range f.deltas {
		if err := onDelta(delta); err != nil {
			return nil, err
		}
	}
	return f.result, f.err
}

func (f *fakeStreamingLLMClient) Generate(ctx context.Context, messages []llm.Message) (*llm.Result, error) {
	return nil, errors.New("unexpected non-streaming call")
}

func (f *fakeStreamingLLMClient) Info() llm.ModelInfo {
	return llm.ModelInfo{Provider: "test", Model: "gpt-test-requested"}
}

type fakeModelCallService struct {
	calls             *[]string
	startCtx          context.Context
	startUserID       uint64
	started           *modelcall.Model
	startErr          error
	completeCtx       context.Context
	completeUser      uint64
	completed         *modelcall.Model
	completeErr       error
	finishCtx         context.Context
	finishUser        uint64
	finished          *modelcall.Model
	finishErr         error
	finishCtxErr      error
	finishDeadline    time.Time
	finishHasDeadline bool
}

func (f *fakeModelCallService) Start(ctx context.Context, userID uint64, model *modelcall.Model) error {
	*f.calls = append(*f.calls, "model-start")
	f.startCtx = ctx
	f.startUserID = userID
	f.started = model
	if f.startErr == nil {
		model.ID = 101
		model.StartedAt = time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC)
		model.Status = modelcall.StatusRunning
	}
	return f.startErr
}

func (f *fakeModelCallService) Complete(ctx context.Context, userID uint64, model *modelcall.Model) error {
	*f.calls = append(*f.calls, "model-complete")
	f.completeCtx = ctx
	f.completeUser = userID
	f.completed = model
	if f.completeErr == nil {
		model.Status = modelcall.StatusCompleted
		finishedAt := time.Date(2026, time.August, 11, 10, 0, 1, 0, time.UTC)
		model.FinishedAt = &finishedAt
	}
	return f.completeErr
}

func (f *fakeModelCallService) Finish(ctx context.Context, userID uint64, model *modelcall.Model) error {
	*f.calls = append(*f.calls, "model-finish")
	f.finishCtx = ctx
	f.finishUser = userID
	f.finished = model
	f.finishCtxErr = ctx.Err()
	f.finishDeadline, f.finishHasDeadline = ctx.Deadline()
	if f.finishErr == nil {
		finishedAt := time.Date(2026, time.August, 11, 10, 0, 1, 0, time.UTC)
		model.FinishedAt = &finishedAt
	}
	return f.finishErr
}

func TestFinishFailedModelCallUsesCleanupContextAndJoinsErrors(t *testing.T) {
	calls := make([]string, 0, 1)
	finishErr := errors.New("finish failed")
	modelCalls := &fakeModelCallService{calls: &calls, finishErr: finishErr}
	service := &Service{modelCall: modelCalls}
	model := &modelcall.Model{Status: modelcall.StatusRunning}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.finishFailedModelCall(ctx, 7, model, context.Canceled, "llm")
	if !errors.Is(err, context.Canceled) || !errors.Is(err, finishErr) {
		t.Fatalf("finishFailedModelCall() error = %v", err)
	}
	if model.Status != modelcall.StatusCancelled || model.ErrorCode == nil || *model.ErrorCode != "llm_cancelled" {
		t.Fatalf("model = %#v", model)
	}
	if modelCalls.finishCtxErr != nil || !modelCalls.finishHasDeadline {
		t.Fatalf("cleanup context error = %v, has deadline = %v", modelCalls.finishCtxErr, modelCalls.finishHasDeadline)
	}
	remaining := time.Until(modelCalls.finishDeadline)
	if remaining <= 0 || remaining > modelCallFinishTimeout {
		t.Fatalf("cleanup deadline remaining = %s", remaining)
	}
}

func TestFinishFailedModelCallPreservesExistingTerminalFailure(t *testing.T) {
	calls := make([]string, 0, 1)
	modelCalls := &fakeModelCallService{calls: &calls}
	service := &Service{modelCall: modelCalls}
	errorCode := "assistant_message_timed_out"
	model := &modelcall.Model{Status: modelcall.StatusTimedOut, ErrorCode: &errorCode}

	if err := service.finishFailedModelCall(context.Background(), 7, model, errors.New("commit failed"), "model_call_complete"); err == nil {
		t.Fatal("finishFailedModelCall() error = nil")
	}
	if model.Status != modelcall.StatusTimedOut || model.ErrorCode == nil || *model.ErrorCode != errorCode {
		t.Fatalf("existing terminal failure was overwritten: %#v", model)
	}
}

type fakeUnitOfWork struct {
	messages   MessageService
	modelCalls ModelCallService
	calls      int
	contexts   []context.Context
	err        error
}

func (f *fakeUnitOfWork) WithinTx(ctx context.Context, fn func(MessageService, ModelCallService) error) error {
	f.calls++
	f.contexts = append(f.contexts, ctx)
	if f.err != nil {
		return f.err
	}
	return fn(f.messages, f.modelCalls)
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
	modelResult := &llm.Result{
		Content:      assistant.Content,
		Model:        "gpt-test-actual",
		InputTokens:  20,
		OutputTokens: 10,
		TotalTokens:  30,
	}
	model := &fakeLLMClient{calls: &calls, result: modelResult}
	modelCalls := &fakeModelCallService{calls: &calls}
	transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
	service := NewService(messages, model, modelCalls, transactions, noDocumentRetriever{})
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	got, err := service.ReceiveAndResponse(ctx, 7, 9, "What is an interface?")
	if err != nil {
		t.Fatalf("ReceiveAndResponse() error = %v", err)
	}
	if got.ID != assistant.ID || got.Role != assistant.Role || got.Content != assistant.Content ||
		!got.CreatedAt.Equal(assistant.CreatedAt) || got.Model != modelResult.Model ||
		got.InputTokens != modelResult.InputTokens || got.OutputTokens != modelResult.OutputTokens ||
		got.TotalTokens != modelResult.TotalTokens {
		t.Fatalf("ReceiveAndResponse() result = %#v", got)
	}
	if want := []string{"user", "model-start", "recent", "llm", "assistant", "model-complete"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	if transactions.calls != 2 || modelCalls.started == nil || modelCalls.completed == nil || modelCalls.finished != nil {
		t.Fatalf("transactions/model calls = tx:%d start:%#v complete:%#v finish:%#v", transactions.calls, modelCalls.started, modelCalls.completed, modelCalls.finished)
	}
	if messages.userID != 7 || messages.conversationID != 9 || messages.userContent != "What is an interface?" || messages.recentLimit != defaultMessageLimit {
		t.Fatalf("message service received user ID %d, conversation ID %d, content %q, limit %d", messages.userID, messages.conversationID, messages.userContent, messages.recentLimit)
	}
	if messages.createUserCtx != ctx || messages.assistantCtx != ctx || model.ctx != ctx {
		t.Fatal("ReceiveAndResponse() did not pass its context through the workflow")
	}
	if messages.assistantText != model.result.Content {
		t.Fatalf("assistant content = %q, want %q", messages.assistantText, model.result.Content)
	}
	if len(model.messages) != 2 {
		t.Fatalf("LLM messages = %#v, want 2 non-nil messages", model.messages)
	}
	if model.messages[0].Role != string(message.RoleAssistant) || model.messages[0].Content != "Earlier answer" ||
		model.messages[1].Role != string(message.RoleUser) || model.messages[1].Content != "What is an interface?" {
		t.Errorf("LLM messages = %#v", model.messages)
	}
}

func TestServiceRespondToMessageUsesExistingRequestMessage(t *testing.T) {
	calls := make([]string, 0, 5)
	assistant := &message.Message{
		ID:             3,
		ConversationID: 9,
		Role:           message.RoleAssistant,
		Content:        "An interface describes behavior.",
	}
	messages := &fakeMessageService{
		calls: &calls,
		recent: []*message.Message{
			{ID: 2, ConversationID: 9, Role: message.RoleUser, Content: "What is an interface?"},
		},
		assistant: assistant,
	}
	modelResult := &llm.Result{Content: assistant.Content, Model: "gpt-test-actual"}
	model := &fakeLLMClient{calls: &calls, result: modelResult}
	modelCalls := &fakeModelCallService{calls: &calls}
	transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
	service := NewService(messages, model, modelCalls, transactions, noDocumentRetriever{})

	got, err := service.RespondToMessage(context.Background(), 7, 9, 2)
	if err != nil {
		t.Fatalf("RespondToMessage() error = %v", err)
	}
	if got.ID != assistant.ID || got.Content != assistant.Content {
		t.Fatalf("RespondToMessage() result = %#v", got)
	}
	if want := []string{"model-start", "recent", "llm", "assistant", "model-complete"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	if messages.createUserCtx != nil {
		t.Fatal("RespondToMessage() created a duplicate user message")
	}
	if modelCalls.started == nil || modelCalls.started.RequestMessageID != 2 ||
		modelCalls.started.ConversationID != 9 || modelCalls.startUserID != 7 {
		t.Fatalf("started model call = %#v for user %d", modelCalls.started, modelCalls.startUserID)
	}
	if transactions.calls != 1 {
		t.Fatalf("transaction calls = %d, want 1 for assistant completion", transactions.calls)
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
		{name: "recent messages", recentErr: recentErr, wantErr: recentErr, wantCalls: []string{"user", "model-start", "recent", "model-finish"}},
		{name: "LLM", modelErr: modelErr, wantErr: modelErr, wantCalls: []string{"user", "model-start", "recent", "llm", "model-finish"}},
		{name: "assistant message", assistantErr: assistantErr, wantErr: assistantErr, wantCalls: []string{"user", "model-start", "recent", "llm", "assistant", "model-finish"}},
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
			model := &fakeLLMClient{calls: &calls, result: &llm.Result{Content: "answer"}, err: tt.modelErr}
			modelCalls := &fakeModelCallService{calls: &calls}
			transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
			service := NewService(messages, model, modelCalls, transactions, noDocumentRetriever{})

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

func TestServiceReceiveAndResponseFinishesWhenCompletionFails(t *testing.T) {
	completeErr := errors.New("complete model call")
	calls := make([]string, 0, 8)
	assistant := &message.Message{ID: 3, Role: message.RoleAssistant, Content: "answer"}
	messages := &fakeMessageService{
		calls:     &calls,
		recent:    []*message.Message{{ID: 1, Role: message.RoleUser, Content: "question"}},
		assistant: assistant,
	}
	model := &fakeLLMClient{
		calls:  &calls,
		result: &llm.Result{Content: assistant.Content, Model: "gpt-test"},
	}
	modelCalls := &fakeModelCallService{calls: &calls, completeErr: completeErr}
	transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
	service := NewService(messages, model, modelCalls, transactions, noDocumentRetriever{})

	got, err := service.ReceiveAndResponse(context.Background(), 7, 9, "question")
	if !errors.Is(err, completeErr) {
		t.Fatalf("ReceiveAndResponse() error = %v, want %v", err, completeErr)
	}
	if got != nil {
		t.Fatalf("ReceiveAndResponse() result = %#v, want nil", got)
	}
	if want := []string{"user", "model-start", "recent", "llm", "assistant", "model-complete", "model-finish"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	if modelCalls.finished == nil || modelCalls.finished.Status != modelcall.StatusFailed {
		t.Fatalf("finished model call = %#v, want failed status", modelCalls.finished)
	}
	if modelCalls.finished.ErrorCode == nil || *modelCalls.finished.ErrorCode != "model_call_complete_failed" {
		t.Fatalf("finished error code = %v, want model_call_complete_failed", modelCalls.finished.ErrorCode)
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
		calls:  &calls,
		deltas: []string{"An interface", " describes behavior."},
		result: &llm.Result{
			Content:      assistant.Content,
			Model:        "gpt-test-actual",
			InputTokens:  20,
			OutputTokens: 10,
			TotalTokens:  30,
		},
	}
	modelCalls := &fakeModelCallService{calls: &calls}
	transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
	service := NewService(messages, streaming, modelCalls, transactions, noDocumentRetriever{})
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
	if got.ID != assistant.ID || got.Role != assistant.Role || got.Content != assistant.Content ||
		!got.CreatedAt.Equal(assistant.CreatedAt) || got.Model != streaming.result.Model ||
		got.InputTokens != streaming.result.InputTokens || got.OutputTokens != streaming.result.OutputTokens ||
		got.TotalTokens != streaming.result.TotalTokens {
		t.Fatalf("ChatStreaming() result = %#v", got)
	}
	if want := []string{"user", "model-start", "recent", "llm-stream", "assistant", "model-complete"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	if transactions.calls != 2 || modelCalls.completed == nil || modelCalls.finished != nil {
		t.Fatalf("transactions/model calls = tx:%d complete:%#v finish:%#v", transactions.calls, modelCalls.completed, modelCalls.finished)
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
	if messages.assistantText != streaming.result.Content {
		t.Fatalf("assistant content = %q, want %q", messages.assistantText, streaming.result.Content)
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
		{name: "recent messages", recentErr: recentErr, wantErr: recentErr, wantCalls: []string{"user", "model-start", "recent", "model-finish"}},
		{name: "LLM stream", streamErr: streamErr, wantErr: streamErr, wantCalls: []string{"user", "model-start", "recent", "llm-stream", "model-finish"}},
		{name: "delta callback", onDelta: func(string) error { return callbackErr }, wantErr: callbackErr, wantCalls: []string{"user", "model-start", "recent", "llm-stream", "model-finish"}},
		{name: "assistant message", assistantErr: assistantErr, wantErr: assistantErr, wantCalls: []string{"user", "model-start", "recent", "llm-stream", "assistant", "model-finish"}},
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
				calls:  &calls,
				deltas: []string{"answer"},
				result: &llm.Result{Content: "answer"},
				err:    tt.streamErr,
			}
			modelCalls := &fakeModelCallService{calls: &calls}
			transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
			service := NewService(messages, streaming, modelCalls, transactions, noDocumentRetriever{})
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

func TestServiceChatStreamingMarksIncompleteResponse(t *testing.T) {
	calls := make([]string, 0, 8)
	messages := &fakeMessageService{
		calls:  &calls,
		recent: []*message.Message{{ID: 1, Role: message.RoleUser, Content: "question"}},
	}
	streaming := &fakeStreamingLLMClient{
		calls:  &calls,
		deltas: []string{"partial"},
		result: &llm.Result{Content: "partial"},
		err:    llm.ErrResponseNotCompleted,
	}
	modelCalls := &fakeModelCallService{calls: &calls}
	transactions := &fakeUnitOfWork{messages: messages, modelCalls: modelCalls}
	service := NewService(messages, streaming, modelCalls, transactions, noDocumentRetriever{})

	got, err := service.ChatStreaming(context.Background(), 7, 9, "question", func(string) error { return nil })
	if !errors.Is(err, llm.ErrResponseNotCompleted) {
		t.Fatalf("ChatStreaming() error = %v, want %v", err, llm.ErrResponseNotCompleted)
	}
	if got != nil {
		t.Fatalf("ChatStreaming() result = %#v, want nil", got)
	}
	if want := []string{"user", "model-start", "recent", "llm-stream", "model-finish"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	if modelCalls.finished == nil || modelCalls.finished.Status != modelcall.StatusIncomplete {
		t.Fatalf("finished model call = %#v, want incomplete status", modelCalls.finished)
	}
	if modelCalls.finished.ErrorCode == nil || *modelCalls.finished.ErrorCode != "llm_incomplete" {
		t.Fatalf("finished error code = %v, want llm_incomplete", modelCalls.finished.ErrorCode)
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
