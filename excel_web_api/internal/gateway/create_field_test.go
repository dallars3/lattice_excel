package gateway

import (
	"net/http"
	"testing"
)

// TestCreateFieldForwardsDescription：H-35 描述写入通道（缺陷 #24）。网关解码
// body 中的 description 并转发 core；未带 description 时转发空值。
func TestCreateFieldForwardsDescription(t *testing.T) {
	stub := newStructureStub()
	pipeline := newStructurePipeline(t, stub)

	response := structureRouteRequest(
		pipeline,
		http.MethodPost,
		"/api/v1/tables/table_1/fields",
		`{"name":"F","type":"text","config_map":{},"description":"用于悬停展示的字段说明"}`,
		true,
	)
	statusCode, code, _ := decodeWorkspaceEnvelope(t, response)
	if statusCode != http.StatusOK || code != 0 {
		t.Fatalf("create field with description = HTTP %d code %d, want success", statusCode, code)
	}
	if got := stub.lastCreateField.GetDescription(); got != "用于悬停展示的字段说明" {
		t.Fatalf("CreateField description = %q, want %q", got, "用于悬停展示的字段说明")
	}

	stub.lastCreateField = nil
	response = structureRouteRequest(
		pipeline,
		http.MethodPost,
		"/api/v1/tables/table_1/fields",
		`{"name":"G","type":"text"}`,
		true,
	)
	statusCode, code, _ = decodeWorkspaceEnvelope(t, response)
	if statusCode != http.StatusOK || code != 0 {
		t.Fatalf("create field without description = HTTP %d code %d, want success", statusCode, code)
	}
	if got := stub.lastCreateField.GetDescription(); got != "" {
		t.Fatalf("CreateField description without body field = %q, want empty", got)
	}
}
