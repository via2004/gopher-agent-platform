package tts

import (
	"context"
	"strings"
	"unicode/utf8"
)

const maxTextRunes = 100_000

type Service struct {
	provider Provider
}

// NewService creates a TTS service. A nil provider keeps the optional feature
// disabled and makes Create and Get return ErrNotConfigured.
func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Create(ctx context.Context, text string) (*Task, error) {
	if s == nil || s.provider == nil {
		return nil, ErrNotConfigured
	}
	if !utf8.ValidString(text) {
		return nil, ErrInvalidText
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrInvalidText
	}
	if utf8.RuneCountInString(text) > maxTextRunes {
		return nil, ErrTextTooLong
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	task, err := s.provider.Create(ctx, text)
	if err != nil {
		return nil, err
	}
	if !validTask(task) {
		return nil, ErrInvalidProviderResult
	}
	return task, nil
}

func (s *Service) Get(ctx context.Context, taskID string) (*Task, error) {
	if s == nil || s.provider == nil {
		return nil, ErrNotConfigured
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, ErrInvalidTaskID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	task, err := s.provider.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !validTask(task) || task.ID != taskID {
		return nil, ErrInvalidProviderResult
	}
	return task, nil
}

func validTask(task *Task) bool {
	if task == nil || task.ID == "" || task.ID != strings.TrimSpace(task.ID) || !task.Status.valid() {
		return false
	}

	audioURLPresent := task.AudioURL != nil && strings.TrimSpace(*task.AudioURL) != ""
	errorCodePresent := task.ErrorCode != nil && strings.TrimSpace(*task.ErrorCode) != ""

	/*
		AudioURL表示生成出来的音频的URL:
		1. Running: 正在生成, 音频没生成出来, 不该有AudioURL和ErrorCode
		2. Succeed: 生成成功, 应该有有效的URL, 以及ErrorCode == nil
		3. Failed:  生成失败, 不该有URL, 同时应该有errorCode
	*/
	switch task.Status {
	case StatusRunning:
		return task.AudioURL == nil && task.ErrorCode == nil
	case StatusSucceeded:
		return audioURLPresent && task.ErrorCode == nil
	case StatusFailed:
		return task.AudioURL == nil && errorCodePresent
	default:
		return false
	}
}
