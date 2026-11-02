// calc/consume.go：将可靠 EventBus 的 calc.job 主题映射到幂等重算路径。
package calc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"excel-libs/eventbus"
	"excel_core_service/bus"
)

// Consumers registers the reliable calc.job handler.
func Consumers(consumer *bus.Consumer, service *Service) error {
	if consumer == nil {
		return errors.New("calc consumer is required")
	}
	if service == nil {
		return errors.New("calc service is required")
	}
	if err := consumer.Register(eventbus.SubscribeOptions{
		Topic:    "calc.job",
		Mode:     eventbus.DeliveryModeReliable,
		Group:    calcConsumerGroup,
		Consumer: calcConsumerGroup,
	}, service.consumeCalcJob); err != nil {
		return fmt.Errorf("register calc consumer: %w", err)
	}
	return nil
}

// Consumers registers the calc.job handler on this service.
func (service *Service) Consumers(consumer *bus.Consumer) error {
	return Consumers(consumer, service)
}

func (service *Service) consumeCalcJob(ctx context.Context, event eventbus.Event) error {
	if event.Header.Type != "calc.job" {
		return fmt.Errorf("unsupported calc topic %q", event.Header.Type)
	}
	var payload calcJobPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode calc.job payload: %w", err)
	}
	if payload.JobID == "" || payload.TableID == "" {
		return errors.New("calc.job job_id and table_id are required")
	}
	job := calcJob{
		JobID:           payload.JobID,
		BaseID:          event.Header.BaseID,
		TableID:         payload.TableID,
		FieldIDList:     append([]string(nil), payload.FieldIDList...),
		TriggerRecordID: payload.Trigger.RecordID,
		TriggerFieldID:  payload.Trigger.FieldID,
	}
	return service.processCalcJob(ctx, job)
}
