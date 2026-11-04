package importexport

import (
	"context"
	"errors"
)

// EnterpriseFileIDs returns disk-backed outputs owned by the dissolved Bases.
func (service *Service) EnterpriseFileIDs(
	ctx context.Context,
	baseIDs []string,
) ([]string, error) {
	if service == nil || service.tasks == nil {
		return nil, errors.New("import/export task repository is required")
	}
	return service.tasks.FileIDsByBaseIDs(ctx, baseIDs)
}

// MarkEnterpriseCleaned retires tasks after their generated files are removed.
func (service *Service) MarkEnterpriseCleaned(
	ctx context.Context,
	baseIDs []string,
) error {
	if service == nil || service.tasks == nil {
		return errors.New("import/export task repository is required")
	}
	_, err := service.tasks.MarkCleanedByBaseIDs(
		ctx,
		baseIDs,
		service.now().UTC().UnixMilli(),
	)
	return err
}
