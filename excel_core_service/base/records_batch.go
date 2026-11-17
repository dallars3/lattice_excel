// base/records_batch.go：records 写链的批量落库语句（导入 20k 行提速）。
// 与逐行 CreateRecordTx/AppendHistoryTx/InsertOplogTx 语义一致，只是把同构行
// 合并为一次多行 INSERT；占位符按 MySQL 上限（65535）分片，避免超长语句。
package base

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// maxSQLPlaceholders 是单条预编译语句的占位符上限（MySQL 65535，留余量）。
const maxSQLPlaceholders = 60000

// chunkRowsForColumns 返回多行 INSERT 每条语句可容纳的行数。
func chunkRowsForColumns(columns int) int {
	if columns <= 0 {
		return 0
	}
	size := maxSQLPlaceholders / columns
	if size > 1000 {
		size = 1000
	}
	if size < 1 {
		return 1
	}
	return size
}

// CreateRecordsTx 在调用者事务内批量插入记录行。
func (repository *RecordsRepository) CreateRecordsTx(
	ctx context.Context,
	tx *sql.Tx,
	records []Record,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	const columns = 6
	for start := 0; start < len(records); start += chunkRowsForColumns(columns) {
		end := start + chunkRowsForColumns(columns)
		if end > len(records) {
			end = len(records)
		}
		chunk := records[start:end]
		var builder strings.Builder
		builder.WriteString(`INSERT INTO excel_records (
			id, table_id, cells_json, created_by, created_at, version
		) VALUES `)
		args := make([]any, 0, len(chunk)*columns)
		for index, record := range chunk {
			if index > 0 {
				builder.WriteString(",")
			}
			builder.WriteString("(?, ?, ?, ?, ?, ?)")
			cellsJSON, err := EncodeRecordCells(record.Cells)
			if err != nil {
				return err
			}
			args = append(args, record.ID, record.TableID, cellsJSON,
				record.CreatedBy, record.CreatedAt, record.Version)
		}
		if _, err := tx.ExecContext(ctx, builder.String(), args...); err != nil {
			return fmt.Errorf("batch insert records: %w", err)
		}
	}
	return nil
}

// AppendHistoryRowsTx 在调用者事务内批量追加字段级历史节点。
func (repository *RecordsRepository) AppendHistoryRowsTx(
	ctx context.Context,
	tx *sql.Tx,
	histories []RecordHistory,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	const columns = 7
	for start := 0; start < len(histories); start += chunkRowsForColumns(columns) {
		end := start + chunkRowsForColumns(columns)
		if end > len(histories) {
			end = len(histories)
		}
		chunk := histories[start:end]
		var builder strings.Builder
		builder.WriteString(`INSERT INTO excel_records_histories (
			id, record_id, field_id, ` + "`before`" + `, ` + "`after`" + `, operator, created_at
		) VALUES `)
		args := make([]any, 0, len(chunk)*columns)
		for index, history := range chunk {
			if history.ID == "" || history.RecordID == "" ||
				history.FieldID == "" || history.Operator == "" {
				return errors.New("record history identity, field and operator are required")
			}
			before, err := nullableEncodedCell(history.Before)
			if err != nil {
				return err
			}
			after, err := nullableEncodedCell(history.After)
			if err != nil {
				return err
			}
			if index > 0 {
				builder.WriteString(",")
			}
			builder.WriteString("(?, ?, ?, ?, ?, ?, ?)")
			args = append(args, history.ID, history.RecordID, history.FieldID,
				before, after, history.Operator, history.CreatedAt)
		}
		if _, err := tx.ExecContext(ctx, builder.String(), args...); err != nil {
			return fmt.Errorf("batch append record history: %w", err)
		}
	}
	return nil
}

// OplogByClientRequests 一次查询多个幂等键；缺失的键不出现在结果里。
func (repository *RecordsRepository) OplogByClientRequests(
	ctx context.Context,
	clientRequestIDs []string,
) ([]RecordOplogEntry, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("records database is required")
	}
	if len(clientRequestIDs) == 0 {
		return nil, nil
	}
	entries := make([]RecordOplogEntry, 0, len(clientRequestIDs))
	for start := 0; start < len(clientRequestIDs); start += chunkRowsForColumns(1) {
		end := start + chunkRowsForColumns(1)
		if end > len(clientRequestIDs) {
			end = len(clientRequestIDs)
		}
		chunk := clientRequestIDs[start:end]
		placeholders := strings.Repeat("?,", len(chunk))
		query := fmt.Sprintf(`SELECT client_request_id, kind, request_json, response_json, created_at
			FROM excel_records_oplog
			WHERE client_request_id IN (%s)`, placeholders[:len(placeholders)-1])
		args := make([]any, 0, len(chunk))
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := repository.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query records oplog batch: %w", err)
		}
		for rows.Next() {
			var entry RecordOplogEntry
			if err := rows.Scan(&entry.ClientRequestID, &entry.Kind,
				&entry.RequestJSON, &entry.ResponseJSON, &entry.CreatedAt); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan records oplog batch: %w", err)
			}
			entries = append(entries, entry)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate records oplog batch: %w", err)
		}
		_ = rows.Close()
	}
	return entries, nil
}

// DeleteExpiredOplogIDsTx 批量清理过期幂等键，使同批 id 可复用。
func (repository *RecordsRepository) DeleteExpiredOplogIDsTx(
	ctx context.Context,
	tx *sql.Tx,
	clientRequestIDs []string,
	cutoff int64,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	for start := 0; start < len(clientRequestIDs); start += chunkRowsForColumns(1) {
		end := start + chunkRowsForColumns(1)
		if end > len(clientRequestIDs) {
			end = len(clientRequestIDs)
		}
		chunk := clientRequestIDs[start:end]
		placeholders := strings.Repeat("?,", len(chunk))
		query := fmt.Sprintf(`DELETE FROM excel_records_oplog
			WHERE client_request_id IN (%s) AND created_at <= ?`, placeholders[:len(placeholders)-1])
		args := make([]any, 0, len(chunk)+1)
		for _, id := range chunk {
			args = append(args, id)
		}
		args = append(args, cutoff)
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("delete expired records oplog batch: %w", err)
		}
	}
	return nil
}

// InsertOplogRowsTx 批量预留幂等键（与 InsertOplogTx 同语义：请求/响应必须是
// 合法 JSON；主键冲突整体报错——批量路径先用 OplogByClientRequests 预筛重放行）。
func (repository *RecordsRepository) InsertOplogRowsTx(
	ctx context.Context,
	tx *sql.Tx,
	entries []RecordOplogEntry,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	const columns = 5
	for start := 0; start < len(entries); start += chunkRowsForColumns(columns) {
		end := start + chunkRowsForColumns(columns)
		if end > len(entries) {
			end = len(entries)
		}
		chunk := entries[start:end]
		var builder strings.Builder
		builder.WriteString(`INSERT INTO excel_records_oplog (
			client_request_id, kind, request_json, response_json, created_at
		) VALUES `)
		args := make([]any, 0, len(chunk)*columns)
		for index, entry := range chunk {
			if !jsonValid(entry.RequestJSON) || !jsonValid(entry.ResponseJSON) {
				return errors.New("records oplog JSON must be valid")
			}
			if index > 0 {
				builder.WriteString(",")
			}
			builder.WriteString("(?, ?, ?, ?, ?)")
			args = append(args, entry.ClientRequestID, entry.Kind,
				entry.RequestJSON, entry.ResponseJSON, entry.CreatedAt)
		}
		if _, err := tx.ExecContext(ctx, builder.String(), args...); err != nil {
			return fmt.Errorf("batch insert records oplog: %w", err)
		}
	}
	return nil
}

// UpsertOplogResponsesTx 一次写回整批幂等响应（幂等键已在同一事务内预留）。
func (repository *RecordsRepository) UpsertOplogResponsesTx(
	ctx context.Context,
	tx *sql.Tx,
	entries []RecordOplogEntry,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	const columns = 5
	for start := 0; start < len(entries); start += chunkRowsForColumns(columns) {
		end := start + chunkRowsForColumns(columns)
		if end > len(entries) {
			end = len(entries)
		}
		chunk := entries[start:end]
		var builder strings.Builder
		builder.WriteString(`INSERT INTO excel_records_oplog (
			client_request_id, kind, request_json, response_json, created_at
		) VALUES `)
		args := make([]any, 0, len(chunk)*columns)
		for index, entry := range chunk {
			if !jsonValid(entry.ResponseJSON) {
				return errors.New("records oplog response must be valid JSON")
			}
			if index > 0 {
				builder.WriteString(",")
			}
			builder.WriteString("(?, ?, ?, ?, ?)")
			args = append(args, entry.ClientRequestID, entry.Kind,
				entry.RequestJSON, entry.ResponseJSON, entry.CreatedAt)
		}
		builder.WriteString(` AS reserved ON DUPLICATE KEY UPDATE response_json = reserved.response_json`)
		if _, err := tx.ExecContext(ctx, builder.String(), args...); err != nil {
			return fmt.Errorf("batch update records oplog response: %w", err)
		}
	}
	return nil
}

func jsonValid(payload []byte) bool {
	return len(payload) > 0 && json.Valid(payload)
}
