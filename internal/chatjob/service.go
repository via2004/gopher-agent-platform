package chatjob

import (
	"context"
	"errors"
	"strings"
	"time"

	"gopherai/internal/conversation"
)

const (
	ErrorCodeChatFailed        = "chat_failed"
	ErrorCodeInvalidChatResult = "invalid_chat_result"
	ErrorCodeChatTimedOut      = "chat_timed_out"
	ErrorCodeOverMaxRetryTime  = "over_max_retry_time"
	ErrorCodeCompleteFailed    = "complete_failed"
)

type Service struct {
	jobs        Repository
	client      Publisher
	service     ChatProcessor
	maxAttempts int64
}

func NewService(jobs Repository, client Publisher, service ChatProcessor, maxAttempts int64) *Service {
	return &Service{
		jobs:        jobs,
		client:      client,
		service:     service,
		maxAttempts: maxAttempts,
	}
}

func (s *Service) Create(ctx context.Context, userID, conversationID uint64, content string) (*Job, error) {
	if userID == 0 {
		return nil, conversation.ErrInvalidUserID
	}

	if conversationID == 0 {
		return nil, conversation.ErrInvalidConversationID
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrInvalidContent
	}

	job, err := s.jobs.Create(ctx, userID, conversationID, content)
	if err != nil {
		return nil, err
	}

	err = s.client.PublishChatJob(ctx, job.ID)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (s *Service) GetByID(ctx context.Context, userID, jobID uint64) (*Job, error) {
	if userID == 0 {
		return nil, conversation.ErrInvalidUserID
	}
	if jobID == 0 {
		return nil, ErrInvalidJobID
	}

	return s.jobs.GetByID(ctx, userID, jobID)
}

func (s *Service) Process(ctx context.Context, jobID uint64) error {
	if jobID == 0 {
		return ErrInvalidJobID
	}

	job, userID, err := s.jobs.ClaimForProcessing(ctx, jobID)
	// 重复消息到达时，Job已经不是pending了，Claim返回ErrJobNotClaimable。Process 返回错误后，Consumer 又执行 Nack(requeue=true)。
	if errors.Is(err, ErrJobNotClaimable) {
		return nil
	}
	if err != nil {
		return err
	}

	// 创建或服用requestMessageID
	requestMessageID, err := s.jobs.EnsureRequestMessage(ctx, userID, job.ID)
	if err != nil && job.AttemptCount < s.maxAttempts {
		return errors.Join(err, s.retry(ctx, job))
	} else if err != nil {
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			5*time.Second,
		)
		defer cancel()

		if failErr := s.jobs.Fail(cleanupCtx, jobID, ErrorCodeOverMaxRetryTime); failErr != nil {
			return errors.Join(err, failErr)
		}
		cancel()

		return nil
	}

	// 找到是否已经有完成了的有AI返回的消息记录
	assistantMessageID, ok, err := s.jobs.FindCompletedAssistantID(ctx, userID, jobID)
	if err != nil && job.AttemptCount < s.maxAttempts {
		return errors.Join(err, s.retry(ctx, job))
	} else if err != nil {
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			5*time.Second,
		)
		defer cancel()

		if failErr := s.jobs.Fail(cleanupCtx, jobID, ErrorCodeOverMaxRetryTime); failErr != nil {
			return errors.Join(err, failErr)
		}
		cancel()

		return nil
	}

	if ok {
		// 如果有，直接Complete
		if err := s.jobs.Complete(ctx, jobID, assistantMessageID); err != nil {
			return s.handleAttemptFailure(ctx, job, err)
		}
		return nil
	}

	// 没有，调模型跑这次的记录
	result, chatErr := s.service.RespondToMessage(ctx, userID, job.ConversationID, requestMessageID)
	if chatErr != nil {
		// 进这个逻辑我们后续由rabbitmq进行retry,不需要进后续的fail逻辑了
		if job.AttemptCount < s.maxAttempts {
			return errors.Join(chatErr, s.retry(ctx, job))
		}

		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			5*time.Second,
		)
		defer cancel()
		switch {
		case errors.Is(chatErr, context.DeadlineExceeded):
			if failErr := s.jobs.Fail(cleanupCtx, jobID, ErrorCodeChatTimedOut); failErr != nil {
				return errors.Join(chatErr, failErr)
			}
		default:
			if failErr := s.jobs.Fail(cleanupCtx, jobID, ErrorCodeChatFailed); failErr != nil {
				return errors.Join(chatErr, failErr)
			}
		}
		/*
			AI失败但成功记录failed,返回nil,连failed都没记录成功，才返回错误
			原因如下：
			RabbitMQ 只根据 Process 的返回值判断：
			Process 返回 nil   -> ACK，消息处理完了
			Process 返回 error -> Nack，消息重新入队
			例如 AI 调用失败：
			调用 AI 失败
				-> 数据库成功写入 status=failed
				-> 这个任务已经有明确结局
				-> Process 返回 nil
				-> RabbitMQ ACK，不再投递
			虽然 AI 失败了，但 Worker 已经成功处理了这个失败，所以对 RabbitMQ 来说是“处理完成”。
			如果你返回 AI 错误：
			数据库已经是 failed
				-> Process 返回 error
				-> RabbitMQ 重新投递
				-> 再次 Claim
				-> SQL 要求 status=pending
				-> 但现在是 failed
				-> Claim 返回 ErrJobNotClaimable
			如果这个错误继续交给 RabbitMQ，就可能不断重新入队。
		*/
		return nil
	}

	// 防御性编程，如果result为nil,下面访问result.ID直接panic
	if result == nil || result.ID == 0 {
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			5*time.Second,
		)
		defer cancel()
		failedErr := s.jobs.Fail(cleanupCtx, jobID, ErrorCodeInvalidChatResult)
		if failedErr != nil {
			return errors.Join(ErrInvalidChatResult, failedErr)
		}
		return nil
	}

	if err := s.jobs.Complete(ctx, jobID, result.ID); err != nil {
		return s.handleAttemptFailure(ctx, job, err)
	}
	return nil
}

func (s *Service) retry(ctx context.Context, job *Job) error {
	retryCtx, retryCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer retryCancel()
	if retryErr := s.jobs.Retry(retryCtx, job.ID); retryErr != nil {
		return retryErr
	}
	retryCancel()

	return nil
}

func (s *Service) handleAttemptFailure(ctx context.Context, job *Job, cause error) error {
	if job.AttemptCount < s.maxAttempts {
		if retryErr := s.retry(ctx, job); retryErr != nil {
			return errors.Join(cause, retryErr)
		}
		// 没到最大重试次数，返回错误让RabbitMQ NACK，从而再次重试
		return cause // 返回错误，让 RabbitMQ Nack
	}

	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		5*time.Second,
	)
	defer cancel()

	if failErr := s.jobs.Fail(cleanupCtx, job.ID, ErrorCodeCompleteFailed); failErr != nil {
		return errors.Join(cause, failErr)
	}

	return nil
}
