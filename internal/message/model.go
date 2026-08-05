package message

import "time"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	ID             uint64
	ConversationID uint64
	Role           Role
	Content        string
	CreatedAt      time.Time
}
