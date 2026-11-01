// audit/consume.go maps EventBus topics into the append-only audit entry path.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/eventbus"
	auditv1 "excel/api/audit/v1"
	"excel_core_service/bus"
)

var auditTopicActions = map[string]auditv1.AuditAction{
	"record.deleted":               auditv1.AuditAction_AUDIT_ACTION_RECORD_DELETED,
	"member.disabled":              auditv1.AuditAction_AUDIT_ACTION_MEMBER_DISABLED,
	"member.enabled":               auditv1.AuditAction_AUDIT_ACTION_MEMBER_ENABLED,
	"link.closed":                  auditv1.AuditAction_AUDIT_ACTION_LINK_FORCE_CLOSED,
	"export.audited":               auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT,
	"import.finished":              auditv1.AuditAction_AUDIT_ACTION_DATA_IMPORT,
	"enterprise.owner_transferred": auditv1.AuditAction_AUDIT_ACTION_OWNER_TRANSFERRED,
	"audit.append":                 auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED,
}

// Consumers registers all audit event handlers on the core consumer.
func Consumers(consumer *bus.Consumer, service *Service) error {
	if consumer == nil {
		return errors.New("audit consumer is required")
	}
	if service == nil {
		return errors.New("audit service is required")
	}
	for topic := range auditTopicActions {
		topic := topic
		if err := consumer.Register(eventbus.SubscribeOptions{
			Topic:    topic,
			Mode:     eventbus.DeliveryModeReliable,
			Group:    AuditConsumerGroup,
			Consumer: AuditConsumerGroup,
		}, func(ctx context.Context, event eventbus.Event) error {
			return service.consumeEvent(ctx, topic, event)
		}); err != nil {
			return fmt.Errorf("register audit consumer for %s: %w", topic, err)
		}
	}
	return nil
}

// Consumers registers all audit event handlers on this service.
func (service *Service) Consumers(consumer *bus.Consumer) error {
	return Consumers(consumer, service)
}

func (service *Service) consumeEvent(
	ctx context.Context,
	topic string,
	event eventbus.Event,
) error {
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", topic, err)
	}
	detail, err := structpb.NewStruct(payload)
	if err != nil {
		return fmt.Errorf("convert %s payload to detail: %w", topic, err)
	}

	action, ok := auditTopicActions[topic]
	if !ok {
		return fmt.Errorf("unsupported audit topic %q", topic)
	}
	operator := stringField(payload, "operator")
	target := stringField(payload, "target")
	switch topic {
	case "record.deleted":
		target = recordTarget(payload["record_id_list"])
	case "member.disabled", "member.enabled":
		target = stringField(payload, "user_id")
	case "link.closed":
		target = firstNonEmpty(stringField(payload, "link_id"), stringField(payload, "base_id"))
	case "export.audited":
		target = firstNonEmpty(target, stringField(payload, "base_id"))
	case "import.finished":
		target = stringField(payload, "task_id")
	case "enterprise.owner_transferred":
		operator = stringField(payload, "from_user_id")
		target = stringField(payload, "to_user_id")
	case "audit.append":
		action, err = parseAuditAction(payload["action"])
		if err != nil {
			return fmt.Errorf("decode audit.append action: %w", err)
		}
		if nested, ok := payload["detail_map"].(map[string]any); ok {
			detail, err = structpb.NewStruct(nested)
			if err != nil {
				return fmt.Errorf("convert audit.append detail_map: %w", err)
			}
		}
	}

	request := &auditv1.AppendRequest{
		EventId:   event.Header.EventID,
		Action:    action,
		Operator:  operator,
		Target:    target,
		BaseId:    firstNonEmpty(stringField(payload, "base_id"), event.Header.BaseID),
		TableId:   stringField(payload, "table_id"),
		DetailMap: detail,
	}
	enterpriseID := stringField(payload, "enterprise_id")
	if enterpriseID == "" {
		if nested, ok := payload["detail_map"].(map[string]any); ok {
			enterpriseID = stringField(nested, "enterprise_id")
		}
	}
	occurredAt := time.UnixMilli(event.Header.OccurredAt).UTC()
	return service.appendAudit(ctx, request, enterpriseID, occurredAt)
}

func parseAuditAction(value any) (auditv1.AuditAction, error) {
	var number int32
	switch typed := value.(type) {
	case string:
		if parsed, ok := auditv1.AuditAction_value[typed]; ok {
			number = parsed
			break
		}
		parsed, err := strconv.ParseInt(typed, 10, 32)
		if err != nil {
			return auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED, errors.New("unknown audit action")
		}
		number = int32(parsed)
	case float64:
		if float64(int32(typed)) != typed {
			return auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED, errors.New("invalid audit action")
		}
		number = int32(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED, errors.New("invalid audit action")
		}
		number = int32(parsed)
	default:
		return auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED, errors.New("audit action is required")
	}
	action := auditv1.AuditAction(number)
	if action == auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED ||
		action < auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT ||
		action > auditv1.AuditAction_AUDIT_ACTION_REGISTER_SWITCH_CHANGED {
		return auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED, errors.New("invalid audit action")
	}
	return action, nil
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func recordTarget(value any) string {
	values, ok := value.([]any)
	if !ok {
		return ""
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if id, ok := value.(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}
