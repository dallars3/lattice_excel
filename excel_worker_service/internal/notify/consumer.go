package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	"excel_worker_service/base"
	workerbus "excel_worker_service/bus"
)

// Consumers registers the worker-notify consumer group for every notification
// producing topic.
func Consumers(consumer *workerbus.Consumer, service *Service) error {
	if consumer == nil {
		return errors.New("notify consumer is required")
	}
	if service == nil {
		return errors.New("notify service is required")
	}
	for _, topic := range notifyTopics {
		topic := topic
		if err := consumer.Register(eventbus.SubscribeOptions{
			Topic:    topic,
			Mode:     eventbus.DeliveryModeReliable,
			Group:    notifyConsumerName,
			Consumer: notifyConsumerName,
		}, service.consume); err != nil {
			return fmt.Errorf("register %s notify consumer: %w", topic, err)
		}
	}
	return nil
}

func (service *Service) consume(
	ctx context.Context,
	event eventbus.Event,
) error {
	if service == nil || service.repository == nil || service.counters == nil {
		return errors.New("notify service dependencies are required")
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", event.Header.Type, err)
	}
	baseID := payloadString(payload, "base_id")
	if baseID == "" {
		baseID = event.Header.BaseID
	}
	recordID := payloadString(payload, "record_id")
	commentID := payloadString(payload, "comment_id")

	var recipients []string
	switch event.Header.Type {
	case "comment.created":
		recipients = appendIfPresent(
			recipients,
			payloadString(payload, "parent_author_id"),
		)
	case "comment.mentioned":
		recipients = appendUnique(recipients, payloadStringList(payload, "mentioned_user_id_list")...)
	case "invite.created", "invite.canceled":
		username := payloadString(payload, "username")
		if username == "" {
			service.warnSkipped(event, "invite username is missing")
			return nil
		}
		userID, err := service.repository.UserIDByUsername(ctx, username)
		if errors.Is(err, base.ErrNotifyUserNotFound) {
			service.warnSkipped(event, "invite username has no user")
			return nil
		}
		if err != nil {
			return err
		}
		recipients = append(recipients, userID)
	case "member.disabled", "member.enabled", "share.changed":
		recipients = appendIfPresent(recipients, payloadString(payload, "user_id"))
	case "enterprise.owner_transferred":
		recipients = appendUnique(
			recipients,
			payloadString(payload, "from_user_id"),
			payloadString(payload, "to_user_id"),
		)
	case "link.closed":
		creatorID := payloadString(payload, "creator_id")
		if creatorID == "" {
			creatorID = payloadString(payload, "operator")
		}
		recipients = appendIfPresent(recipients, creatorID)
	case "import.finished":
		recipients = appendIfPresent(
			recipients,
			firstPayloadString(payload, "creator_id", "created_by", "initiator_id", "operator", "user_id"),
		)
	default:
		return fmt.Errorf("unsupported notify topic %q", event.Header.Type)
	}
	if len(recipients) == 0 {
		if event.Header.Type == "comment.created" {
			return nil
		}
		service.warnSkipped(event, "event has no notification recipient")
		return nil
	}
	for _, userID := range recipients {
		notificationID, err := idgen.New()
		if err != nil {
			return fmt.Errorf("generate notification id: %w", err)
		}
		notification := base.Notification{
			ID:         notificationID,
			UserID:     userID,
			Type:       event.Header.Type,
			BaseID:     baseID,
			RecordID:   recordID,
			CommentID:  commentID,
			PayloadMap: append([]byte(nil), event.Payload...),
			CreatedAt:  service.now().UTC().UnixMilli(),
		}
		if err := service.repository.Create(ctx, notification); err != nil {
			return err
		}
		if err := service.counters.Increment(ctx, userID); err != nil {
			_ = service.repository.Delete(ctx, notification.ID)
			return err
		}
		if err := service.publishCreated(ctx, notification); err != nil {
			_ = service.repository.Delete(ctx, notification.ID)
			_ = service.counters.Decrement(ctx, userID)
			return err
		}
	}
	return nil
}

func (service *Service) warnSkipped(event eventbus.Event, reason string) {
	service.logger.Warn(
		"notification event skipped",
		"topic", event.Header.Type,
		"event_id", event.Header.EventID,
		"reason", reason,
	)
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func firstPayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := payloadString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func payloadStringList(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if stringsValue, ok := raw.([]string); ok {
			return stringsValue
		}
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			values = append(values, strings.TrimSpace(value))
		}
	}
	return values
}

func appendIfPresent(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	return append(values, value)
}

func appendUnique(values []string, candidates ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(candidates))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		values = append(values, candidate)
	}
	return values
}
