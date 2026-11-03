// base/file.go：对应 excel_file_meta 表的数据操作。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	// FileMetaTable stores finalized file metadata; file bytes stay on disk.
	FileMetaTable = "excel_file_meta"
	fileUsageSQL  = "`usage`"
)

var (
	// ErrFileNotFound reports a missing file metadata row.
	ErrFileNotFound = errors.New("file metadata not found")
)

// FileMeta is the persistence projection of one stored file.
type FileMeta struct {
	ID          string
	Name        string
	DisplayName string
	Ext         string
	Mime        string
	Size        int64
	Usage       string
	Uploader    string
	UploadBase  string
	StoragePath string
	Checksum    string
	CreatedAt   int64
}

// FileRepository owns SQL access to excel_file_meta.
type FileRepository struct {
	db *sql.DB
}

// NewFileRepository creates the file metadata persistence gateway.
func NewFileRepository(db *sql.DB) *FileRepository {
	return &FileRepository{db: db}
}

// Create inserts one finalized file metadata row.
func (repository *FileRepository) Create(ctx context.Context, meta FileMeta) error {
	if repository == nil || repository.db == nil {
		return errors.New("file database is required")
	}
	_, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_file_meta (
			id, name, display_name, ext, mime, size, `+fileUsageSQL+`,
			uploader, upload_base, storage_path, checksum, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
	`, meta.ID, meta.Name, meta.DisplayName, meta.Ext, meta.Mime, meta.Size,
		meta.Usage, meta.Uploader, meta.UploadBase, meta.StoragePath,
		meta.Checksum, meta.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert file metadata: %w", err)
	}
	return nil
}

// Get reads one file metadata row by identifier.
func (repository *FileRepository) Get(ctx context.Context, fileID string) (FileMeta, error) {
	if repository == nil || repository.db == nil {
		return FileMeta{}, errors.New("file database is required")
	}
	var meta FileMeta
	err := repository.db.QueryRowContext(ctx, `
		SELECT id, name, display_name, ext, mime, size, `+fileUsageSQL+`,
		       uploader, COALESCE(upload_base, ''), storage_path,
		       checksum, created_at
		FROM excel_file_meta
		WHERE id = ?
	`, fileID).Scan(
		&meta.ID,
		&meta.Name,
		&meta.DisplayName,
		&meta.Ext,
		&meta.Mime,
		&meta.Size,
		&meta.Usage,
		&meta.Uploader,
		&meta.UploadBase,
		&meta.StoragePath,
		&meta.Checksum,
		&meta.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return FileMeta{}, ErrFileNotFound
	}
	if err != nil {
		return FileMeta{}, fmt.Errorf("read file metadata: %w", err)
	}
	return meta, nil
}

// Rename updates only the user-visible display name.
func (repository *FileRepository) Rename(
	ctx context.Context,
	fileID string,
	displayName string,
) (FileMeta, error) {
	if repository == nil || repository.db == nil {
		return FileMeta{}, errors.New("file database is required")
	}
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_file_meta
		SET display_name = ?
		WHERE id = ?
	`, displayName, fileID)
	if err != nil {
		return FileMeta{}, fmt.Errorf("rename file metadata: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return FileMeta{}, fmt.Errorf("read rename result: %w", err)
	}
	if affected == 0 {
		return FileMeta{}, ErrFileNotFound
	}
	return repository.Get(ctx, fileID)
}

// Delete removes metadata and returns the deleted row for disk and quota cleanup.
func (repository *FileRepository) Delete(
	ctx context.Context,
	fileID string,
) (FileMeta, error) {
	meta, err := repository.Get(ctx, fileID)
	if err != nil {
		return FileMeta{}, err
	}
	if _, err := repository.db.ExecContext(
		ctx,
		`DELETE FROM excel_file_meta WHERE id = ?`,
		fileID,
	); err != nil {
		return FileMeta{}, fmt.Errorf("delete file metadata: %w", err)
	}
	return meta, nil
}

// UsedBytes returns the authoritative attachment-pool ledger.
func (repository *FileRepository) UsedBytes(ctx context.Context) (int64, error) {
	if repository == nil || repository.db == nil {
		return 0, errors.New("file database is required")
	}
	var used sql.NullInt64
	if err := repository.db.QueryRowContext(ctx, `
		SELECT SUM(size) FROM excel_file_meta
	`).Scan(&used); err != nil {
		return 0, fmt.Errorf("sum attachment pool: %w", err)
	}
	if !used.Valid {
		return 0, nil
	}
	return used.Int64, nil
}
