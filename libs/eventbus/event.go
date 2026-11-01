// Package eventbus defines the transport-neutral event contract used by all services.
package eventbus

import (
	"encoding/json"
	"errors"
	"fmt"
)

// EventHeader is the common header shared by every service event.
type EventHeader struct {
	EventID    string `json:"event_id"`
	Type       string `json:"type"`
	Version    int32  `json:"version"`
	BaseID     string `json:"base_id,omitempty"`
	Seq        int64  `json:"seq,omitempty"`
	OccurredAt int64  `json:"occurred_at"`
	Producer   string `json:"producer"`
}

// Event is the common envelope carrying an EventHeader and a JSON payload.
type Event struct {
	Header  EventHeader     `json:"header"`
	Payload json.RawMessage `json:"payload"`
}

// Validate rejects events that cannot be safely written to an outbox or bus.
func (e Event) Validate() error {
	if e.Header.EventID == "" {
		return errors.New("event_id is required")
	}
	if e.Header.Type == "" {
		return errors.New("type is required")
	}
	if e.Header.Version <= 0 {
		return errors.New("version must be positive")
	}
	if e.Header.OccurredAt <= 0 {
		return errors.New("occurred_at is required")
	}
	if e.Header.Producer == "" {
		return errors.New("producer is required")
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return errors.New("payload must be valid JSON")
	}
	return nil
}

// Clone returns an event that does not share its mutable payload storage.
func (e Event) Clone() Event {
	clone := e
	clone.Payload = append(json.RawMessage(nil), e.Payload...)
	return clone
}

func marshalEvent(event Event) ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("validate event: %w", err)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return payload, nil
}

func unmarshalEvent(payload []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return Event{}, fmt.Errorf("unmarshal event: %w", err)
	}
	if err := event.Validate(); err != nil {
		return Event{}, fmt.Errorf("validate event: %w", err)
	}
	return event, nil
}
