// base/tracking.go：埋点表保留期清理（写入在 core，跨共享 MySQL 实例按 schema 限定删除）。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TrackingRetentionRepository deletes tracking rows past retention. coreSchemaName
// 是 core 侧埋点表的跨库库名：生产默认 CoreSchemaName 常量，worker 测试经
// WithCoreSchemaName 指到 core 测试库（双轴评审第 5 项：包级可变改注入）。
type TrackingRetentionRepository struct {
	db             *sql.DB
	coreSchemaName string
}

// NewTrackingRetentionRepository creates the tracking retention gateway.
func NewTrackingRetentionRepository(db *sql.DB) *TrackingRetentionRepository {
	return &TrackingRetentionRepository{db: db, coreSchemaName: CoreSchemaName}
}

// WithCoreSchemaName overrides the cross-schema name of the core-owned tracking
// table. 测试注入缝（excel_core_test）；生产装配不调用，保持默认共享库名。
func (repository *TrackingRetentionRepository) WithCoreSchemaName(name string) *TrackingRetentionRepository {
	repository.coreSchemaName = name
	return repository
}

// coreTrackingTable is the schema-qualified name of the core-owned tracking
// table. 埋点写入归 core（见 excel_core_service/base/tracking.go），worker 仅做
// 保留期清理，故以 schema 全限定名访问。
func (repository *TrackingRetentionRepository) coreTrackingTable() string {
	return repository.coreSchemaName + ".excel_tracking_events"
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
	if _, err := repository.db.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s WHERE received_at_ms < ?
	`, repository.coreTrackingTable()), cutoff); err != nil {
		return fmt.Errorf("cleanup expired tracking events: %w", err)
	}
	return nil
}