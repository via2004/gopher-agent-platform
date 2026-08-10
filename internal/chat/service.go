package chat

import (
	"context"
	"gopherai/internal/llm"
	"gopherai/internal/message"
	"time"
)

const (
	defaultMessageLimit = 40
)

type Service struct {
	messages  MessageService
	model     llm.Client
	streaming llm.StreamingClient
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

func NewService(messages MessageService, model llm.Client, streaming llm.StreamingClient) *Service {
	return &Service{
		messages:  messages,
		model:     model,
		streaming: streaming,
	}
}

func (s *Service) ReceiveAndResponse(ctx context.Context, userID uint64,
	conversationID uint64, content string) (*Result, error) {
	_, err := s.messages.CreateUserMessage(ctx, userID, conversationID, content)
	if err != nil {
		return nil, err
	}

	messages, err := s.messages.ListRecent(ctx, userID, conversationID, defaultMessageLimit)
	if err != nil {
		return nil, err
	}

	modelResult, err := s.model.Generate(ctx, toLLMMessages(messages))
	if err != nil {
		return nil, err
	}

	assistant, err := s.messages.CreateAssistantMessage(ctx, userID, conversationID, modelResult.Content)
	if err != nil {
		return nil, err
	}

	return &Result{
		ID:           assistant.ID,
		Role:         assistant.Role,
		Content:      assistant.Content,
		CreatedAt:    assistant.CreatedAt,
		Model:        modelResult.Model,
		InputTokens:  modelResult.InputTokens,
		OutputTokens: modelResult.OutputTokens,
		TotalTokens:  modelResult.TotalTokens,
	}, nil
}

func (s *Service) ChatStreaming(ctx context.Context, userID uint64,
	conversationID uint64, content string, onDelta func(string) error) (*Result, error) {
	_, err := s.messages.CreateUserMessage(ctx, userID, conversationID, content)
	if err != nil {
		return nil, err
	}

	messages, err := s.messages.ListRecent(ctx, userID, conversationID, defaultMessageLimit)
	if err != nil {
		return nil, err
	}
	modelResult, err := s.streaming.GenerateStream(ctx, toLLMMessages(messages), onDelta)
	if err != nil {
		return nil, err
	}

	assistant, err := s.messages.CreateAssistantMessage(ctx, userID, conversationID, modelResult.Content)
	if err != nil {
		return nil, err
	}

	return &Result{
		ID:           assistant.ID,
		Role:         assistant.Role,
		Content:      assistant.Content,
		CreatedAt:    assistant.CreatedAt,
		Model:        modelResult.Model,
		InputTokens:  modelResult.InputTokens,
		OutputTokens: modelResult.OutputTokens,
		TotalTokens:  modelResult.TotalTokens,
	}, nil
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
