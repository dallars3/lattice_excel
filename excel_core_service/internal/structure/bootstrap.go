package structure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"excel-libs/idgen"
	"excel_core_service/base"
)

// BootstrapDefaultStructure creates the Base's default table, three text
// fields and one table view in one structure transaction.
func BootstrapDefaultStructure(
	ctx context.Context,
	repository *base.StructureRepository,
	baseID string,
) (tableID string, err error) {
	if repository == nil {
		return "", errors.New("structure repository is required")
	}
	if baseID == "" {
		return "", errors.New("Base id is required")
	}
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var enterpriseID string
	if err := tx.QueryRowContext(ctx, `
		SELECT enterprise_id FROM excel_workspace_bases WHERE id = ?
	`, baseID).Scan(&enterpriseID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", base.ErrWorkspaceNotFound
		}
		return "", fmt.Errorf("query Base enterprise: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	tableID, err = idgen.New()
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO excel_structure_tables (
			id, enterprise_id, base_id, name, position, created_at
		) VALUES (?, ?, ?, '表格', 0, ?)
	`, tableID, enterpriseID, baseID, now); err != nil {
		return "", fmt.Errorf("insert default structure table: %w", err)
	}

	for position, name := range []string{"标题", "文本", "备注"} {
		fieldID, idErr := idgen.New()
		if idErr != nil {
			return "", idErr
		}
		config, encodeErr := base.EncodeStructureConfig(map[string]any{})
		if encodeErr != nil {
			return "", encodeErr
		}
		if err := insertBootstrapField(ctx, tx, base.StructureField{
			ID:        fieldID,
			TableID:   tableID,
			Name:      name,
			Type:      "FIELD_TYPE_TEXT",
			Config:    config,
			IsPrimary: position == 0,
			Position:  position,
			CreatedAt: now,
		}); err != nil {
			return "", err
		}
	}

	viewID, err := idgen.New()
	if err != nil {
		return "", err
	}
	viewConfig, err := base.EncodeStructureConfig(map[string]any{})
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO excel_structure_views (
			id, table_id, name, type, config, position, created_at
		) VALUES (?, ?, '表格', 'table', ?, 0, ?)
	`, viewID, tableID, viewConfig, now); err != nil {
		return "", fmt.Errorf("insert default structure view: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit default Base structure: %w", err)
	}
	return tableID, nil
}

func insertBootstrapField(
	ctx context.Context,
	tx *sql.Tx,
	field base.StructureField,
) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO excel_structure_fields (
			id, table_id, name, type, config, is_primary, position, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, field.ID, field.TableID, field.Name, field.Type, field.Config,
		field.IsPrimary, field.Position, field.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert default structure field: %w", err)
	}
	return nil
}

// DeleteAllForBase removes every structure row owned by one Base.
func DeleteAllForBase(
	ctx context.Context,
	repository *base.StructureRepository,
	baseID string,
) error {
	if repository == nil {
		return errors.New("structure repository is required")
	}
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		DELETE options
		FROM excel_structure_field_options AS options
		INNER JOIN excel_structure_fields AS fields ON fields.id = options.field_id
		INNER JOIN excel_structure_tables AS tables ON tables.id = fields.table_id
		WHERE tables.base_id = ?
	`, baseID); err != nil {
		return fmt.Errorf("delete Base structure options: %w", err)
	}
	for _, statement := range []string{
		`DELETE fields FROM excel_structure_fields AS fields
		 INNER JOIN excel_structure_tables AS tables ON tables.id = fields.table_id
		 WHERE tables.base_id = ?`,
		`DELETE views FROM excel_structure_views AS views
		 INNER JOIN excel_structure_tables AS tables ON tables.id = views.table_id
		 WHERE tables.base_id = ?`,
		`DELETE FROM excel_structure_tables WHERE base_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, baseID); err != nil {
			return fmt.Errorf("delete Base structure: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Base structure cleanup: %w", err)
	}
	return nil
}
