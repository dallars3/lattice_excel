package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"excel_worker_service/base"
)

// CleanupFiles removes enterprise attachments, import traces and generated files.
func (service *Service) CleanupFiles(
	ctx context.Context,
	baseIDs, generatedFileIDs []string,
) error {
	if service == nil || service.repository == nil {
		return errors.New("file service dependencies are required")
	}
	files, err := service.repository.ListCleanupCandidates(ctx, baseIDs, generatedFileIDs)
	if err != nil {
		return err
	}
	for _, candidate := range files {
		deleted, err := service.repository.Delete(ctx, candidate.ID)
		if errors.Is(err, base.ErrFileNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		path := filepath.Join(service.attachDir, deleted.StoragePath)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			service.logger.Warn(
				"remove dissolved enterprise file bytes",
				"file_id", deleted.ID,
				"error", err,
			)
		}
		if service.quota != nil {
			if err := service.quota.Release(ctx, deleted.Size); err != nil {
				return err
			}
		}
	}
	return nil
}
