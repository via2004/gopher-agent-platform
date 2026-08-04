package conversation

import "time"

// Conversation represents a user's AI conversation.
type Conversation struct {
	ID        uint64
	UserID    uint64
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}
