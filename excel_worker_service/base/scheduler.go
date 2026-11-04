// base/scheduler.go：对应 excel_scheduler 表的数据操作。
package base

import "time"

const (
	TaskTypeDeactivation          = "deactivation"
	TaskTypeEnterpriseDissolution = "enterprise_dissolution"
	TaskTypeRetentionCleanup      = "retention_cleanup"
	TaskTypeLinkTTL               = "link_ttl"
	TaskTypeOrphanCleanup         = "orphan_cleanup"
	TaskTypeUndoReclaim           = "undo_reclaim"
	TaskTypeDeadLetterRetry       = "dead_letter_retry"

	HistoryRetention      = 90 * 24 * time.Hour
	NotificationRetention = 90 * 24 * time.Hour
	AuditRetention        = 180 * 24 * time.Hour
	// TrackingRetention 与审计保留期一致（180 天），埋点数据保留期有界以约束存储成本。
	TrackingRetention = 180 * 24 * time.Hour
)
