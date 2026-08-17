package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type ChatJobHandler func(ctx context.Context, jobID uint64) error

func (c *RabbitMQClient) ConsumeChatJobs(
	ctx context.Context,
	handler ChatJobHandler,
) error {
	channel, err := c.connection.Channel()
	if err != nil {
		return fmt.Errorf("new connection channel: %w", err)
	}

	defer func() {
		_ = channel.Close()
	}()

	closed := channel.NotifyClose(make(chan *amqp.Error, 1))

	// set prefetch = 1
	if err := channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("channel set Qos: %w", err)
	}

	// 这里不设置consumer name,让RabbitMQ 生成consumer tag
	deliveries, err := channel.ConsumeWithContext(ctx, QueueName, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("new delivery: %w", err)
	}

	for d := range deliveries {
		body := d.Body
		message := &chatJobMessage{}

		// TODO: 这里下面的错误处理似乎可以打印日志，但我目前没加，只忽略了Reject、Nack、Ack的错误
		if err := json.Unmarshal(body, message); err != nil || message.JobID == 0 {
			_ = d.Reject(false)
			continue
		}
		if err := handler(ctx, message.JobID); err != nil {
			_ = d.Nack(false, true) // 重新入队，稍后重试,第一个参数表示只确认这一条，不批量确认, NOT MULTIPLE CHECK
		} else {
			_ = d.Ack(false)
		}
	}

	if ctx.Err() != nil {
		return nil // 上层主动取消，正常退出
	}

	select {
	case amqpErr, ok := <-closed:
		// 非正常关闭下的错误，返回连接异常
		if ok {
			return fmt.Errorf("%w: %w", ErrChannelConnection, amqpErr)
		}
	default:
	}
	/*
		deliveries channel 已关闭
		ctx 又没有取消
		也就是说 Consumer 停止了，但不是上层要求停止的，应当视为异常。
		此时 closed 中不一定能立刻读到具体错误，例如存在事件到达时序差异。
		即使 default 被选中，我们也不能返回 nil，否则 Worker 会悄悄停止消费。
	*/

	return ErrChannelConnection
}
