// gateway/delete_field.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	structurev1 "excel/api/structure/v1"
	"excel_web_api/deal"
)

// deleteField 转发 H-39 删除字段：
//   - confirm=false / body 缺省：影响面预览通道，请求按 Confirm=false 透传 core，
//     响应 data 为 {impact:{record_count, dependent_formulas, dependent_lookups}}
//     （接口文档 §8.1.4 H-39 出参），不产生删除；
//   - confirm=true：执行删除，出参保持扁平 record_count 等字段（既有前端口径）。
func (routes *structureRoutes) deleteField(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	fieldID := strings.TrimSpace(request.PathValue("field_id"))
	if fieldID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	confirm, err := decodeDeleteConfirm(response, request)
	if err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DeleteField(
		request.Context(),
		&structurev1.DeleteFieldRequest{FieldId: fieldID, Confirm: confirm},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	if !confirm {
		writeDeleteImpact(response, result)
		return
	}
	writeProtoResult(response, result)
}

// decodeDeleteConfirm 解析 DELETE 请求体中的 confirm 开关。body 缺省/为空视为
// confirm=false（H-39 预览通道）；非法 JSON 或未知字段一律报错（1001）。
func decodeDeleteConfirm(
	w http.ResponseWriter,
	request *http.Request,
) (bool, error) {
	request.Body = http.MaxBytesReader(w, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := decoder.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false, errors.New("request body must contain one JSON object")
	}
	return body.Confirm, nil
}

// writeDeleteImpact 按接口文档 H-39 输出影响面信封 data.impact。
func writeDeleteImpact(
	response http.ResponseWriter,
	result *structurev1.DeleteFieldResponse,
) {
	writeSuccess(response, map[string]any{
		"impact": map[string]any{
			"record_count":       result.GetRecordCount(),
			"dependent_formulas": result.GetDependentFormulas(),
			"dependent_lookups":  result.GetDependentLookups(),
		},
	})
}
