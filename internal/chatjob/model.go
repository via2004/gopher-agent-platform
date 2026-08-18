package chatjob

import "time"

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

type Job struct {
	ID                 uint64
	ConversationID     uint64
	Content            string
	Status             Status
	AssistantMessageID *uint64
	ErrorCode          *string
	AttemptCount       int64
	CreatedAt          time.Time
	StartedAt          *time.Time
	FinishedAt         *time.Time
}
