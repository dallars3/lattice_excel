// scheduler/link_ttl_guard.go：一文件一接口/任务。细则见《excel_worker_service-技术方案》。
package scheduler

import (
	"context"
	"errors"
)

// LinkTTLCloser closes expired link sessions through the configured seam.
type LinkTTLCloser interface {
	CloseExpiredLinks(context.Context) error
}

// LinkTTLGuardHandler adapts the TTL fallback seam to scheduler.Handler.
type LinkTTLGuardHandler struct {
	Closer LinkTTLCloser
}

// Handle executes one link-TTL fallback pass.
func (handler LinkTTLGuardHandler) Handle(ctx context.Context, _ []byte) error {
	if handler.Closer == nil {
		return errors.New("link TTL closer is required")
	}
	return handler.Closer.CloseExpiredLinks(ctx)
}
