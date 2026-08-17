package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
)

type chatJobMessage struct {
	JobID uint64 `json:"job_id"`
}

func (c *RabbitMQClient) PublishChatJob(ctx context.Context, jobID uint64) error {
	if jobID == 0 {
		return ErrJobIDInvalid
	}

	message := &chatJobMessage{
		JobID: jobID,
	}

	body, err := json.Marshal(message)
	if err != nil {
		return ErrMarshalJobIDFailed
	}

	channel, err := c.connection.Channel()
	if err != nil {
		return fmt.Errorf("new connection channel: %w", err)
	}

	defer func() {
		_ = channel.Close()
	}()

	returned := channel.NotifyReturn(make(chan amqp.Return, 1))

	/*
		这里的 false 是 noWait=false：
		Go 请求开启 Confirm
		RabbitMQ 回复 confirm.select-ok
		然后才继续发布
	*/
	if err := channel.Confirm(false); err != nil {
		return fmt.Errorf("enable publisher confirm: %w", err)
	}

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(ctx, ExchangeName, QueueBindKey,
		true, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPublishFailed, err)
	}

	// Wait until RabbitMQ accepts responsibility for the publishing.
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait publisher confirm: %w", err)
	}
	if !acked {
		return ErrPublishNotConfirmed
	}

	select {
	/*
		ok == false
		表示 channel 已经关闭，里面也没有剩余数据。此时 Go 会返回 amqp.Return 的零值

		成功路由时根本不return,所以我们要先做confirm后做return
	*/
	case returnedMessage, ok := <-returned:
		if ok {
			return fmt.Errorf("%w: code=%d, reason:%s",
				ErrPublishUnroutable,
				returnedMessage.ReplyCode,
				returnedMessage.ReplyText,
			)
		}
		return nil
	default:
		return nil
	}
}
