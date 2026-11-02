// base/structure.go：excel_structure 模块表的数据访问。
package base

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	StructureTablesTable       = "excel_structure_tables"
	StructureFieldsTable       = "excel_structure_fields"
	StructureViewsTable        = "excel_structure_views"
	StructureFieldOptionsTable = "excel_structure_field_options"
)

// ErrStructureNotFound identifies a missing table, field, or view.
var ErrStructureNotFound = errors.New("structure resource not found")

// StructureTable is the persistence projection of one data table.
type StructureTable struct {
	ID           string
	EnterpriseID string
	BaseID       string
	Name         string
	Position     int
	FieldCount   int
	ViewCount    int
	CreatedAt    int64
}

// StructureField is the persistence projection of one field.
type StructureField struct {
	ID        string
	TableID   string
	Name      string
	Type      string
	Config    []byte
	IsPrimary bool
	Position  int
	CreatedAt int64
}

// StructureOption is the persistence projection of one select option.
type StructureOption struct {
	FieldID  string
	OptionID string
	Name     string
	Color    string
	Position int
}

// StructureView is the persistence projection of one table view.
type StructureView struct {
	ID        string
	TableID   string
	Name      string
	Type      string
	Config    []byte
	Position  int
	CreatedAt int64
}

// StructureRepository owns only excel_structure_* tables.
type StructureRepository struct {
	db *sql.DB
}

// NewStructureRepository creates the structure persistence gateway.
func NewStructureRepository(db *sql.DB) *StructureRepository {
	return &StructureRepository{db: db}
}

// BeginTx starts one structure mutation transaction.
func (repository *StructureRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("structure database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin structure transaction: %w", err)
	}
	return tx, nil
}

// CreateTable inserts a table row.
func (repository *StructureRepository) CreateTable(
	ctx context.Context,
	table StructureTable,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("structure database is required")
	}
	_, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_structure_tables (
			id, enterprise_id, base_id, name, position, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, table.ID, table.EnterpriseID, table.BaseID, table.Name, table.Position, table.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert structure table: %w", err)
	}
	return nil
}

// DuplicateTableStructure atomically copies a table plus its fields, options
// and views into one new table identity.
func (repository *StructureRepository) DuplicateTableStructure(
	ctx context.Context,
	table StructureTable,
	fields []StructureField,
	optionsByField map[string][]StructureOption,
	views []StructureView,
) error {
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO excel_structure_tables (
			id, enterprise_id, base_id, name, position, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`,
		table.ID,
		table.EnterpriseID,
		table.BaseID,
		table.Name,
		table.Position,
		table.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert duplicated structure table: %w", err)
	}
	for _, field := range fields {
		if err := insertStructureField(ctx, tx, field); err != nil {
			return err
		}
		if err := replaceStructureOptions(
			ctx,
			tx,
			field.ID,
			optionsByField[field.ID],
		); err != nil {
			return err
		}
	}
	for _, view := range views {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO excel_structure_views (
				id, table_id, name, type, config, position, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
			view.ID,
			view.TableID,
			view.Name,
			view.Type,
			view.Config,
			view.Position,
			view.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert duplicated structure view: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit duplicated structure table: %w", err)
	}
	return nil
}

// TableByID loads one table visible to the requested enterprise.
func (repository *StructureRepository) TableByID(
	ctx context.Context,
	enterpriseID, tableID string,
) (StructureTable, error) {
	if repository == nil || repository.db == nil {
		return StructureTable{}, errors.New("structure database is required")
	}
	var table StructureTable
	err := repository.db.QueryRowContext(ctx, `
		SELECT
			t.id, t.enterprise_id, t.base_id, t.name, t.position, t.created_at,
			(SELECT COUNT(*) FROM excel_structure_fields f WHERE f.table_id = t.id),
			(SELECT COUNT(*) FROM excel_structure_views v WHERE v.table_id = t.id)
		FROM excel_structure_tables t
		WHERE t.id = ? AND t.enterprise_id = ?
	`, tableID, enterpriseID).Scan(
		&table.ID,
		&table.EnterpriseID,
		&table.BaseID,
		&table.Name,
		&table.Position,
		&table.CreatedAt,
		&table.FieldCount,
		&table.ViewCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StructureTable{}, ErrStructureNotFound
	}
	if err != nil {
		return StructureTable{}, fmt.Errorf("query structure table: %w", err)
	}
	return table, nil
}

// TablesByBase returns tables ordered by their persisted position.
func (repository *StructureRepository) TablesByBase(
	ctx context.Context,
	enterpriseID, baseID string,
) ([]StructureTable, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("structure database is required")
	}
	rows, err := repository.db.QueryContext(ctx, `
		SELECT
			t.id, t.enterprise_id, t.base_id, t.name, t.position, t.created_at,
			(SELECT COUNT(*) FROM excel_structure_fields f WHERE f.table_id = t.id),
			(SELECT COUNT(*) FROM excel_structure_views v WHERE v.table_id = t.id)
		FROM excel_structure_tables t
		WHERE t.base_id = ? AND t.enterprise_id = ?
		ORDER BY t.position, t.created_at, t.id
	`, baseID, enterpriseID)
	if err != nil {
		return nil, fmt.Errorf("query structure tables: %w", err)
	}
	defer rows.Close()

	tables := make([]StructureTable, 0)
	for rows.Next() {
		var table StructureTable
		if err := rows.Scan(
			&table.ID,
			&table.EnterpriseID,
			&table.BaseID,
			&table.Name,
			&table.Position,
			&table.CreatedAt,
			&table.FieldCount,
			&table.ViewCount,
		); err != nil {
			return nil, fmt.Errorf("scan structure table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate structure tables: %w", err)
	}
	return tables, nil
}

// CountTables returns the table count for one Base.
func (repository *StructureRepository) CountTables(
	ctx context.Context,
	enterpriseID, baseID string,
) (int, error) {
	var count int
	err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM excel_structure_tables
		WHERE base_id = ? AND enterprise_id = ?
	`, baseID, enterpriseID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count structure tables: %w", err)
	}
	return count, nil
}

// TableNameExists checks an exact trimmed table name within one Base.
func (repository *StructureRepository) TableNameExists(
	ctx context.Context,
	enterpriseID, baseID, name, exceptID string,
) (bool, error) {
	var count int
	err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM excel_structure_tables
		WHERE base_id = ? AND enterprise_id = ? AND BINARY name = ? AND id <> ?
	`, baseID, enterpriseID, name, exceptID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check structure table name: %w", err)
	}
	return count > 0, nil
}

// RenameTable updates a table name.
func (repository *StructureRepository) RenameTable(
	ctx context.Context,
	enterpriseID, tableID, name string,
) error {
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_structure_tables SET name = ?
		WHERE id = ? AND enterprise_id = ?
	`, name, tableID, enterpriseID)
	if err != nil {
		return fmt.Errorf("rename structure table: %w", err)
	}
	return requireStructureRow(result, "rename structure table")
}

// CreateField inserts a field and replaces its option projection atomically.
func (repository *StructureRepository) CreateField(
	ctx context.Context,
	field StructureField,
	options []StructureOption,
) error {
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertStructureField(ctx, tx, field); err != nil {
		return err
	}
	if err := replaceStructureOptions(ctx, tx, field.ID, options); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create structure field: %w", err)
	}
	return nil
}

func insertStructureField(
	ctx context.Context,
	tx *sql.Tx,
	field StructureField,
) error {
	if tx == nil {
		return errors.New("structure transaction is required")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO excel_structure_fields (
			id, table_id, name, type, config, is_primary, position, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		field.ID,
		field.TableID,
		field.Name,
		field.Type,
		field.Config,
		field.IsPrimary,
		field.Position,
		field.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert structure field: %w", err)
	}
	return nil
}

// FieldByID loads one field by id.
func (repository *StructureRepository) FieldByID(
	ctx context.Context,
	fieldID string,
) (StructureField, error) {
	var field StructureField
	err := repository.db.QueryRowContext(ctx, `
		SELECT id, table_id, name, type, config, is_primary, position, created_at
		FROM excel_structure_fields
		WHERE id = ?
	`, fieldID).Scan(
		&field.ID,
		&field.TableID,
		&field.Name,
		&field.Type,
		&field.Config,
		&field.IsPrimary,
		&field.Position,
		&field.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StructureField{}, ErrStructureNotFound
	}
	if err != nil {
		return StructureField{}, fmt.Errorf("query structure field: %w", err)
	}
	return field, nil
}

// FieldsByTable returns fields in display order.
func (repository *StructureRepository) FieldsByTable(
	ctx context.Context,
	tableID string,
) ([]StructureField, error) {
	rows, err := repository.db.QueryContext(ctx, `
		SELECT id, table_id, name, type, config, is_primary, position, created_at
		FROM excel_structure_fields
		WHERE table_id = ?
		ORDER BY position, created_at, id
	`, tableID)
	if err != nil {
		return nil, fmt.Errorf("query structure fields: %w", err)
	}
	defer rows.Close()
	return scanStructureFields(rows)
}

// AllFields returns the complete field graph used by calc.
func (repository *StructureRepository) AllFields(
	ctx context.Context,
) ([]StructureField, error) {
	rows, err := repository.db.QueryContext(ctx, `
		SELECT id, table_id, name, type, config, is_primary, position, created_at
		FROM excel_structure_fields
		ORDER BY table_id, position, created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query all structure fields: %w", err)
	}
	defer rows.Close()
	return scanStructureFields(rows)
}

func scanStructureFields(rows *sql.Rows) ([]StructureField, error) {
	fields := make([]StructureField, 0)
	for rows.Next() {
		var field StructureField
		if err := rows.Scan(
			&field.ID,
			&field.TableID,
			&field.Name,
			&field.Type,
			&field.Config,
			&field.IsPrimary,
			&field.Position,
			&field.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan structure field: %w", err)
		}
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate structure fields: %w", err)
	}
	return fields, nil
}

// CountFields returns the number of fields in one table.
func (repository *StructureRepository) CountFields(
	ctx context.Context,
	tableID string,
) (int, error) {
	var count int
	err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM excel_structure_fields WHERE table_id = ?
	`, tableID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count structure fields: %w", err)
	}
	return count, nil
}

// FieldNameExists checks an exact trimmed field name within one table.
func (repository *StructureRepository) FieldNameExists(
	ctx context.Context,
	tableID, name, exceptID string,
) (bool, error) {
	var count int
	err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM excel_structure_fields
		WHERE table_id = ? AND BINARY name = ? AND id <> ?
	`, tableID, name, exceptID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check structure field name: %w", err)
	}
	return count > 0, nil
}

// RenameField updates a field name.
func (repository *StructureRepository) RenameField(
	ctx context.Context,
	fieldID, name string,
) error {
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_structure_fields SET name = ? WHERE id = ?
	`, name, fieldID)
	if err != nil {
		return fmt.Errorf("rename structure field: %w", err)
	}
	return requireStructureRow(result, "rename structure field")
}

// UpdateField replaces a field type/config and option projection atomically.
func (repository *StructureRepository) UpdateField(
	ctx context.Context,
	field StructureField,
	options []StructureOption,
) error {
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_structure_fields
		SET type = ?, config = ?
		WHERE id = ? AND table_id = ?
	`, field.Type, field.Config, field.ID, field.TableID)
	if err != nil {
		return fmt.Errorf("update structure field: %w", err)
	}
	if err := requireStructureRow(result, "update structure field"); err != nil {
		return err
	}
	if err := replaceStructureOptions(ctx, tx, field.ID, options); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update structure field: %w", err)
	}
	return nil
}

// DeleteField removes a non-primary field and its option projection.
func (repository *StructureRepository) DeleteField(
	ctx context.Context,
	fieldID string,
) error {
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_structure_field_options WHERE field_id = ?
	`, fieldID); err != nil {
		return fmt.Errorf("delete structure field options: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM excel_structure_fields WHERE id = ? AND is_primary = 0
	`, fieldID)
	if err != nil {
		return fmt.Errorf("delete structure field: %w", err)
	}
	if err := requireStructureRow(result, "delete structure field"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete structure field: %w", err)
	}
	return nil
}

// CreateView inserts one table view.
func (repository *StructureRepository) CreateView(
	ctx context.Context,
	view StructureView,
) error {
	_, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_structure_views (
			id, table_id, name, type, config, position, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, view.ID, view.TableID, view.Name, view.Type, view.Config, view.Position, view.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert structure view: %w", err)
	}
	return nil
}

// ViewByID loads one view.
func (repository *StructureRepository) ViewByID(
	ctx context.Context,
	viewID string,
) (StructureView, error) {
	var view StructureView
	err := repository.db.QueryRowContext(ctx, `
		SELECT id, table_id, name, type, config, position, created_at
		FROM excel_structure_views
		WHERE id = ?
	`, viewID).Scan(
		&view.ID,
		&view.TableID,
		&view.Name,
		&view.Type,
		&view.Config,
		&view.Position,
		&view.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StructureView{}, ErrStructureNotFound
	}
	if err != nil {
		return StructureView{}, fmt.Errorf("query structure view: %w", err)
	}
	return view, nil
}

// ViewsByTable returns views in display order.
func (repository *StructureRepository) ViewsByTable(
	ctx context.Context,
	tableID string,
) ([]StructureView, error) {
	rows, err := repository.db.QueryContext(ctx, `
		SELECT id, table_id, name, type, config, position, created_at
		FROM excel_structure_views
		WHERE table_id = ?
		ORDER BY position, created_at, id
	`, tableID)
	if err != nil {
		return nil, fmt.Errorf("query structure views: %w", err)
	}
	defer rows.Close()
	views := make([]StructureView, 0)
	for rows.Next() {
		var view StructureView
		if err := rows.Scan(
			&view.ID,
			&view.TableID,
			&view.Name,
			&view.Type,
			&view.Config,
			&view.Position,
			&view.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan structure view: %w", err)
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate structure views: %w", err)
	}
	return views, nil
}

// CountViews returns the number of views in one table.
func (repository *StructureRepository) CountViews(
	ctx context.Context,
	tableID string,
) (int, error) {
	var count int
	err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM excel_structure_views WHERE table_id = ?
	`, tableID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count structure views: %w", err)
	}
	return count, nil
}

// ViewNameExists checks an exact trimmed view name within one table.
func (repository *StructureRepository) ViewNameExists(
	ctx context.Context,
	tableID, name, exceptID string,
) (bool, error) {
	var count int
	err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM excel_structure_views
		WHERE table_id = ? AND BINARY name = ? AND id <> ?
	`, tableID, name, exceptID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check structure view name: %w", err)
	}
	return count > 0, nil
}

// RenameView updates a view name.
func (repository *StructureRepository) RenameView(
	ctx context.Context,
	viewID, name string,
) error {
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_structure_views SET name = ? WHERE id = ?
	`, name, viewID)
	if err != nil {
		return fmt.Errorf("rename structure view: %w", err)
	}
	return requireStructureRow(result, "rename structure view")
}

// UpdateView replaces one view's name/type/config.
func (repository *StructureRepository) UpdateView(
	ctx context.Context,
	view StructureView,
) error {
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_structure_views SET name = ?, type = ?, config = ?
		WHERE id = ? AND table_id = ?
	`, view.Name, view.Type, view.Config, view.ID, view.TableID)
	if err != nil {
		return fmt.Errorf("update structure view: %w", err)
	}
	return requireStructureRow(result, "update structure view")
}

// DeleteView removes one view.
func (repository *StructureRepository) DeleteView(
	ctx context.Context,
	viewID string,
) error {
	result, err := repository.db.ExecContext(ctx, `
		DELETE FROM excel_structure_views WHERE id = ?
	`, viewID)
	if err != nil {
		return fmt.Errorf("delete structure view: %w", err)
	}
	return requireStructureRow(result, "delete structure view")
}

// DeleteTable cascades to every structure-owned child row of one table.
func (repository *StructureRepository) DeleteTable(
	ctx context.Context,
	tableID string,
) error {
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		DELETE options
		FROM excel_structure_field_options AS options
		INNER JOIN excel_structure_fields AS fields ON fields.id = options.field_id
		WHERE fields.table_id = ?
	`, tableID); err != nil {
		return fmt.Errorf("delete structure field options by table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_structure_fields WHERE table_id = ?
	`, tableID); err != nil {
		return fmt.Errorf("delete structure fields by table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_structure_views WHERE table_id = ?
	`, tableID); err != nil {
		return fmt.Errorf("delete structure views by table: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM excel_structure_tables WHERE id = ?
	`, tableID)
	if err != nil {
		return fmt.Errorf("delete structure table: %w", err)
	}
	if err := requireStructureRow(result, "delete structure table"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete structure table: %w", err)
	}
	return nil
}

func replaceStructureOptions(
	ctx context.Context,
	tx *sql.Tx,
	fieldID string,
	options []StructureOption,
) error {
	if tx == nil {
		return errors.New("structure transaction is required")
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_structure_field_options WHERE field_id = ?
	`, fieldID); err != nil {
		return fmt.Errorf("clear structure field options: %w", err)
	}
	for _, option := range options {
		option.FieldID = fieldID
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO excel_structure_field_options (
				field_id, option_id, name, color, position
			) VALUES (?, ?, ?, ?, ?)
		`, option.FieldID, option.OptionID, option.Name, option.Color, option.Position); err != nil {
			return fmt.Errorf("insert structure field option: %w", err)
		}
	}
	return nil
}

// DecodeStructureConfig decodes a persisted config object.
func DecodeStructureConfig(payload []byte) (map[string]any, error) {
	if len(payload) == 0 {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal(payload, &config); err != nil {
		return nil, fmt.Errorf("decode structure config: %w", err)
	}
	if config == nil {
		config = map[string]any{}
	}
	return config, nil
}

// EncodeStructureConfig validates and encodes a config object.
func EncodeStructureConfig(config map[string]any) ([]byte, error) {
	if config == nil {
		config = map[string]any{}
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode structure config: %w", err)
	}
	return payload, nil
}

func requireStructureRow(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: read affected rows: %w", operation, err)
	}
	if affected == 0 {
		return ErrStructureNotFound
	}
	return nil
}
