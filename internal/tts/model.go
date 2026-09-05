package tts

type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type Task struct {
	ID        string
	Status    Status
	AudioURL  *string
	ErrorCode *string
}

func (s Status) valid() bool {
	switch s {
	case StatusRunning, StatusSucceeded, StatusFailed:
		return true
	default:
		return false
	}
}
