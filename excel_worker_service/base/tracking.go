// base/tracking.go：埋点表保留期清理（写入在 core，跨共享 MySQL 实例按 schema 限定删除）。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TrackingEventsTableCore is the core-owned tracking table on the shared MySQL
// instance. 埋点写入归 core（见 excel_core_service/base/tracking.go），worker 仅做
// 保留期清理，故以 schema 全限定名访问。
const TrackingEventsTableCore = "excel_core.excel_tracking_events"

// TrackingRetentionRepository deletes tracking rows past retention.
type TrackingRetentionRepository struct {
	db *sql.DB
}

// NewTrackingRetentionRepository creates the tracking retention gateway.
func NewTrackingRetentionRepository(db *sql.DB) *TrackingRetentionRepository {
	return &TrackingRetentionRepository{db: db}
}

// CleanupTracking deletes tracking rows whose server-received time precedes the
// retention cutoff（删除条件走服务端兜底的 received_at_ms，而非客户端时钟）。
func (repository *TrackingRetentionRepository) CleanupTracking(
	ctx context.Context,
	cutoff int64,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("tracking retention database is required")
	}
	if _, err := repository.db.ExecContext(ctx, `
		DELETE FROM excel_core.excel_tracking_events WHERE received_at_ms < ?
	`, cutoff); err != nil {
		return fmt.Errorf("cleanup expired tracking events: %w", err)
	}
	return nil
}