// realtime/common.go：房间/推送可复用逻辑归口。
package realtime

import (
	"context"
	"hash/fnv"
	"time"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	webapiv1 "excel/api/webapi/v1"
)

const (
	// DefaultHeartbeatTimeout is the read timeout used to retire silent members.
	DefaultHeartbeatTimeout = 30 * time.Second
	// DefaultConnectionLimit is the per-process WebSocket connection capacity.
	DefaultConnectionLimit = 500
	// DefaultFlushWindow coalesces room writes into a bounded fan-out window.
	DefaultFlushWindow = 50 * time.Millisecond
	// DefaultMaxBatch limits frames emitted by one coalesced flush.
	DefaultMaxBatch = 15
	// DefaultClientHeartbeatInterval documents the client keepalive cadence.
	DefaultClientHeartbeatInterval = 10 * time.Second
)

var defaultPalette = []string{
	"blue",
	"green",
	"orange",
	"purple",
	"red",
	"cyan",
	"yellow",
	"pink",
}

// SessionResolver is the IAM seam shared with rpc.IamRPC.
type SessionResolver interface {
	ExchangeSession(
		context.Context,
		*iamv1.ExchangeSessionRequest,
	) (*iamv1.ExchangeSessionResponse, error)
}

// EventStore supplies durable WebSocket events after a sequence watermark.
// Adapters convert records.GetEventsSince responses into WebSocket frames.
type EventStore interface {
	Since(
		context.Context,
		string,
		string,
		int64,
		int,
	) ([]*webapiv1.Frame, error)
}

// HubDeps contains the external seams and bounded runtime settings for a Hub.
type HubDeps struct {
	Session SessionResolver
	Events  EventStore
	Bus     eventbus.EventBus
	Now     func() time.Time

	HeartbeatTimeout        time.Duration
	ConnectionLimit         int
	FlushWindow             time.Duration
	MaxBatch                int
	ClientHeartbeatInterval time.Duration
	Colors                  []string
}

type principalMember struct {
	userID   string
	color    string
	internal bool
}

func memberFromPrincipal(principal *commonv1.Principal, palette []string) principalMember {
	return principalMember{
		userID:   principal.GetUserId(),
		color:    memberColor(principal.GetUserId(), palette),
		internal: principal.GetInternal(),
	}
}

func memberColor(userID string, palette []string) string {
	if len(palette) == 0 {
		palette = defaultPalette
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(userID))
	return palette[int(hasher.Sum32())%len(palette)]
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func positiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func frameSequence(frame *webapiv1.Frame) int64 {
	switch payload := frame.GetPayload().(type) {
	case *webapiv1.Frame_CellChanged:
		return payload.CellChanged.GetSeq()
	case *webapiv1.Frame_RecordDeleted:
		return payload.RecordDeleted.GetSeq()
	case *webapiv1.Frame_StructureChanged:
		return payload.StructureChanged.GetSeq()
	default:
		return 0
	}
}
