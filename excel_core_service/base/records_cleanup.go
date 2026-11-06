// base/records_cleanup.go：Base 级联删除的记录侧清理。
package base

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DeleteBaseRows removes every record-owned row for the Base's tables. The
// cleanup runs in one transaction so a partial failure cannot leave records
// without their histories or materialized values.
//
// 跨模块 SQL（§2.5）：删除 Base 必须同时清掉 calc 的物化行与任务行，否则会
// 留下无法再访问的孤儿数据；记录与 calc 表同库，直接在同一清理事务内处理。
func (repository *RecordsRepository) DeleteBaseRows(
	ctx context.Context,
	baseID string,
	tableIDList []string,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("records database is required")
	}
	if len(tableIDList) == 0 {
		return nil
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Base %s records cleanup: %w", baseID, err)
	}
	defer func() { _ = tx.Rollback() }()

	placeholders, arguments := cleanupPlaceholders(tableIDList)
	recordIDs, err := baseRecordIDs(ctx, tx, placeholders, arguments)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM excel_calc_materialized
		 WHERE record_id IN (
			SELECT id FROM excel_records WHERE table_id IN (` + placeholders + `)
		 )`,
		`DELETE FROM excel_records_histories
		 WHERE record_id IN (
			SELECT id FROM excel_records WHERE table_id IN (` + placeholders + `)
		 )`,
	} {
		if _, err := tx.ExecContext(ctx, statement, arguments...); err != nil {
			return fmt.Errorf("delete Base %s record dependents: %w", baseID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_calc_jobs WHERE table_id IN (`+placeholders+`)
	`, arguments...); err != nil {
		return fmt.Errorf("delete Base %s calc jobs: %w", baseID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_records_oplog
		WHERE JSON_UNQUOTE(JSON_EXTRACT(request_json, '$.table_id')) IN (`+placeholders+`)
		   OR JSON_UNQUOTE(JSON_EXTRACT(request_json, '$.record_id')) IN (
			SELECT id FROM excel_records WHERE table_id IN (`+placeholders+`)
		   )
	`, append(append([]any{}, arguments...), arguments...)...); err != nil {
		return fmt.Errorf("delete Base %s attributable oplog: %w", baseID, err)
	}
	if err := deleteOplogLists(ctx, tx, recordIDs); err != nil {
		return fmt.Errorf("delete Base %s list oplog: %w", baseID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_records WHERE table_id IN (`+placeholders+`)
	`, arguments...); err != nil {
		return fmt.Errorf("delete Base %s records: %w", baseID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Base %s records cleanup: %w", baseID, err)
	}
	return nil
}

func baseRecordIDs(
	ctx context.Context,
	tx *sql.Tx,
	placeholders string,
	arguments []any,
) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM excel_records WHERE table_id IN (`+placeholders+`)
	`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("collect Base record ids: %w", err)
	}
	defer rows.Close()
	ids := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan Base record id: %w", err)
		}
		ids[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Base record ids: %w", err)
	}
	return ids, nil
}

// deleteOplogLists removes idempotency rows whose whole record_id_list belongs
// to the deleted records. Lists spanning other tables are unattributable and
// stay untouched, so an oplog row can never be deleted by a partial match.
func deleteOplogLists(
	ctx context.Context,
	tx *sql.Tx,
	recordIDs map[string]struct{},
) error {
	if len(recordIDs) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT client_request_id, request_json
		FROM excel_records_oplog
		WHERE JSON_TYPE(JSON_EXTRACT(request_json, '$.record_id_list')) = 'ARRAY'
		  AND JSON_LENGTH(JSON_EXTRACT(request_json, '$.record_id_list')) > 0
	`)
	if err != nil {
		return fmt.Errorf("query list oplog: %w", err)
	}
	defer rows.Close()
	removable := make([]string, 0)
	for rows.Next() {
		var clientRequestID string
		var requestJSON []byte
		if err := rows.Scan(&clientRequestID, &requestJSON); err != nil {
			return fmt.Errorf("scan list oplog: %w", err)
		}
		var request struct {
			RecordIDList []string `json:"record_id_list"`
		}
		if err := json.Unmarshal(requestJSON, &request); err != nil {
			continue
		}
		if len(request.RecordIDList) == 0 {
			continue
		}
		owned := true
		for _, recordID := range request.RecordIDList {
			if _, ok := recordIDs[recordID]; !ok {
				owned = false
				break
			}
		}
		if owned {
			removable = append(removable, clientRequestID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate list oplog: %w", err)
	}
	for _, clientRequestID := range removable {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM excel_records_oplog WHERE client_request_id = ?
		`, clientRequestID); err != nil {
			return fmt.Errorf("delete list oplog %s: %w", clientRequestID, err)
		}
	}
	return nil
}

func cleanupPlaceholders(values []string) (string, []any) {
	placeholders := make([]string, len(values))
	arguments := make([]any, len(values))
	for index, value := range values {
		placeholders[index] = "?"
		arguments[index] = value
	}
	return strings.Join(placeholders, ", "), arguments
}
