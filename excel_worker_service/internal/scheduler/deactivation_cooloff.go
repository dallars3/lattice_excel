// scheduler/deactivation_cooloff.go：一文件一接口/任务。细则见《excel_worker_service-技术方案》。
package scheduler

import (
	"context"
	"encoding/json"
	"errors"

	iamv1 "excel/api/iam/v1"
	worker_rpc "excel_worker_service/rpc"
)

// DeactivationCooloff executes one due account cancellation through core IAM.
type DeactivationCooloff struct {
	IAM worker_rpc.IAMRPC
}

// Handle executes a task payload containing user_id.
func (handler *DeactivationCooloff) Handle(ctx context.Context, payload []byte) error {
	if handler == nil || handler.IAM == nil {
		return errors.New("IAM RPC client is required")
	}
	var request struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	if request.UserID == "" {
		return errors.New("deactivation user_id is required")
	}
	_, err := handler.IAM.ExecuteDeactivation(
		ctx,
		&iamv1.ExecuteDeactivationRequest{UserId: request.UserID},
	)
	return err
}
