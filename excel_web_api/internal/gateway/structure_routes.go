// gateway/structure_routes.go：数据表/字段/视图结构路由（H-29 ~ H-47、H-92）。
package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	sharev1 "excel/api/share/v1"
	structurev1 "excel/api/structure/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type structureRoutes struct {
	pipeline *Pipeline
	client   rpc.StructureRPC
}

// RegisterStructureRoutes binds the table/field/view structure endpoints.
func RegisterStructureRoutes(pipeline *Pipeline, client rpc.StructureRPC) {
	if pipeline == nil {
		panic("structure routes pipeline is required")
	}
	routes := &structureRoutes{pipeline: pipeline, client: client}
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/tables",
		sharev1.Action_ACTION_VIEW.String(),
		http.HandlerFunc(routes.listTables),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/tables",
		sharev1.Action_ACTION_MANAGE_STRUCTURE.String(),
		http.HandlerFunc(routes.createTable),
	)
	pipeline.SessionRequired(http.MethodPatch, "/api/v1/tables/{table_id}", http.HandlerFunc(routes.renameTable))
	pipeline.SessionRequired(http.MethodPost, "/api/v1/tables/{table_id}/duplicate", http.HandlerFunc(routes.duplicateTable))
	pipeline.SessionRequired(http.MethodDelete, "/api/v1/tables/{table_id}", http.HandlerFunc(routes.deleteTable))
	pipeline.SessionRequired(http.MethodGet, "/api/v1/tables/{table_id}/fields", http.HandlerFunc(routes.listFields))
	pipeline.SessionRequired(http.MethodPost, "/api/v1/tables/{table_id}/fields", http.HandlerFunc(routes.createField))
	pipeline.SessionRequired(http.MethodPatch, "/api/v1/fields/{field_id}", http.HandlerFunc(routes.renameField))
	pipeline.SessionRequired(http.MethodPut, "/api/v1/fields/{field_id}/config", http.HandlerFunc(routes.updateFieldConfig))
	pipeline.SessionRequired(http.MethodPost, "/api/v1/fields/{field_id}/convert", http.HandlerFunc(routes.convertField))
	pipeline.SessionRequired(http.MethodDelete, "/api/v1/fields/{field_id}", http.HandlerFunc(routes.deleteField))
	pipeline.SessionRequired(http.MethodPut, "/api/v1/fields/{field_id}/options", http.HandlerFunc(routes.updateOptions))
	pipeline.SessionRequired(http.MethodPost, "/api/v1/fields/{field_id}/duplicate", http.HandlerFunc(routes.duplicateField))
	pipeline.SessionRequired(http.MethodGet, "/api/v1/tables/{table_id}/views", http.HandlerFunc(routes.listViews))
	pipeline.SessionRequired(http.MethodPost, "/api/v1/tables/{table_id}/views", http.HandlerFunc(routes.createView))
	pipeline.SessionRequired(http.MethodPatch, "/api/v1/views/{view_id}", http.HandlerFunc(routes.renameView))
	pipeline.SessionRequired(http.MethodPost, "/api/v1/views/{view_id}/duplicate", http.HandlerFunc(routes.duplicateView))
	pipeline.SessionRequired(http.MethodDelete, "/api/v1/views/{view_id}", http.HandlerFunc(routes.deleteView))
	pipeline.SessionRequired(http.MethodGet, "/api/v1/views/{view_id}/config", http.HandlerFunc(routes.getViewConfig))
	pipeline.SessionRequired(http.MethodPut, "/api/v1/views/{view_id}/config", http.HandlerFunc(routes.updateViewConfig))
}

func (routes *structureRoutes) listTables(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	baseID := strings.TrimSpace(request.PathValue("base_id"))
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ListTables(
		request.Context(),
		&structurev1.ListTablesRequest{BaseId: baseID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) createTable(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	baseID := strings.TrimSpace(request.PathValue("base_id"))
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Name) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.CreateTable(
		request.Context(),
		&structurev1.CreateTableRequest{BaseId: baseID, Name: strings.TrimSpace(body.Name)},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) renameTable(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	tableID := strings.TrimSpace(request.PathValue("table_id"))
	if tableID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Name) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.RenameTable(
		request.Context(),
		&structurev1.RenameTableRequest{TableId: tableID, Name: strings.TrimSpace(body.Name)},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) duplicateTable(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	tableID := strings.TrimSpace(request.PathValue("table_id"))
	if tableID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DuplicateTable(
		request.Context(),
		&structurev1.DuplicateTableRequest{TableId: tableID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) deleteTable(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	tableID := strings.TrimSpace(request.PathValue("table_id"))
	if tableID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil || !body.Confirm {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DeleteTable(
		request.Context(),
		&structurev1.DeleteTableRequest{TableId: tableID, Confirm: true},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) listFields(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	tableID := strings.TrimSpace(request.PathValue("table_id"))
	if tableID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ListFields(
		request.Context(),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) createField(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	tableID := strings.TrimSpace(request.PathValue("table_id"))
	if tableID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Name      string           `json:"name"`
		Type      string           `json:"type"`
		ConfigMap *structpb.Struct `json:"config_map"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Name) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	fieldType, ok := parseFieldType(body.Type)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.CreateField(
		request.Context(),
		&structurev1.CreateFieldRequest{
			TableId:   tableID,
			Name:      strings.TrimSpace(body.Name),
			Type:      fieldType,
			ConfigMap: body.ConfigMap,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) renameField(
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
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Name) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.RenameField(
		request.Context(),
		&structurev1.RenameFieldRequest{FieldId: fieldID, Name: strings.TrimSpace(body.Name)},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) updateFieldConfig(
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
	var body struct {
		ConfigMap *structpb.Struct `json:"config_map"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil || body.ConfigMap == nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.UpdateFieldConfig(
		request.Context(),
		&structurev1.UpdateFieldConfigRequest{FieldId: fieldID, ConfigMap: body.ConfigMap},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) convertField(
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
	var body struct {
		TargetType string           `json:"target_type"`
		ConfigMap  *structpb.Struct `json:"config_map"`
		Confirm    bool             `json:"confirm"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	targetType, ok := parseFieldType(body.TargetType)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ConvertField(
		request.Context(),
		&structurev1.ConvertFieldRequest{
			FieldId:    fieldID,
			TargetType: targetType,
			ConfigMap:  body.ConfigMap,
			Confirm:    body.Confirm,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

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
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil || !body.Confirm {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DeleteField(
		request.Context(),
		&structurev1.DeleteFieldRequest{FieldId: fieldID, Confirm: true},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) updateOptions(
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
	var body struct {
		OptionList []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"option_list"`
		AllowCreate bool `json:"allow_create"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	options := make([]*structurev1.FieldOption, 0, len(body.OptionList))
	for _, option := range body.OptionList {
		options = append(options, &structurev1.FieldOption{
			Id:    option.ID,
			Name:  option.Name,
			Color: option.Color,
		})
	}
	result, err := routes.client.UpdateOptions(
		request.Context(),
		&structurev1.UpdateOptionsRequest{
			FieldId:     fieldID,
			OptionList:  options,
			AllowCreate: body.AllowCreate,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) duplicateField(
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
	result, err := routes.client.DuplicateField(
		request.Context(),
		&structurev1.DuplicateFieldRequest{FieldId: fieldID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) listViews(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	tableID := strings.TrimSpace(request.PathValue("table_id"))
	if tableID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ListViews(
		request.Context(),
		&structurev1.ListViewsRequest{TableId: tableID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) createView(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	tableID := strings.TrimSpace(request.PathValue("table_id"))
	if tableID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Name) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	viewType := strings.TrimSpace(body.Type)
	if viewType == "" {
		viewType = "table"
	}
	result, err := routes.client.CreateView(
		request.Context(),
		&structurev1.CreateViewRequest{
			TableId: tableID,
			Name:    strings.TrimSpace(body.Name),
			Type:    viewType,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) renameView(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	viewID := strings.TrimSpace(request.PathValue("view_id"))
	if viewID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Name) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.RenameView(
		request.Context(),
		&structurev1.RenameViewRequest{ViewId: viewID, Name: strings.TrimSpace(body.Name)},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) duplicateView(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	viewID := strings.TrimSpace(request.PathValue("view_id"))
	if viewID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DuplicateView(
		request.Context(),
		&structurev1.DuplicateViewRequest{ViewId: viewID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) deleteView(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	viewID := strings.TrimSpace(request.PathValue("view_id"))
	if viewID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DeleteView(
		request.Context(),
		&structurev1.DeleteViewRequest{ViewId: viewID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) getViewConfig(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	viewID := strings.TrimSpace(request.PathValue("view_id"))
	if viewID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.GetViewConfig(
		request.Context(),
		&structurev1.GetViewConfigRequest{ViewId: viewID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *structureRoutes) updateViewConfig(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	viewID := strings.TrimSpace(request.PathValue("view_id"))
	if viewID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		ViewConfig json.RawMessage `json:"view_config"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil || len(body.ViewConfig) == 0 {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var viewConfig structurev1.ViewConfig
	if err := protojson.Unmarshal(body.ViewConfig, &viewConfig); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.UpdateViewConfig(
		request.Context(),
		&structurev1.UpdateViewConfigRequest{ViewId: viewID, ViewConfig: &viewConfig},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}
