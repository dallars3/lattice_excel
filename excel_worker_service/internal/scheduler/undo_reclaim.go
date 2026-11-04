// scheduler/undo_reclaim.go：一文件一接口/任务。细则见《excel_worker_service-技术方案》。
package scheduler

import (
	"context"
	"errors"
)

// UndoReclaimer reclaims expired seven-second undo reservations.
type UndoReclaimer interface {
	ReclaimExpiredUndo(context.Context) error
}

// UndoReclaimHandler adapts undo reclamation to scheduler.Handler.
type UndoReclaimHandler struct {
	Reclaimer UndoReclaimer
}

// Handle executes one undo-reservation maintenance pass.
func (handler UndoReclaimHandler) Handle(ctx context.Context, _ []byte) error {
	if handler.Reclaimer == nil {
		return errors.New("undo reclaimer is required")
	}
	return handler.Reclaimer.ReclaimExpiredUndo(ctx)
}
