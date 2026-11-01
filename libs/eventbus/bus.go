package eventbus

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DeliveryMode selects the Redis delivery semantics.
type DeliveryMode string

const (
	// DeliveryModeReliable uses a Streams consumer group and requires ack.
	DeliveryModeReliable DeliveryMode = "streams"
	// DeliveryModePubSub uses transient Redis PubSub delivery.
	DeliveryModePubSub DeliveryMode = "pubsub"
)

// SubscribeOptions describes a protocol-neutral subscription.
type SubscribeOptions struct {
	Topic          string
	Mode           DeliveryMode
	Group          string
	Consumer       string
	Count          int64
	Block          time.Duration
	PendingMinIdle time.Duration
	QueueSize      int
}

// Delivery is one event delivery returned by a subscription.
type Delivery struct {
	Event    Event
	RecordID string
}

// Subscription exposes transport delivery and acknowledgement without exposing
// Redis or Kafka-specific APIs to business code.
type Subscription interface {
	Receive(context.Context) (Delivery, error)
	Ack(context.Context, Delivery) error
	Close() error
}

// EventBus is the shared publish/subscribe interface. Redis and Kafka adapters
// implement this interface while consuming business code remains unchanged.
type EventBus interface {
	Publish(context.Context, Event) error
	Subscribe(context.Context, SubscribeOptions) (Subscription, error)
}

// RetryPolicy defines bounded exponential backoff for consumer attempts.
type RetryPolicy struct {
	Initial     time.Duration
	Max         time.Duration
	MaxAttempts int
}

// DefaultRetryPolicy returns the service default 1s, 2s, 4s... cap at 1 minute.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Initial:     time.Second,
		Max:         time.Minute,
		MaxAttempts: 5,
	}
}

// Validate checks that a retry policy can safely terminate.
func (p RetryPolicy) Validate() error {
	if p.Initial <= 0 {
		return errors.New("retry initial delay must be positive")
	}
	if p.Max < p.Initial {
		return errors.New("retry max delay must be greater than or equal to initial delay")
	}
	if p.MaxAttempts <= 0 {
		return errors.New("retry max attempts must be positive")
	}
	return nil
}

// Delay returns the exponential delay after the given failed attempt.
func (p RetryPolicy) Delay(attempt int) time.Duration {
	if attempt <= 1 {
		return p.Initial
	}
	delay := p.Initial
	for current := 1; current < attempt; current++ {
		if delay >= p.Max/2 {
			return p.Max
		}
		delay *= 2
	}
	if delay > p.Max {
		return p.Max
	}
	return delay
}

func validateSubscribeOptions(options SubscribeOptions) error {
	if options.Topic == "" {
		return errors.New("topic is required")
	}
	if options.Mode == "" {
		options.Mode = DeliveryModeReliable
	}
	switch options.Mode {
	case DeliveryModeReliable:
		if options.Group == "" {
			return errors.New("consumer group is required for reliable delivery")
		}
		if options.Consumer == "" {
			return errors.New("consumer name is required for reliable delivery")
		}
	case DeliveryModePubSub:
	default:
		return fmt.Errorf("unsupported delivery mode %q", options.Mode)
	}
	if options.Count < 0 {
		return errors.New("count cannot be negative")
	}
	if options.Block < 0 {
		return errors.New("block cannot be negative")
	}
	if options.PendingMinIdle < 0 {
		return errors.New("pending min idle cannot be negative")
	}
	if options.QueueSize < 0 {
		return errors.New("queue size cannot be negative")
	}
	return nil
}
