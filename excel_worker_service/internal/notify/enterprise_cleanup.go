package notify

import (
	"context"
	"errors"
)

// CleanupNotifications removes rows and counter residue for dissolved members.
func (service *Service) CleanupNotifications(
	ctx context.Context,
	memberIDs []string,
) error {
	if service == nil || service.repository == nil {
		return errors.New("notify service dependencies are required")
	}
	if _, err := service.repository.DeleteByUserIDs(ctx, memberIDs); err != nil {
		return err
	}
	if service.counters == nil {
		return nil
	}
	for _, memberID := range memberIDs {
		if err := service.counters.Clear(ctx, memberID); err != nil {
			return err
		}
	}
	return nil
}

// CleanupBefore deletes notifications older than the retention cutoff.
func (service *Service) CleanupBefore(ctx context.Context, cutoff int64) error {
	if service == nil || service.repository == nil {
		return errors.New("notify service dependencies are required")
	}
	_, err := service.repository.DeleteBefore(ctx, cutoff)
	return err
}
