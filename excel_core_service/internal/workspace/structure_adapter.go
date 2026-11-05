package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"excel_core_service/base"
)

type structureDuplicator struct {
	repository *base.StructureRepository
	now        func() time.Time
}

// NewStructureDuplicator creates the structure copy adapter used by DuplicateBase.
func NewStructureDuplicator(
	repository *base.StructureRepository,
	now func() time.Time,
) StructureDuplicator {
	if now == nil {
		now = time.Now
	}
	return &structureDuplicator{repository: repository, now: now}
}

func (duplicator *structureDuplicator) DuplicateBase(
	ctx context.Context,
	enterpriseID, sourceBaseID, targetBaseID string,
) (map[string]string, error) {
	if duplicator == nil || duplicator.repository == nil {
		return nil, errors.New("structure repository is required")
	}
	tx, err := duplicator.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	tables, err := duplicateSourceTables(ctx, tx, enterpriseID, sourceBaseID)
	if err != nil {
		return nil, err
	}
	tableIDs := make(map[string]string, len(tables))
	for _, table := range tables {
		tableID, err := idgen.New()
		if err != nil {
			return nil, err
		}
		tableIDs[table.ID] = tableID
	}
	fieldsByTable := make(map[string][]base.StructureField, len(tables))
	fieldIDs := make(map[string]string)
	for _, table := range tables {
		fields, err := duplicateSourceFields(ctx, tx, table.ID)
		if err != nil {
			return nil, err
		}
		fieldsByTable[table.ID] = fields
		for _, field := range fields {
			fieldID, err := idgen.New()
			if err != nil {
				return nil, err
			}
			fieldIDs[field.ID] = fieldID
		}
	}

	now := duplicator.now().UTC().UnixMilli()
	for _, table := range tables {
		targetTableID := tableIDs[table.ID]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO excel_structure_tables (
				id, enterprise_id, base_id, name, position, created_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`, targetTableID, enterpriseID, targetBaseID, table.Name,
			table.Position, now,
		); err != nil {
			return nil, fmt.Errorf("insert duplicated Base table: %w", err)
		}
		for _, sourceField := range fieldsByTable[table.ID] {
			config, err := remapFieldConfig(
				sourceField.Config,
				tableIDs,
				fieldIDs,
			)
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO excel_structure_fields (
					id, table_id, name, type, config, description, is_primary, position, created_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, fieldIDs[sourceField.ID], targetTableID, sourceField.Name,
				sourceField.Type, config, sourceField.Description, sourceField.IsPrimary,
				sourceField.Position, now,
			); err != nil {
				return nil, fmt.Errorf("insert duplicated Base field: %w", err)
			}
			if err := duplicateFieldOptions(
				ctx,
				tx,
				sourceField.ID,
				fieldIDs[sourceField.ID],
			); err != nil {
				return nil, err
			}
		}
		if err := duplicateViews(
			ctx,
			tx,
			table.ID,
			targetTableID,
			fieldIDs,
			now,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit duplicated Base structure: %w", err)
	}
	return tableIDs, nil
}

func duplicateSourceTables(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID, baseID string,
) ([]base.StructureTable, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, enterprise_id, base_id, name, position, created_at
		FROM excel_structure_tables
		WHERE enterprise_id = ? AND base_id = ?
		ORDER BY position, created_at, id
	`, enterpriseID, baseID)
	if err != nil {
		return nil, fmt.Errorf("query source Base tables: %w", err)
	}
	defer rows.Close()
	tables := make([]base.StructureTable, 0)
	for rows.Next() {
		var table base.StructureTable
		if err := rows.Scan(
			&table.ID,
			&table.EnterpriseID,
			&table.BaseID,
			&table.Name,
			&table.Position,
			&table.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan source Base table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source Base tables: %w", err)
	}
	return tables, nil
}

func duplicateSourceFields(
	ctx context.Context,
	tx *sql.Tx,
	tableID string,
) ([]base.StructureField, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, table_id, name, type, config, description, is_primary, position, created_at
		FROM excel_structure_fields
		WHERE table_id = ?
		ORDER BY position, created_at, id
	`, tableID)
	if err != nil {
		return nil, fmt.Errorf("query source table fields: %w", err)
	}
	defer rows.Close()
	fields := make([]base.StructureField, 0)
	for rows.Next() {
		var field base.StructureField
		if err := rows.Scan(
			&field.ID,
			&field.TableID,
			&field.Name,
			&field.Type,
			&field.Config,
			&field.Description,
			&field.IsPrimary,
			&field.Position,
			&field.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan source table field: %w", err)
		}
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source table fields: %w", err)
	}
	return fields, nil
}

func remapFieldConfig(
	payload []byte,
	tableIDs, fieldIDs map[string]string,
) ([]byte, error) {
	config, err := base.DecodeStructureConfig(payload)
	if err != nil {
		return nil, err
	}
	if target, ok := config["target_table_id"].(string); ok {
		if mapped := tableIDs[target]; mapped != "" {
			config["target_table_id"] = mapped
		}
	}
	for _, key := range []string{"relation_field_id", "target_field_id"} {
		if source, ok := config[key].(string); ok {
			if mapped := fieldIDs[source]; mapped != "" {
				config[key] = mapped
			}
		}
	}
	return base.EncodeStructureConfig(config)
}

func duplicateFieldOptions(
	ctx context.Context,
	tx *sql.Tx,
	sourceFieldID, targetFieldID string,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT option_id, name, color, position
		FROM excel_structure_field_options
		WHERE field_id = ?
		ORDER BY position, option_id
	`, sourceFieldID)
	if err != nil {
		return fmt.Errorf("query source field options: %w", err)
	}
	defer rows.Close()
	type option struct {
		id       string
		name     string
		color    string
		position int
	}
	options := make([]option, 0)
	for rows.Next() {
		var item option
		if err := rows.Scan(&item.id, &item.name, &item.color, &item.position); err != nil {
			return fmt.Errorf("scan source field option: %w", err)
		}
		options = append(options, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source field options: %w", err)
	}
	for _, item := range options {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO excel_structure_field_options (
				field_id, option_id, name, color, position
			) VALUES (?, ?, ?, ?, ?)
		`, targetFieldID, item.id, item.name, item.color, item.position); err != nil {
			return fmt.Errorf("insert duplicated field option: %w", err)
		}
	}
	return nil
}

func duplicateViews(
	ctx context.Context,
	tx *sql.Tx,
	sourceTableID, targetTableID string,
	fieldIDs map[string]string,
	now int64,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, type, config, position
		FROM excel_structure_views
		WHERE table_id = ?
		ORDER BY position, created_at, id
	`, sourceTableID)
	if err != nil {
		return fmt.Errorf("query source table views: %w", err)
	}
	defer rows.Close()
	type view struct {
		name     string
		kind     string
		config   []byte
		position int
	}
	views := make([]view, 0)
	for rows.Next() {
		var item view
		var sourceID string
		if err := rows.Scan(
			&sourceID,
			&item.name,
			&item.kind,
			&item.config,
			&item.position,
		); err != nil {
			return fmt.Errorf("scan source table view: %w", err)
		}
		views = append(views, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source table views: %w", err)
	}
	for _, item := range views {
		config, err := remapViewConfig(item.config, fieldIDs)
		if err != nil {
			return err
		}
		viewID, err := idgen.New()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO excel_structure_views (
				id, table_id, name, type, config, position, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`, viewID, targetTableID, item.name, item.kind, config, item.position, now); err != nil {
			return fmt.Errorf("insert duplicated table view: %w", err)
		}
	}
	return nil
}

func remapViewConfig(payload []byte, fieldIDs map[string]string) ([]byte, error) {
	config := &structurev1.ViewConfig{}
	if len(payload) > 0 {
		if err := protojson.Unmarshal(payload, config); err != nil {
			return nil, fmt.Errorf("decode source view config: %w", err)
		}
	}
	config.HiddenFieldList = remapIDs(config.GetHiddenFieldList(), fieldIDs)
	config.FrozenFieldList = remapIDs(config.GetFrozenFieldList(), fieldIDs)
	for _, filter := range config.GetFilterList() {
		if mapped := fieldIDs[filter.GetFieldId()]; mapped != "" {
			filter.FieldId = mapped
		}
	}
	for _, sortItem := range config.GetSortList() {
		if mapped := fieldIDs[sortItem.GetFieldId()]; mapped != "" {
			sortItem.FieldId = mapped
		}
	}
	for _, group := range config.GetGroupByList() {
		if mapped := fieldIDs[group.GetFieldId()]; mapped != "" {
			group.FieldId = mapped
		}
	}
	encoded, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode duplicated view config: %w", err)
	}
	return encoded, nil
}

func remapIDs(ids []string, fieldIDs map[string]string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if mapped := fieldIDs[id]; mapped != "" {
			result = append(result, mapped)
		}
	}
	return result
}
