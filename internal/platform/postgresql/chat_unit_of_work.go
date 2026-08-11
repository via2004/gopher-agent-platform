package platform

import (
	"context"
	"gopherai/internal/chat"
	"gopherai/internal/message"
	"gopherai/internal/modelcall"
)

type ChatUnitOfWork struct {
	transactions *TxManager
}

var _ chat.UnitOfWork = (*ChatUnitOfWork)(nil)

func (u *ChatUnitOfWork) WithinTx(
	ctx context.Context, fn func(messages chat.MessageService,
		modelCalls chat.ModelCallService) error) error {
	return u.transactions.WithinTx(ctx, func(db DBTX) error {
		messageRepository := NewMessageRepository(db)
		messageService := message.NewService(messageRepository)

		modelRepository := NewModelRepository(db)
		modelCallService := modelcall.NewService(modelRepository)

		return fn(messageService, modelCallService)
	})
}

func NewChatUnitOfWork(transactions *TxManager) *ChatUnitOfWork {
	return &ChatUnitOfWork{
		transactions: transactions,
	}
}
