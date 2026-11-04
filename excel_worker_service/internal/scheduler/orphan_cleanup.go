// scheduler/orphan_cleanup.go：一文件一接口/任务。细则见《excel_worker_service-技术方案》。
package scheduler

import (
	"context"
	"errors"
)

// OrphanRemover removes expired temporary and unreferenced file bytes.
type OrphanRemover interface {
	CleanupOrphans(context.Context) error
}

// OrphanCleanupHandler adapts orphan cleanup to scheduler.Handler.
type OrphanCleanupHandler struct {
	Remover OrphanRemover
}

// Handle executes one orphan-file maintenance pass.
func (handler OrphanCleanupHandler) Handle(ctx context.Context, _ []byte) error {
	if handler.Remover == nil {
		return errors.New("orphan remover is required")
	}
	return handler.Remover.CleanupOrphans(ctx)
}
