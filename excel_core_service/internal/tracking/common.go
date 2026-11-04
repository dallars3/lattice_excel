// tracking/common.go：模块内可复用逻辑归口。
package tracking

import (
	"excel_core_service/base"
)

const (
	// TrackingConsumerGroup is the durable EventBus consumer group for tracking.
	TrackingConsumerGroup = "core-tracking"
)

// Service sinks track.reported events into the tracking table. It exposes no
// RPC surface（埋点仅入库，不对客）。
type Service struct {
	repository *base.TrackingRepository
}

// NewService constructs the tracking sink service.
func NewService(repository *base.TrackingRepository) *Service {
	return &Service{repository: repository}
}