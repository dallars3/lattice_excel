// records/base_rows_deleter.go：workspace 删除 Base 时的记录清理适配。
package records

import (
	"context"

	"excel_core_service/base"
)

// BaseRowsDeleter adapts the records persistence cleanup to the workspace
// Base-deletion seam.
type BaseRowsDeleter struct {
	repository *base.RecordsRepository
}

// NewBaseRowsDeleter creates the records cleanup adapter used by workspace.
func NewBaseRowsDeleter(repository *base.RecordsRepository) *BaseRowsDeleter {
	return &BaseRowsDeleter{repository: repository}
}

// DeleteBaseRows removes records, histories, calc rows and attributable oplog.
func (deleter *BaseRowsDeleter) DeleteBaseRows(
	ctx context.Context,
	baseID string,
	tableIDList []string,
) error {
	return deleter.repository.DeleteBaseRows(ctx, baseID, tableIDList)
}
