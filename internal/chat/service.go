package chat

import (
	"context"
	"gopherai/internal/llm"
	"gopherai/internal/message"
)

const (
	defaultMessageLimit = 40
)

type Service struct {
	messages MessageService
	model    llm.Client
}

func NewService(messages MessageService, model llm.Client) *Service {
	return &Service{
		messages: messages,
		model:    model,
	}
}

func (s *Service) ReceiveAndResponse(ctx context.Context, userID uint64,
	conversationID uint64, content string) (*message.Message, error) {
	_, err := s.messages.CreateUserMessage(ctx, userID, conversationID, content)
	if err != nil {
		return nil, err
	}

	messages, err := s.messages.ListRecent(ctx, userID, conversationID, defaultMessageLimit)
	if err != nil {
		return nil, err
	}

	responseContent, err := s.model.Generate(ctx, toLLMMessages(messages))
	if err != nil {
		return nil, err
	}

	responseMessage, err := s.messages.CreateAssistantMessage(ctx, userID, conversationID, responseContent)
	if err != nil {
		return nil, err
	}

	return responseMessage, nil
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
