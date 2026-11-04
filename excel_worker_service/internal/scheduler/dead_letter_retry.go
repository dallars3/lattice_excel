// scheduler/dead_letter_retry.go：一文件一接口/任务。细则见《excel_worker_service-技术方案》。
package scheduler

import (
	"context"
	"errors"
)

// DeadLetterRetrier performs the configured dead-letter replay/alert action.
type DeadLetterRetrier interface {
	RetryDeadLetters(context.Context) error
}

// DeadLetterRetryHandler adapts a dead-letter retrier to scheduler.Handler.
type DeadLetterRetryHandler struct {
	Retrier DeadLetterRetrier
}

// Handle executes one dead-letter maintenance pass.
func (handler DeadLetterRetryHandler) Handle(ctx context.Context, _ []byte) error {
	if handler.Retrier == nil {
		return errors.New("dead-letter retrier is required")
	}
	return handler.Retrier.RetryDeadLetters(ctx)
}
