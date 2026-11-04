// scheduler/lifecycle_intake.go：生命周期到期任务 intake。
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"excel-libs/eventbus"
	"excel_worker_service/base"
	workerbus "excel_worker_service/bus"
)

const lifecycleIntakeConsumerName = "worker-lifecycle"

// LifecycleIntake consumes deactivation.initiated / dissolution.initiated events
// and materializes each into one due scheduler task.
type LifecycleIntake struct {
	tasks *base.TaskRepository
}

// NewLifecycleIntake constructs the E-18 / E-19 intake writer.
func NewLifecycleIntake(repository *base.TaskRepository) (*LifecycleIntake, error) {
	if repository == nil {
		return nil, errors.New("task repository is required")
	}
	return &LifecycleIntake{tasks: repository}, nil
}

// RegisterLifecycleIntake subscribes the intake to both lifecycle topics.
func RegisterLifecycleIntake(consumer *workerbus.Consumer, intake *LifecycleIntake) error {
	if consumer == nil {
		return errors.New("worker consumer is required")
	}
	if intake == nil {
		return errors.New("lifecycle intake is required")
	}
	for _, topic := range []string{"deactivation.initiated", "dissolution.initiated"} {
		if err := consumer.Register(eventbus.SubscribeOptions{
			Topic:    topic,
			Mode:     eventbus.DeliveryModeReliable,
			Group:    lifecycleConsumerName,
			Consumer: lifecycleConsumerName,
		}, intake.Consume); err != nil {
			return err
		}
	}
	return nil
}

// Consume validates one intake event and writes a single delayed scheduler task.
func (intake *LifecycleIntake) Consume(
	ctx context.Context,
	event eventbus.Event,
) error {
	if intake == nil || intake.tasks == nil {
		return errors.New("lifecycle intake task repository is required")
	}
	var payload struct {
		EntityID   string `json:"entity_id"`
		Kind       string `json:"kind"`
		DeadlineMs int64  `json:"deadline_ms"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode lifecycle intake payload: %w", err)
	}

	taskType := ""
	var taskPayload any
	switch event.Header.Type {
	case "deactivation.initiated":
		if payload.Kind != "user" {
			return fmt.Errorf("deactivation.initiated kind %q, want user", payload.Kind)
		}
		taskType = base.TaskTypeDeactivation
		taskPayload = struct {
			UserID string `json:"user_id"`
		}{UserID: payload.EntityID}
	case "dissolution.initiated":
		if payload.Kind != "enterprise" {
			return fmt.Errorf("dissolution.initiated kind %q, want enterprise", payload.Kind)
		}
		taskType = base.TaskTypeEnterpriseDissolution
		taskPayload = struct {
			EnterpriseID string `json:"enterprise_id"`
		}{EnterpriseID: payload.EntityID}
	default:
		return fmt.Errorf("unsupported lifecycle intake topic %q", event.Header.Type)
	}
	if strings.TrimSpace(payload.EntityID) == "" {
		return errors.New("lifecycle intake entity_id is required")
	}
	if payload.DeadlineMs <= 0 {
		return errors.New("lifecycle intake deadline_ms is required")
	}
	encoded, err := json.Marshal(taskPayload)
	if err != nil {
		return fmt.Errorf("marshal lifecycle task payload: %w", err)
	}
	_, err = intake.tasks.EnqueueLifecycle(ctx, base.SchedulerTask{
		ID:       event.Header.EventID,
		Type:     taskType,
		Kind:     payload.Kind,
		EntityID: payload.EntityID,
		EventID:  event.Header.EventID,
		RunAt:    payload.DeadlineMs,
		Payload:  encoded,
	})
	if err != nil {
		return fmt.Errorf("enqueue lifecycle task: %w", err)
	}
	return nil
}