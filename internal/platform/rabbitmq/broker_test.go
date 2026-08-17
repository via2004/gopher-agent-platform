package rabbitmq

import (
	"context"
	"errors"
	"testing"
)

func TestNewRabbitMQClientRejectsBlankURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "whitespace", url: " \t\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewRabbitMQClient(tt.url)

			if !errors.Is(err, ErrURLInvalid) {
				t.Fatalf("NewRabbitMQClient() error = %v; want %v", err, ErrURLInvalid)
			}
			if client != nil {
				t.Fatalf("NewRabbitMQClient() client = %#v; want nil", client)
			}
		})
	}
}

func TestPublishChatJobRejectsZeroID(t *testing.T) {
	client := &RabbitMQClient{}

	err := client.PublishChatJob(context.Background(), 0)

	if !errors.Is(err, ErrJobIDInvalid) {
		t.Fatalf("PublishChatJob() error = %v; want %v", err, ErrJobIDInvalid)
	}
}
