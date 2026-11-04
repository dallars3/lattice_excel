package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	structurev1 "excel/api/structure/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type structureStub struct {
	listTablesResponse     *structurev1.ListTablesResponse
	listTablesErr          error
	listTablesCalls        int
	createTableResponse    *structurev1.CreateTableResponse
	renameTableResponse    *structurev1.RenameTableResponse
	duplicateTableResponse *structurev1.DuplicateTableResponse
	listFieldsResponse     *structurev1.ListFieldsResponse
	createFieldResponse    *structurev1.CreateFieldResponse
	renameFieldResponse    *structurev1.RenameFieldResponse
	updateConfigResponse   *structurev1.UpdateFieldConfigResponse
	convertFieldResponse   *structurev1.ConvertFieldResponse
	deleteFieldResponse    *structurev1.DeleteFieldResponse
	updateOptionsResponse  *structurev1.UpdateOptionsResponse
	duplicateFieldResponse *structurev1.DuplicateFieldResponse
	listViewsResponse      *structurev1.ListViewsResponse
	createViewResponse     *structurev1.CreateViewResponse
	renameViewResponse     *structurev1.RenameViewResponse
	duplicateViewResponse  *structurev1.DuplicateViewResponse
	getViewConfigResponse  *structurev1.GetViewConfigResponse
	updateViewConfigResp   *structurev1.UpdateViewConfigResponse

	lastCreateTable      *structurev1.CreateTableRequest
	lastRenameTable      *structurev1.RenameTableRequest
	lastDuplicateTable   *structurev1.DuplicateTableRequest
	lastDeleteTable      *structurev1.DeleteTableRequest
	lastListFields       *structurev1.ListFieldsRequest
	lastCreateField      *structurev1.CreateFieldRequest
	lastRenameField      *structurev1.RenameFieldRequest
	lastUpdateConfig     *structurev1.UpdateFieldConfigRequest
	lastConvertField     *structurev1.ConvertFieldRequest
	lastDeleteField      *structurev1.DeleteFieldRequest
	lastUpdateOptions    *structurev1.UpdateOptionsRequest
	lastDuplicateField   *structurev1.DuplicateFieldRequest
	lastListViews        *structurev1.ListViewsRequest
	lastCreateView       *structurev1.CreateViewRequest
	lastRenameView       *structurev1.RenameViewRequest
	lastDuplicateView    *structurev1.DuplicateViewRequest
	lastDeleteView       *structurev1.DeleteViewRequest
	lastGetViewConfig    *structurev1.GetViewConfigRequest
	lastUpdateViewConfig *structurev1.UpdateViewConfigRequest
}

func (stub *structureStub) ListTables(
	_ context.Context,
	request *structurev1.ListTablesRequest,
) (*structurev1.ListTablesResponse, error) {
	stub.listTablesCalls++
	return stub.listTablesResponse, stub.listTablesErr
}

func (stub *structureStub) CreateTable(
	_ context.Context,
	request *structurev1.CreateTableRequest,
) (*structurev1.CreateTableResponse, error) {
	stub.lastCreateTable = request
	return stub.createTableResponse, nil
}

func (stub *structureStub) RenameTable(
	_ context.Context,
	request *structurev1.RenameTableRequest,
) (*structurev1.RenameTableResponse, error) {
	stub.lastRenameTable = request
	return stub.renameTableResponse, nil
}

func (stub *structureStub) DuplicateTable(
	_ context.Context,
	request *structurev1.DuplicateTableRequest,
) (*structurev1.DuplicateTableResponse, error) {
	stub.lastDuplicateTable = request
	return stub.duplicateTableResponse, nil
}

func (stub *structureStub) DeleteTable(
	_ context.Context,
	request *structurev1.DeleteTableRequest,
) (*emptypb.Empty, error) {
	stub.lastDeleteTable = request
	return &emptypb.Empty{}, nil
}

func (stub *structureStub) ListFields(
	_ context.Context,
	request *structurev1.ListFieldsRequest,
) (*structurev1.ListFieldsResponse, error) {
	stub.lastListFields = request
	return stub.listFieldsResponse, nil
}

func (stub *structureStub) CreateField(
	_ context.Context,
	request *structurev1.CreateFieldRequest,
) (*structurev1.CreateFieldResponse, error) {
	stub.lastCreateField = request
	return stub.createFieldResponse, nil
}

func (stub *structureStub) RenameField(
	_ context.Context,
	request *structurev1.RenameFieldRequest,
) (*structurev1.RenameFieldResponse, error) {
	stub.lastRenameField = request
	return stub.renameFieldResponse, nil
}

func (stub *structureStub) UpdateFieldConfig(
	_ context.Context,
	request *structurev1.UpdateFieldConfigRequest,
) (*structurev1.UpdateFieldConfigResponse, error) {
	stub.lastUpdateConfig = request
	return stub.updateConfigResponse, nil
}

func (stub *structureStub) ConvertField(
	_ context.Context,
	request *structurev1.ConvertFieldRequest,
) (*structurev1.ConvertFieldResponse, error) {
	stub.lastConvertField = request
	return stub.convertFieldResponse, nil
}

func (stub *structureStub) DeleteField(
	_ context.Context,
	request *structurev1.DeleteFieldRequest,
) (*structurev1.DeleteFieldResponse, error) {
	stub.lastDeleteField = request
	return stub.deleteFieldResponse, nil
}

func (stub *structureStub) UpdateOptions(
	_ context.Context,
	request *structurev1.UpdateOptionsRequest,
) (*structurev1.UpdateOptionsResponse, error) {
	stub.lastUpdateOptions = request
	return stub.updateOptionsResponse, nil
}

func (stub *structureStub) DuplicateField(
	_ context.Context,
	request *structurev1.DuplicateFieldRequest,
) (*structurev1.DuplicateFieldResponse, error) {
	stub.lastDuplicateField = request
	return stub.duplicateFieldResponse, nil
}

func (stub *structureStub) ListViews(
	_ context.Context,
	request *structurev1.ListViewsRequest,
) (*structurev1.ListViewsResponse, error) {
	stub.lastListViews = request
	return stub.listViewsResponse, nil
}

func (stub *structureStub) CreateView(
	_ context.Context,
	request *structurev1.CreateViewRequest,
) (*structurev1.CreateViewResponse, error) {
	stub.lastCreateView = request
	return stub.createViewResponse, nil
}

func (stub *structureStub) RenameView(
	_ context.Context,
	request *structurev1.RenameViewRequest,
) (*structurev1.RenameViewResponse, error) {
	stub.lastRenameView = request
	return stub.renameViewResponse, nil
}

func (stub *structureStub) DuplicateView(
	_ context.Context,
	request *structurev1.DuplicateViewRequest,
) (*structurev1.DuplicateViewResponse, error) {
	stub.lastDuplicateView = request
	return stub.duplicateViewResponse, nil
}

func (stub *structureStub) DeleteView(
	_ context.Context,
	request *structurev1.DeleteViewRequest,
) (*emptypb.Empty, error) {
	stub.lastDeleteView = request
	return &emptypb.Empty{}, nil
}

func (stub *structureStub) GetViewConfig(
	_ context.Context,
	request *structurev1.GetViewConfigRequest,
) (*structurev1.GetViewConfigResponse, error) {
	stub.lastGetViewConfig = request
	return stub.getViewConfigResponse, nil
}

func (stub *structureStub) UpdateViewConfig(
	_ context.Context,
	request *structurev1.UpdateViewConfigRequest,
) (*structurev1.UpdateViewConfigResponse, error) {
	stub.lastUpdateViewConfig = request
	return stub.updateViewConfigResp, nil
}

var _ rpc.StructureRPC = (*structureStub)(nil)

func newStructureStub() *structureStub {
	return &structureStub{
		listTablesResponse:     &structurev1.ListTablesResponse{TableList: []*structurev1.Table{{Id: "table_1"}}},
		createTableResponse:    &structurev1.CreateTableResponse{Table: &structurev1.Table{Id: "table_2"}},
		renameTableResponse:    &structurev1.RenameTableResponse{Table: &structurev1.Table{Id: "table_1"}},
		duplicateTableResponse: &structurev1.DuplicateTableResponse{Table: &structurev1.Table{Id: "table_copy"}},
		listFieldsResponse:     &structurev1.ListFieldsResponse{FieldList: []*structurev1.Field{{Id: "field_1"}}},
		createFieldResponse:    &structurev1.CreateFieldResponse{Field: &structurev1.Field{Id: "field_2"}},
		renameFieldResponse:    &structurev1.RenameFieldResponse{Field: &structurev1.Field{Id: "field_1"}},
		updateConfigResponse:   &structurev1.UpdateFieldConfigResponse{Field: &structurev1.Field{Id: "field_1"}},
		convertFieldResponse:   &structurev1.ConvertFieldResponse{Field: &structurev1.Field{Id: "field_1"}},
		deleteFieldResponse:    &structurev1.DeleteFieldResponse{RecordCount: 1},
		updateOptionsResponse:  &structurev1.UpdateOptionsResponse{Field: &structurev1.Field{Id: "field_1"}},
		duplicateFieldResponse: &structurev1.DuplicateFieldResponse{Field: &structurev1.Field{Id: "field_copy"}},
		listViewsResponse:      &structurev1.ListViewsResponse{ViewList: []*structurev1.View{{Id: "view_1"}}},
		createViewResponse:     &structurev1.CreateViewResponse{View: &structurev1.View{Id: "view_2"}},
		renameViewResponse:     &structurev1.RenameViewResponse{View: &structurev1.View{Id: "view_1"}},
		duplicateViewResponse:  &structurev1.DuplicateViewResponse{View: &structurev1.View{Id: "view_copy"}},
		getViewConfigResponse:  &structurev1.GetViewConfigResponse{ViewConfig: &structurev1.ViewConfig{}},
		updateViewConfigResp:   &structurev1.UpdateViewConfigResponse{ViewConfig: &structurev1.ViewConfig{HiddenFieldList: []string{"f1"}}},
	}
}

func newStructurePipeline(t *testing.T, stub *structureStub) *Pipeline {
	t.Helper()
	sessionIAM := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	pipeline := NewPipeline(sessionIAM, share, nil, nil)
	RegisterStructureRoutes(pipeline, stub)
	return pipeline
}

func structureRouteRequest(
	pipeline *Pipeline,
	method string,
	path string,
	body string,
	withCookie bool,
) *httptest.ResponseRecorder {
	reader := strings.NewReader(body)
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if withCookie {
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	}
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)
	return response
}

func TestStructureRoutesRejectMissingSession(t *testing.T) {
	pipeline := newStructurePipeline(t, newStructureStub())
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/bases/base_1/tables"},
		{http.MethodPost, "/api/v1/bases/base_1/tables"},
		{http.MethodPatch, "/api/v1/tables/table_1"},
		{http.MethodPost, "/api/v1/tables/table_1/duplicate"},
		{http.MethodDelete, "/api/v1/tables/table_1"},
		{http.MethodGet, "/api/v1/tables/table_1/fields"},
		{http.MethodPost, "/api/v1/tables/table_1/fields"},
		{http.MethodPatch, "/api/v1/fields/field_1"},
		{http.MethodPut, "/api/v1/fields/field_1/config"},
		{http.MethodPost, "/api/v1/fields/field_1/convert"},
		{http.MethodDelete, "/api/v1/fields/field_1"},
		{http.MethodPut, "/api/v1/fields/field_1/options"},
		{http.MethodPost, "/api/v1/fields/field_1/duplicate"},
		{http.MethodGet, "/api/v1/tables/table_1/views"},
		{http.MethodPost, "/api/v1/tables/table_1/views"},
		{http.MethodPatch, "/api/v1/views/view_1"},
		{http.MethodPost, "/api/v1/views/view_1/duplicate"},
		{http.MethodDelete, "/api/v1/views/view_1"},
		{http.MethodGet, "/api/v1/views/view_1/config"},
		{http.MethodPut, "/api/v1/views/view_1/config"},
	}
	for _, route := range routes {
		response := structureRouteRequest(pipeline, route.method, route.path, "", false)
		statusCode, code, _ := decodeWorkspaceEnvelope(t, response)
		if statusCode != http.StatusUnauthorized || code != deal.CodeUnauthenticated {
			t.Fatalf(
				"%s %s = HTTP %d code %d, want 401/2001",
				route.method,
				route.path,
				statusCode,
				code,
			)
		}
	}
}

func TestStructureRoutesForwardSuccessfully(t *testing.T) {
	stub := newStructureStub()
	pipeline := newStructurePipeline(t, stub)
	routes := []struct {
		method    string
		path      string
		body      string
		key       string
		id        string
		checkStub func(t *testing.T, stub *structureStub)
	}{
		{
			method: http.MethodGet, path: "/api/v1/bases/base_1/tables",
			key: "table_list", id: "table_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.listTablesCalls != 1 {
					t.Fatalf("ListTables calls = %d, want 1", stub.listTablesCalls)
				}
			},
		},
		{
			method: http.MethodPost, path: "/api/v1/bases/base_1/tables",
			body: `{"name":"T1"}`, key: "table", id: "table_2",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastCreateTable.GetBaseId() != "base_1" || stub.lastCreateTable.GetName() != "T1" {
					t.Fatalf("CreateTable request = %v", stub.lastCreateTable)
				}
			},
		},
		{
			method: http.MethodPatch, path: "/api/v1/tables/table_1",
			body: `{"name":"T2"}`, key: "table", id: "table_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastRenameTable.GetTableId() != "table_1" {
					t.Fatalf("RenameTable table_id = %q", stub.lastRenameTable.GetTableId())
				}
			},
		},
		{
			method: http.MethodPost, path: "/api/v1/tables/table_1/duplicate",
			key: "table", id: "table_copy",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastDuplicateTable.GetTableId() != "table_1" {
					t.Fatalf("DuplicateTable table_id = %q", stub.lastDuplicateTable.GetTableId())
				}
			},
		},
		{
			method: http.MethodDelete, path: "/api/v1/tables/table_1",
			body: `{"confirm":true}`,
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastDeleteTable.GetTableId() != "table_1" || !stub.lastDeleteTable.GetConfirm() {
					t.Fatalf("DeleteTable request = %v", stub.lastDeleteTable)
				}
			},
		},
		{
			method: http.MethodGet, path: "/api/v1/tables/table_1/fields",
			key: "field_list", id: "field_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastListFields.GetTableId() != "table_1" {
					t.Fatalf("ListFields table_id = %q", stub.lastListFields.GetTableId())
				}
			},
		},
		{
			method: http.MethodPost, path: "/api/v1/tables/table_1/fields",
			body: `{"name":"F","type":"text","config_map":{"precision":2}}`,
			key:  "field", id: "field_2",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastCreateField.GetName() != "F" ||
					stub.lastCreateField.GetType() != structurev1.FieldType_FIELD_TYPE_TEXT ||
					stub.lastCreateField.GetConfigMap() == nil {
					t.Fatalf("CreateField request = %v", stub.lastCreateField)
				}
			},
		},
		{
			method: http.MethodPatch, path: "/api/v1/fields/field_1",
			body: `{"name":"F2"}`, key: "field", id: "field_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastRenameField.GetFieldId() != "field_1" {
					t.Fatalf("RenameField field_id = %q", stub.lastRenameField.GetFieldId())
				}
			},
		},
		{
			method: http.MethodPut, path: "/api/v1/fields/field_1/config",
			body: `{"config_map":{"precision":3}}`, key: "field", id: "field_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastUpdateConfig.GetFieldId() != "field_1" || stub.lastUpdateConfig.GetConfigMap() == nil {
					t.Fatalf("UpdateFieldConfig request = %v", stub.lastUpdateConfig)
				}
			},
		},
		{
			method: http.MethodPost, path: "/api/v1/fields/field_1/convert",
			body: `{"target_type":"number","confirm":false}`, key: "field", id: "field_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastConvertField.GetFieldId() != "field_1" ||
					stub.lastConvertField.GetTargetType() != structurev1.FieldType_FIELD_TYPE_NUMBER ||
					stub.lastConvertField.GetConfirm() {
					t.Fatalf("ConvertField request = %v", stub.lastConvertField)
				}
			},
		},
		{
			method: http.MethodDelete, path: "/api/v1/fields/field_1",
			body: `{"confirm":true}`, key: "record_count",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastDeleteField.GetFieldId() != "field_1" || !stub.lastDeleteField.GetConfirm() {
					t.Fatalf("DeleteField request = %v", stub.lastDeleteField)
				}
			},
		},
		{
			method: http.MethodPut, path: "/api/v1/fields/field_1/options",
			body: `{"option_list":[{"id":"o1","name":"Opt1","color":"red"}],"allow_create":true}`,
			key:  "field", id: "field_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastUpdateOptions.GetFieldId() != "field_1" ||
					!stub.lastUpdateOptions.GetAllowCreate() ||
					len(stub.lastUpdateOptions.GetOptionList()) != 1 ||
					stub.lastUpdateOptions.GetOptionList()[0].GetId() != "o1" {
					t.Fatalf("UpdateOptions request = %v", stub.lastUpdateOptions)
				}
			},
		},
		{
			method: http.MethodPost, path: "/api/v1/fields/field_1/duplicate",
			key: "field", id: "field_copy",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastDuplicateField.GetFieldId() != "field_1" {
					t.Fatalf("DuplicateField field_id = %q", stub.lastDuplicateField.GetFieldId())
				}
			},
		},
		{
			method: http.MethodGet, path: "/api/v1/tables/table_1/views",
			key: "view_list", id: "view_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastListViews.GetTableId() != "table_1" {
					t.Fatalf("ListViews table_id = %q", stub.lastListViews.GetTableId())
				}
			},
		},
		{
			method: http.MethodPost, path: "/api/v1/tables/table_1/views",
			body: `{"name":"V1"}`, key: "view", id: "view_2",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastCreateView.GetTableId() != "table_1" ||
					stub.lastCreateView.GetName() != "V1" ||
					stub.lastCreateView.GetType() != "table" {
					t.Fatalf("CreateView request = %v", stub.lastCreateView)
				}
			},
		},
		{
			method: http.MethodPatch, path: "/api/v1/views/view_1",
			body: `{"name":"V2"}`, key: "view", id: "view_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastRenameView.GetViewId() != "view_1" {
					t.Fatalf("RenameView view_id = %q", stub.lastRenameView.GetViewId())
				}
			},
		},
		{
			method: http.MethodPost, path: "/api/v1/views/view_1/duplicate",
			key: "view", id: "view_copy",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastDuplicateView.GetViewId() != "view_1" {
					t.Fatalf("DuplicateView view_id = %q", stub.lastDuplicateView.GetViewId())
				}
			},
		},
		{
			method: http.MethodDelete, path: "/api/v1/views/view_1",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastDeleteView.GetViewId() != "view_1" {
					t.Fatalf("DeleteView view_id = %q", stub.lastDeleteView.GetViewId())
				}
			},
		},
		{
			method: http.MethodGet, path: "/api/v1/views/view_1/config",
			key: "view_config",
			checkStub: func(t *testing.T, stub *structureStub) {
				if stub.lastGetViewConfig.GetViewId() != "view_1" {
					t.Fatalf("GetViewConfig view_id = %q", stub.lastGetViewConfig.GetViewId())
				}
			},
		},
		{
			method: http.MethodPut, path: "/api/v1/views/view_1/config",
			body: `{"view_config":{"hidden_field_list":["f1"]}}`, key: "view_config",
			checkStub: func(t *testing.T, stub *structureStub) {
				config := stub.lastUpdateViewConfig
				if config.GetViewId() != "view_1" ||
					len(config.GetViewConfig().GetHiddenFieldList()) != 1 ||
					config.GetViewConfig().GetHiddenFieldList()[0] != "f1" {
					t.Fatalf("UpdateViewConfig request = %v", config)
				}
			},
		},
	}

	for _, route := range routes {
		response := structureRouteRequest(pipeline, route.method, route.path, route.body, true)
		statusCode, code, data := decodeWorkspaceEnvelope(t, response)
		if statusCode != http.StatusOK || code != 0 {
			t.Fatalf(
				"%s %s = HTTP %d code %d, want success",
				route.method,
				route.path,
				statusCode,
				code,
			)
		}
		if route.key != "" {
			item, ok := data[route.key]
			if !ok {
				t.Fatalf("%s %s: data missing key %q: %+v", route.method, route.path, route.key, data)
			}
			if route.id != "" {
				assertFirstID(t, item, route.id)
			}
		}
		route.checkStub(t, stub)
	}
}

func TestStructureRoutesMapGRPCError(t *testing.T) {
	stub := newStructureStub()
	stub.listTablesErr = status.Error(codes.PermissionDenied, "forbidden")
	pipeline := newStructurePipeline(t, stub)
	response := structureRouteRequest(pipeline, http.MethodGet, "/api/v1/bases/base_1/tables", "", true)
	statusCode, code, _ := decodeWorkspaceEnvelope(t, response)
	if statusCode != http.StatusForbidden || code != deal.CodePermissionDenied {
		t.Fatalf(
			"listTables gRPC error = HTTP %d code %d, want 403/3001",
			statusCode,
			code,
		)
	}
}

func TestStructureDeleteTableRequiresConfirm(t *testing.T) {
	stub := newStructureStub()
	pipeline := newStructurePipeline(t, stub)
	response := structureRouteRequest(pipeline, http.MethodDelete, "/api/v1/tables/table_1", `{"confirm":false}`, true)
	statusCode, code, _ := decodeWorkspaceEnvelope(t, response)
	if statusCode != http.StatusBadRequest || code != deal.CodeInvalidArgument {
		t.Fatalf("deleteTable confirm=false = HTTP %d code %d, want 400/1001", statusCode, code)
	}
	if stub.lastDeleteTable != nil {
		t.Fatalf("DeleteTable must not be called, got %v", stub.lastDeleteTable)
	}
}
