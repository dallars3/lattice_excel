// tracking/consume.go maps the track.reported bus topic into the tracking sink.
package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"excel-libs/eventbus"

	"excel_core_service/base"
	"excel_core_service/bus"
)

// trackingReportedPayload 必须与网关 tracking_routes.go 发布的 track.reported
// payload 对齐（event_id/event_name/detail/client_at_ms/user_id/user_type/received_at_ms）。
// client_at_ms 由网关以 omitempty 发布：缺省时为 0。
type trackingReportedPayload struct {
	EventID      string          `json:"event_id"`
	EventName    string          `json:"event_name"`
	Detail       json.RawMessage `json:"detail"`
	ClientAtMs   int64           `json:"client_at_ms"`
	UserID       string          `json:"user_id"`
	UserType     string          `json:"user_type"`
	ReceivedAtMs int64           `json:"received_at_ms"`
}

// Consumers registers the track.reported handler on the core consumer.
func Consumers(consumer *bus.Consumer, service *Service) error {
	if consumer == nil {
		return errors.New("tracking consumer is required")
	}
	if service == nil {
		return errors.New("tracking service is required")
	}
	if err := consumer.Register(eventbus.SubscribeOptions{
		Topic:    "track.reported",
		Mode:     eventbus.DeliveryModeReliable,
		Group:    TrackingConsumerGroup,
		Consumer: TrackingConsumerGroup,
	}, func(ctx context.Context, event eventbus.Event) error {
		return service.consumeReportingEvent(ctx, event)
	}); err != nil {
		return fmt.Errorf("register tracking consumer for track.reported: %w", err)
	}
	return nil
}

// Consumers registers this service's handlers on the core consumer.
func (service *Service) Consumers(consumer *bus.Consumer) error {
	return Consumers(consumer, service)
}

func (service *Service) consumeReportingEvent(
	ctx context.Context,
	event eventbus.Event,
) error {
	if event.Header.Type != "track.reported" {
		return fmt.Errorf("unsupported tracking topic %q", event.Header.Type)
	}
	var payload trackingReportedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode track.reported payload: %w", err)
	}
	if payload.EventID == "" || payload.EventName == "" || payload.UserID == "" {
		return errors.New("track.reported payload missing required fields")
	}
	if service == nil || service.repository == nil {
		return errors.New("tracking repository is required")
	}
	err := service.repository.Insert(ctx, base.TrackingEventRecord{
		EventID:      payload.EventID,
		UserID:       payload.UserID,
		UserType:     payload.UserType,
		EventName:    payload.EventName,
		Detail:       payload.Detail,
		ClientAtMs:   payload.ClientAtMs,
		ReceivedAtMs: payload.ReceivedAtMs,
	})
	if err == nil || errors.Is(err, base.ErrTrackingEventExists) {
		return nil
	}
	return err
}