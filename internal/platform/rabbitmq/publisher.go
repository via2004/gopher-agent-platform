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

// 发布Job到消息队列中
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

	// 消息找不到能接收它的队列时,通知发布方
	returned := channel.NotifyReturn(make(chan amqp.Return, 1))

	// RabbitMQ要对发布结果发送确认
	if err := channel.Confirm(false); err != nil {
		return fmt.Errorf("enable publisher confirm: %w", err)
	}

	// 发布到exchange, 由它路由到Queue
	// mandatory=true: 无法路由到任何队列时,要求退回消息
	// immediate=false: 不要求此刻必须有消费者立即接收
	// Persistent: 消息标记为持久化
	confirmation, err := channel.PublishWithDeferredConfirmWithContext(ctx, ExchangeName, QueueBindKey,
		true, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPublishFailed, err)
	}

	// 等待这条消息的发布确认
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait publisher confirm: %w", err)
	}
	if !acked {
		return ErrPublishNotConfirmed
	}

	select {
	// 为什么收到 Confirm 后还要检查 Return？
	// 无法路由的消息也可能收到肯定 Confirm。 对于这里的 mandatory 消息，RabbitMQ 会先发送 Return，再发送 Confirm.
	// 所以代码先等待 Confirm，这时候如果有return存在return也已经收到了,这里保底判定一下消息有没有被退回来
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
