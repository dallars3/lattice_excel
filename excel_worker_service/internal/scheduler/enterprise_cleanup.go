package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"excel-libs/eventbus"
	workerbus "excel_worker_service/bus"
)

const lifecycleConsumerName = "worker-lifecycle"

// EnterpriseFileCleaner removes persisted bytes and metadata for one enterprise.
type EnterpriseFileCleaner interface {
	CleanupFiles(ctx context.Context, baseIDs, generatedFileIDs []string) error
}

// EnterpriseTaskCleaner discovers and retires import/export tasks.
type EnterpriseTaskCleaner interface {
	EnterpriseFileIDs(ctx context.Context, baseIDs []string) ([]string, error)
	MarkEnterpriseCleaned(ctx context.Context, baseIDs []string) error
}

// EnterpriseNotificationCleaner removes notifications for the dissolved members.
type EnterpriseNotificationCleaner interface {
	CleanupNotifications(ctx context.Context, memberIDs []string) error
}

// EnterpriseCleanupDependencies are the worker-owned E-17 cleanup boundaries.
type EnterpriseCleanupDependencies struct {
	Files         EnterpriseFileCleaner
	Tasks         EnterpriseTaskCleaner
	Notifications EnterpriseNotificationCleaner
}

// EnterpriseCleanup consumes enterprise.dissolved events.
type EnterpriseCleanup struct {
	files         EnterpriseFileCleaner
	tasks         EnterpriseTaskCleaner
	notifications EnterpriseNotificationCleaner
}

// NewEnterpriseCleanup constructs the idempotent E-17 handler.
func NewEnterpriseCleanup(dependencies EnterpriseCleanupDependencies) (*EnterpriseCleanup, error) {
	if dependencies.Files == nil ||
		dependencies.Tasks == nil ||
		dependencies.Notifications == nil {
		return nil, errors.New("enterprise cleanup dependencies are required")
	}
	return &EnterpriseCleanup{
		files:         dependencies.Files,
		tasks:         dependencies.Tasks,
		notifications: dependencies.Notifications,
	}, nil
}

// Consumers registers the worker lifecycle consumer group.
func Consumers(consumer *workerbus.Consumer, cleanup *EnterpriseCleanup) error {
	if consumer == nil {
		return errors.New("worker consumer is required")
	}
	if cleanup == nil {
		return errors.New("enterprise cleanup is required")
	}
	return consumer.Register(eventbus.SubscribeOptions{
		Topic:    "enterprise.dissolved",
		Mode:     eventbus.DeliveryModeReliable,
		Group:    lifecycleConsumerName,
		Consumer: lifecycleConsumerName,
	}, cleanup.Consume)
}

// Consume applies all E-17 cleanup scopes. event_id deduplication is enforced by
// workerbus.Consumer before this handler is called.
func (cleanup *EnterpriseCleanup) Consume(
	ctx context.Context,
	event eventbus.Event,
) error {
	if cleanup == nil || cleanup.files == nil || cleanup.tasks == nil || cleanup.notifications == nil {
		return errors.New("enterprise cleanup dependencies are required")
	}
	if event.Header.Type != "enterprise.dissolved" {
		return fmt.Errorf("unsupported enterprise cleanup topic %q", event.Header.Type)
	}
	var payload struct {
		EnterpriseID string   `json:"enterprise_id"`
		BaseIDList   []string `json:"base_id_list"`
		MemberIDs    []string `json:"members_ids"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode enterprise.dissolved payload: %w", err)
	}
	if payload.EnterpriseID == "" {
		return errors.New("enterprise.dissolved enterprise_id is required")
	}
	generatedFileIDs, err := cleanup.tasks.EnterpriseFileIDs(ctx, payload.BaseIDList)
	if err != nil {
		return fmt.Errorf("list enterprise import/export files: %w", err)
	}
	if err := cleanup.files.CleanupFiles(ctx, payload.BaseIDList, generatedFileIDs); err != nil {
		return fmt.Errorf("clean enterprise files: %w", err)
	}
	if err := cleanup.tasks.MarkEnterpriseCleaned(ctx, payload.BaseIDList); err != nil {
		return fmt.Errorf("mark enterprise import/export tasks cleaned: %w", err)
	}
	if err := cleanup.notifications.CleanupNotifications(ctx, payload.MemberIDs); err != nil {
		return fmt.Errorf("clean enterprise notifications: %w", err)
	}
	return nil
}
