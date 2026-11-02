package share

import (
	"context"
	"errors"
	"fmt"

	"excel_core_service/base"
)

// DeleteAllForBase removes every collaborator and link owned by one Base.
func DeleteAllForBase(
	ctx context.Context,
	repository *base.ShareRepository,
	baseID string,
) error {
	if repository == nil {
		return errors.New("share repository is required")
	}
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_share_collaborators WHERE base_id = ?
	`, baseID); err != nil {
		return fmt.Errorf("delete Base collaborators: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_share_links WHERE base_id = ?
	`, baseID); err != nil {
		return fmt.Errorf("delete Base share link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Base share cleanup: %w", err)
	}
	return nil
}
