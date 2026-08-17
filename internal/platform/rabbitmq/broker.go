package rabbitmq

import (
	"fmt"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeName = "gopherai.jobs"
	ExchangeType = "direct"

	QueueName = "gopherai.chat.jobs"

	QueueBindKey = "chat.generate"
)

type RabbitMQClient struct {
	connection *amqp.Connection
}

func NewRabbitMQClient(url string) (client *RabbitMQClient, errs error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, ErrURLInvalid
	}

	connection, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAMQPDialFailed, err)
	}

	defer func() {
		// that means connection operation failed, we should close the connection
		if errs != nil {
			_ = connection.Close()
		}
	}()

	channel, err := connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("new connection channel: %w", err)
	}

	defer func() {
		_ = channel.Close()
	}()

	if err := channel.ExchangeDeclare(ExchangeName, ExchangeType, true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("exchange declare: %w", err)
	}

	_, err = channel.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("queue declare: %w", err)
	}

	if err := channel.QueueBind(QueueName, QueueBindKey, ExchangeName, false, nil); err != nil {
		return nil, fmt.Errorf("queue bind: %w", err)
	}

	return &RabbitMQClient{
		connection: connection,
	}, nil
}

func (c *RabbitMQClient) Close() error {
	return c.connection.Close()
}
