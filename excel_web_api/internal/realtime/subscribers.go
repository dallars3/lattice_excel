// realtime/subscribers.go：EventBus 实时事件订阅与重连。
package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
)

const subscriberReconnectDelay = 100 * time.Millisecond

var realtimeTopics = []string{"record.changed", "record.deleted", "link.closed"}

type eventPayload struct {
	BaseID       string          `json:"base_id"`
	TableID      string          `json:"table_id"`
	RecordID     string          `json:"record_id"`
	RecordIDList []string        `json:"record_id_list"`
	FieldID      string          `json:"field_id"`
	Value        json.RawMessage `json:"value"`
	Version      int64           `json:"version"`
	Operator     string          `json:"operator"`
	ChangeType   string          `json:"change_type"`
}

func (hub *Hub) startSubscriptions(ctx context.Context) error {
	subscriptions := make([]eventbus.Subscription, 0, len(realtimeTopics))
	for _, topic := range realtimeTopics {
		subscription, err := hub.subscribe(ctx, topic)
		if err != nil {
			for _, opened := range subscriptions {
				_ = opened.Close()
			}
			return err
		}
		subscriptions = append(subscriptions, subscription)
	}
	for index, subscription := range subscriptions {
		hub.wait.Add(1)
		go hub.consume(ctx, realtimeTopics[index], subscription)
	}
	hub.wait.Add(1)
	go hub.supervise(ctx)
	return nil
}

func (hub *Hub) supervise(ctx context.Context) {
	defer hub.wait.Done()
	interval := hub.heartbeatTimeout / 10
	if interval < 5*time.Millisecond {
		interval = 5 * time.Millisecond
	}
	if interval > time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hub.closeExpiredClients()
		}
	}
}

func (hub *Hub) closeExpiredClients() {
	now := hub.now()
	hub.mu.Lock()
	expired := make([]*client, 0)
	for client := range hub.clients {
		if client.expired(now, hub.heartbeatTimeout) {
			expired = append(expired, client)
		}
	}
	hub.mu.Unlock()
	for _, client := range expired {
		client.close()
	}
}

func (hub *Hub) subscribe(
	ctx context.Context,
	topic string,
) (eventbus.Subscription, error) {
	return hub.bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: topic,
		Mode:  eventbus.DeliveryModePubSub,
	})
}

func (hub *Hub) consume(
	ctx context.Context,
	topic string,
	subscription eventbus.Subscription,
) {
	defer hub.wait.Done()
	current := subscription
	defer func() {
		if current != nil {
			_ = current.Close()
		}
	}()
	for {
		if current == nil {
			current = hub.resubscribe(ctx, topic)
			if current == nil {
				return
			}
		}
		delivery, err := current.Receive(ctx)
		if err == nil {
			hub.handleEvent(delivery.Event)
			_ = current.Ack(ctx, delivery)
			continue
		}
		_ = current.Close()
		current = nil
		if ctx.Err() != nil {
			return
		}
	}
}

func (hub *Hub) resubscribe(ctx context.Context, topic string) eventbus.Subscription {
	timer := time.NewTimer(subscriberReconnectDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		subscription, err := hub.subscribe(ctx, topic)
		if err == nil {
			return subscription
		}
		timer.Reset(subscriberReconnectDelay)
	}
}

func (hub *Hub) handleEvent(event eventbus.Event) {
	switch event.Header.Type {
	case "record.changed":
		hub.handleRecordChanged(event)
	case "record.deleted":
		hub.handleRecordDeleted(event)
	case "link.closed":
		hub.handleLinkClosed(event)
	}
}

func (hub *Hub) handleRecordChanged(event eventbus.Event) {
	payload, err := decodeEventPayload(event.Payload)
	if err != nil {
		return
	}
	baseID := firstNonEmpty(event.Header.BaseID, payload.BaseID)
	if baseID == "" {
		return
	}
	if isStructureChange(payload.ChangeType) {
		hub.broadcastBase(baseID, event.Header.Seq, payload.Operator, &webapiv1.Frame{
			Payload: &webapiv1.Frame_StructureChanged{
				StructureChanged: &webapiv1.StructureChanged{
					Seq:        event.Header.Seq,
					ChangeType: payload.ChangeType,
					TableId:    payload.TableID,
					FieldId:    payload.FieldID,
				},
			},
		})
		return
	}
	value, err := valueFromJSON(payload.Value)
	if err != nil {
		return
	}
	frame := &webapiv1.Frame{Payload: &webapiv1.Frame_CellChanged{
		CellChanged: &webapiv1.CellChanged{
			Seq:      event.Header.Seq,
			RecordId: payload.RecordID,
			FieldId:  payload.FieldID,
			Value:    value,
			Version:  payload.Version,
			Operator: payload.Operator,
		},
	}}
	hub.broadcastBase(baseID, event.Header.Seq, payload.Operator, frame)
}

func isStructureChange(changeType string) bool {
	normalized := strings.ToLower(changeType)
	return strings.Contains(normalized, "field") ||
		strings.Contains(normalized, "table") ||
		strings.Contains(normalized, "view") ||
		strings.Contains(normalized, "structure")
}

func (hub *Hub) handleRecordDeleted(event eventbus.Event) {
	payload, err := decodeEventPayload(event.Payload)
	if err != nil {
		return
	}
	baseID := firstNonEmpty(event.Header.BaseID, payload.BaseID)
	if baseID == "" {
		return
	}
	frames := make([]*webapiv1.Frame, 0, len(payload.RecordIDList))
	for _, recordID := range payload.RecordIDList {
		if recordID == "" {
			continue
		}
		frames = append(frames, &webapiv1.Frame{
			Payload: &webapiv1.Frame_RecordDeleted{
				RecordDeleted: &webapiv1.RecordDeleted{
					Seq:      event.Header.Seq,
					RecordId: recordID,
				},
			},
		})
	}
	hub.broadcastBaseFrames(baseID, event.Header.Seq, "", frames)
}

func (hub *Hub) handleLinkClosed(event eventbus.Event) {
	payload, err := decodeEventPayload(event.Payload)
	if err != nil {
		return
	}
	baseID := firstNonEmpty(event.Header.BaseID, payload.BaseID)
	if baseID == "" {
		return
	}
	hub.broadcastBase(baseID, 0, "", &webapiv1.Frame{
		Payload: &webapiv1.Frame_SessionRevoked{
			SessionRevoked: &webapiv1.SessionRevoked{Reason: "link_closed"},
		},
	})
}

func (hub *Hub) broadcastBase(
	baseID string,
	seq int64,
	source string,
	frame *webapiv1.Frame,
) {
	hub.broadcastBaseFrames(baseID, seq, source, []*webapiv1.Frame{frame})
}

func (hub *Hub) broadcastBaseFrames(
	baseID string,
	seq int64,
	source string,
	frames []*webapiv1.Frame,
) {
	if len(frames) == 0 {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if seq > hub.baseSeq[baseID] {
		hub.baseSeq[baseID] = seq
	}
	for key, currentRoom := range hub.rooms {
		if key.baseID != baseID {
			continue
		}
		for client := range currentRoom.clients {
			if source != "" && client.principal.GetUserId() == source {
				continue
			}
			if seq > 0 && seq <= client.lastSeq {
				continue
			}
			for _, frame := range frames {
				client.enqueue(frame)
			}
			if seq > client.lastSeq {
				client.lastSeq = seq
			}
		}
	}
}

func decodeEventPayload(payload []byte) (eventPayload, error) {
	var decoded eventPayload
	if len(payload) == 0 {
		return decoded, errors.New("event payload is empty")
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return eventPayload{}, err
	}
	return decoded, nil
}

func valueFromJSON(raw json.RawMessage) (*commonv1.Value, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return valueToProto(value), nil
}

func valueToProto(value any) *commonv1.Value {
	switch typed := value.(type) {
	case string:
		return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: typed}}
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: integer}}
		}
		if number, err := typed.Float64(); err == nil {
			return &commonv1.Value{Kind: &commonv1.Value_DoubleValue{DoubleValue: number}}
		}
	case bool:
		return &commonv1.Value{Kind: &commonv1.Value_BoolValue{BoolValue: typed}}
	case []any:
		items := make([]*commonv1.Value, 0, len(typed))
		for _, item := range typed {
			if converted := valueToProto(item); converted != nil {
				items = append(items, converted)
			}
		}
		return &commonv1.Value{Kind: &commonv1.Value_ListValue{
			ListValue: &commonv1.ValueList{ValueList: items},
		}}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
