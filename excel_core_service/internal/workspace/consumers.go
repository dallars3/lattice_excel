// workspace/consumers.go：事件消费者注册（import.finished 解锁导入 Base）。
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"excel-libs/eventbus"

	"excel_core_service/bus"
)

// WorkspaceConsumerGroup is the durable EventBus consumer group for workspace.
const WorkspaceConsumerGroup = "core-workspace"

// importFinishedPayload is the subset of the import.finished payload used here.
type importFinishedPayload struct {
	BaseID string `json:"base_id"`
}

// Consumers registers the workspace event handlers on the core consumer.
func Consumers(consumer *bus.Consumer, service *Service) error {
	if consumer == nil {
		return errors.New("workspace consumer is required")
	}
	if service == nil {
		return errors.New("workspace service is required")
	}
	if err := consumer.Register(eventbus.SubscribeOptions{
		Topic:    "import.finished",
		Mode:     eventbus.DeliveryModeReliable,
		Group:    WorkspaceConsumerGroup,
		Consumer: WorkspaceConsumerGroup,
	}, func(ctx context.Context, event eventbus.Event) error {
		return service.consumeImportFinished(ctx, event)
	}); err != nil {
		return fmt.Errorf("register workspace consumer for import.finished: %w", err)
	}
	return nil
}

// consumeImportFinished releases the import lock once the import completes
//（ticket 16：导入中新 Base 不可访问，完成后恢复可见）。重复投递幂等。
func (service *Service) consumeImportFinished(
	ctx context.Context,
	event eventbus.Event,
) error {
	var payload importFinishedPayload
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode import.finished payload: %w", err)
		}
	}
	if payload.BaseID == "" {
		return nil
	}
	if service == nil || service.repository == nil {
		return errors.New("workspace repository is required")
	}
	return service.repository.ClearImportStatus(ctx, payload.BaseID)
}
