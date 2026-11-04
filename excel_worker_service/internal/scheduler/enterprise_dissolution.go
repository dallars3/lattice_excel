// scheduler/enterprise_dissolution.go：一文件一接口/任务。细则见《excel_worker_service-技术方案》。
package scheduler

import (
	"context"
	"encoding/json"
	"errors"

	iamv1 "excel/api/iam/v1"
	worker_rpc "excel_worker_service/rpc"
)

// EnterpriseDissolution executes one due enterprise deletion through core IAM.
type EnterpriseDissolution struct {
	IAM worker_rpc.IAMRPC
}

// Handle executes a task payload containing enterprise_id.
func (handler *EnterpriseDissolution) Handle(ctx context.Context, payload []byte) error {
	if handler == nil || handler.IAM == nil {
		return errors.New("IAM RPC client is required")
	}
	var request struct {
		EnterpriseID string `json:"enterprise_id"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	if request.EnterpriseID == "" {
		return errors.New("enterprise dissolution enterprise_id is required")
	}
	_, err := handler.IAM.ExecuteEnterpriseDissolution(
		ctx,
		&iamv1.ExecuteEnterpriseDissolutionRequest{EnterpriseId: request.EnterpriseID},
	)
	return err
}
