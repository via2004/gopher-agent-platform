package modelcall

import (
	"context"
	"errors"
	"gopherai/internal/conversation"
	"strings"
)

var (
	ErrInvalidRequestMessageID   = errors.New("request message ID is invalid")
	ErrInvalidModelCallID        = errors.New("model call ID is invalid")
	ErrInvalidModelProvider      = errors.New("model provider is invalid")
	ErrInvalidAssistantMessageID = errors.New("assistant message is invalid")
	ErrInvalidModelCallStatus    = errors.New("model call status is invalid")
	ErrModelCallIsEmpty          = errors.New("model call is nil")
	ErrModelCallRepository       = errors.New("model call failed")
	ErrModelCallNotFound         = errors.New("model not found")
)

type Service struct {
	modelCalls Repository
}

func NewService(modelCalls Repository) *Service {
	return &Service{
		modelCalls: modelCalls,
	}
}

func (s *Service) Start(ctx context.Context, userID uint64, modelCall *Model) error {
	if userID == 0 {
		return conversation.ErrInvalidUserID
	}
	if modelCall == nil {
		return ErrModelCallIsEmpty
	}
	if modelCall.ConversationID == 0 {
		return conversation.ErrInvalidConversationID
	}
	if modelCall.RequestMessageID == 0 {
		return ErrInvalidRequestMessageID
	}
	modelCall.Provider = strings.TrimSpace(modelCall.Provider)
	if modelCall.Provider == "" {
		return ErrInvalidModelProvider
	}

	if err := s.modelCalls.Create(ctx, userID, modelCall); err != nil {
		return err
	}

	modelCall.Status = StatusRunning
	return nil
}

func (s *Service) Complete(ctx context.Context, userID uint64, modelCall *Model) error {
	if userID == 0 {
		return conversation.ErrInvalidUserID
	}
	if modelCall == nil {
		return ErrModelCallIsEmpty
	}
	if modelCall.ID == 0 {
		return ErrInvalidModelCallID
	}
	if modelCall.AssistantMessageID == nil || *modelCall.AssistantMessageID == 0 {
		return ErrInvalidAssistantMessageID
	}

	return s.modelCalls.CompleteModelCall(ctx, userID, modelCall)
}

func (s *Service) Finish(ctx context.Context, userID uint64, modelCall *Model) error {
	if userID == 0 {
		return conversation.ErrInvalidUserID
	}
	if modelCall == nil {
		return ErrModelCallIsEmpty
	}
	if modelCall.ID == 0 {
		return ErrInvalidModelCallID
	}

	if !modelCall.Status.IsFailureTerminal() {
		return ErrInvalidModelCallStatus
	}

	return s.modelCalls.FinishModelCall(ctx, userID, modelCall)
}

func (s *Service) Get(ctx context.Context, userID, modelCallID uint64) (*Model, error) {
	if userID == 0 {
		return nil, conversation.ErrInvalidUserID
	}

	if modelCallID == 0 {
		return nil, ErrInvalidModelCallID
	}

	return s.modelCalls.GetModelCall(ctx, modelCallID, userID)
}
