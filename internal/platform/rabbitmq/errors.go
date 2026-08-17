package rabbitmq

import "errors"

var (
	ErrURLInvalid          = errors.New("url is invalid")
	ErrAMQPDialFailed      = errors.New("amqp dial failed")
	ErrJobIDInvalid        = errors.New("jobID is invalid")
	ErrMarshalJobIDFailed  = errors.New("marshal jobID failed")
	ErrPublishFailed       = errors.New("publish failed")
	ErrPublishNotConfirmed = errors.New("publish not confirmed")
	ErrPublishUnroutable   = errors.New("publish unroutable")
	ErrChannelConnection   = errors.New("channel connection error")
)
