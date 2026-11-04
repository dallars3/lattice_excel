// scheduler/retention_cleanup.go：一文件一接口/任务。细则见《excel_worker_service-技术方案》。
package scheduler

import (
	"context"
	"errors"

	"excel_worker_service/base"
)

// HistoryRetentionCleaner removes record history before a millisecond cutoff.
type HistoryRetentionCleaner interface {
	CleanupHistory(context.Context, int64) error
}

// NotificationRetentionCleaner removes notifications before a millisecond cutoff.
type NotificationRetentionCleaner interface {
	CleanupBefore(context.Context, int64) error
}

// AuditRetentionCleaner removes audit rows before a millisecond cutoff.
type AuditRetentionCleaner interface {
	CleanupAudit(context.Context, int64) error
}

// RetentionJob owns the 90/90/180 retention policy.
type RetentionJob struct {
	History       HistoryRetentionCleaner
	Notifications NotificationRetentionCleaner
	Audit         AuditRetentionCleaner
}

// Run applies all configured retention scopes.
func (job RetentionJob) Run(ctx context.Context, now int64) error {
	var cleanupErrors []error
	if job.History != nil {
		if err := job.History.CleanupHistory(
			ctx,
			now-base.HistoryRetention.Milliseconds(),
		); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if job.Notifications != nil {
		if err := job.Notifications.CleanupBefore(
			ctx,
			now-base.NotificationRetention.Milliseconds(),
		); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if job.Audit != nil {
		if err := job.Audit.CleanupAudit(
			ctx,
			now-base.AuditRetention.Milliseconds(),
		); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

// RetentionCleanupHandler adapts RetentionJob to scheduler.Handler.
type RetentionCleanupHandler struct {
	Job RetentionJob
	Now func() int64
}

// Handle executes the configured retention job.
func (handler RetentionCleanupHandler) Handle(ctx context.Context, _ []byte) error {
	now := handler.Now
	if now == nil {
		return errors.New("retention clock is required")
	}
	return handler.Job.Run(ctx, now())
}
