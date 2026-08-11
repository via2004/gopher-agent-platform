package chat

import (
	"context"
	"errors"
	"gopherai/internal/llm"
	"gopherai/internal/message"
	"gopherai/internal/modelcall"
	"time"
)

const (
	defaultMessageLimit = 40
)

type Service struct {
	messages     MessageService
	model        llm.ModelClient
	modelCall    ModelCallService
	transactions UnitOfWork
}

func NewService(messages MessageService, model llm.ModelClient,
	modelCall ModelCallService, transactions UnitOfWork) *Service {
	return &Service{
		messages:     messages,
		model:        model,
		transactions: transactions,
		modelCall:    modelCall,
	}
}

type Result struct {
	ID           uint64
	Role         message.Role
	Content      string
	CreatedAt    time.Time
	Model        string
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

type generateFunc func(context.Context, []llm.Message) (*llm.Result, error)

func (s *Service) ReceiveAndResponse(ctx context.Context, userID uint64,
	conversationID uint64, content string) (*Result, error) {

	return s.receive(ctx, userID, conversationID, content, s.model.Generate)
}

func (s *Service) ChatStreaming(ctx context.Context, userID uint64,
	conversationID uint64, content string, onDelta func(string) error) (*Result, error) {
	generate := func(ctx context.Context, messages []llm.Message) (*llm.Result, error) {
		return s.model.GenerateStream(ctx, messages, onDelta)
	}

	return s.receive(ctx, userID, conversationID, content, generate)
}

func toLLMMessages(messages []*message.Message) []llm.Message {
	result := make([]llm.Message, 0, len(messages))
	for _, item := range messages {
		if item == nil {
			continue
		}

		result = append(result, llm.Message{
			Role:    string(item.Role),
			Content: item.Content,
		})
	}
	return result
}

func (s *Service) startModelCall(ctx context.Context, userID, conversationID uint64, content string, model *modelcall.Model) error {
	return s.transactions.WithinTx(ctx, func(
		messages MessageService,
		modelCalls ModelCallService,
	) error {
		message, err := messages.CreateUserMessage(ctx, userID, conversationID, content)
		if err != nil {
			return err
		}

		info := s.model.Info()

		model.RequestMessageID = message.ID
		model.RequestedModel = &info.Model
		model.Provider = info.Provider

		return modelCalls.Start(ctx, userID, model)
	})
}

func (s *Service) saveAssistantAndComplete(ctx context.Context, userID,
	conversationID uint64, modelResult *llm.Result, model *modelcall.Model) (*message.Message, error) {
	var assistant *message.Message
	err := s.transactions.WithinTx(ctx, func(
		messages MessageService,
		modelCalls ModelCallService,
	) error {
		created, err := messages.CreateAssistantMessage(ctx, userID,
			conversationID, modelResult.Content)
		if err != nil {
			markModelCallFailure(model, err, "assistant_message")
			return err
		}

		assistant = created

		model.AssistantMessageID = &assistant.ID
		model.ActualModel = &modelResult.Model
		model.InputTokens = &modelResult.InputTokens
		model.OutputTokens = &modelResult.OutputTokens
		model.TotalTokens = &modelResult.TotalTokens

		if err := modelCalls.Complete(ctx, userID, model); err != nil {
			markModelCallFailure(model, err, "model_call_complete")
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return assistant, nil
}

func toResult(assistant *message.Message, modelResult *llm.Result) *Result {
	return &Result{
		ID:           assistant.ID,
		Role:         assistant.Role,
		Content:      assistant.Content,
		CreatedAt:    assistant.CreatedAt,
		Model:        modelResult.Model,
		InputTokens:  modelResult.InputTokens,
		OutputTokens: modelResult.OutputTokens,
		TotalTokens:  modelResult.TotalTokens,
	}
}

func (s *Service) receive(ctx context.Context, userID uint64,
	conversationID uint64, content string,
	generate generateFunc) (*Result, error) {

	model := &modelcall.Model{
		ConversationID: conversationID,
	}

	err := s.startModelCall(ctx, userID, conversationID, content, model)
	if err != nil {
		return nil, err
	}

	messages, err := s.messages.ListRecent(ctx, userID, conversationID, defaultMessageLimit)
	if err != nil {
		markModelCallFailure(model, err, "history_load")
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.modelCall.Finish(cleanupCtx, userID, model))
		return nil, err
	}

	modelResult, err := generate(ctx, toLLMMessages(messages))
	if err != nil {
		markModelCallFailure(model, err, "llm")
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.modelCall.Finish(cleanupCtx, userID, model))
		return nil, err
	}

	assistant, err := s.saveAssistantAndComplete(ctx, userID, conversationID, modelResult, model)
	if err != nil {
		if !model.Status.IsFailureTerminal() {
			markModelCallFailure(model, err, "model_call_complete")
		}

		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.modelCall.Finish(cleanupCtx, userID, model))
		return nil, err
	}

	return toResult(assistant, modelResult), nil
}

func markModelCallFailure(model *modelcall.Model, err error, operation string) {
	statusSuffix := "failed"
	model.Status = modelcall.StatusFailed

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		model.Status = modelcall.StatusTimedOut
		statusSuffix = "timed_out"
	case errors.Is(err, context.Canceled):
		model.Status = modelcall.StatusCancelled
		statusSuffix = "cancelled"
	case errors.Is(err, llm.ErrResponseNotCompleted):
		model.Status = modelcall.StatusIncomplete
		statusSuffix = "incomplete"
	}

	errorCode := operation + "_" + statusSuffix
	model.ErrorCode = &errorCode
}
