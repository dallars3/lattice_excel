// records/get_history.go：90 天字段级操作历史。
package records

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	recordsv1 "excel/api/records/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

const historyRetention = 90 * 24 * time.Hour

type historyCursor struct {
	CreatedAt int64 `json:"created_at"`
	Seq       int64 `json:"seq"`
}

// GetHistory returns visible field changes inside the 90-day retention window.
func (service *Service) GetHistory(
	ctx context.Context,
	request *recordsv1.GetHistoryRequest,
) (*recordsv1.GetHistoryResponse, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("records repository is not configured")
	}
	if request == nil || strings.TrimSpace(request.GetRecordId()) == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	scope, err := rowScopeFromContext(ctx)
	if err != nil {
		return nil, err
	}
	record, err := service.repository.RecordByID(ctx, request.GetRecordId())
	if err != nil {
		return nil, mapRecordError(err)
	}
	if scope == rowScopeOwn &&
		record.CreatedBy != principal.GetUserId() {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	since := service.now().UTC().Add(-historyRetention).UnixMilli()
	histories, err := service.repository.HistoriesByRecord(
		ctx,
		record.ID,
		since,
	)
	if err != nil {
		return nil, err
	}
	start := 0
	if request.GetCursor() != "" {
		cursor, err := decodeHistoryCursor(request.GetCursor())
		if err != nil {
			return nil, err
		}
		start = historyCursorPosition(histories, cursor)
		if start < 0 {
			return nil, dataError("cursor does not match record history")
		}
	}
	limit := normalizePageLimit(request.GetLimit())
	end := start + limit
	if end > len(histories) {
		end = len(histories)
	}
	page := histories[start:end]
	response := &recordsv1.GetHistoryResponse{
		HistoryList: make([]*recordsv1.HistoryNode, 0, len(page)),
		HasMore:     end < len(histories),
	}
	for _, history := range page {
		response.HistoryList = append(response.HistoryList, historyToProto(history))
	}
	if response.HasMore && len(page) > 0 {
		response.NextCursor, err = encodeHistoryCursor(historyCursor{
			CreatedAt: page[len(page)-1].CreatedAt,
			Seq:       page[len(page)-1].Seq,
		})
		if err != nil {
			return nil, err
		}
	}
	return response, nil
}

func historyToProto(history base.RecordHistory) *recordsv1.HistoryNode {
	return &recordsv1.HistoryNode{
		Id:        history.ID,
		RecordId:  history.RecordID,
		FieldId:   history.FieldID,
		Before:    history.Before,
		After:     history.After,
		Operator:  history.Operator,
		CreatedAt: history.CreatedAt,
	}
}

func historyCursorPosition(histories []base.RecordHistory, cursor historyCursor) int {
	for index, history := range histories {
		if history.CreatedAt == cursor.CreatedAt && history.Seq == cursor.Seq {
			return index + 1
		}
	}
	for index, history := range histories {
		if history.CreatedAt < cursor.CreatedAt ||
			(history.CreatedAt == cursor.CreatedAt && history.Seq < cursor.Seq) {
			return index
		}
	}
	return -1
}

func encodeHistoryCursor(cursor historyCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeHistoryCursor(value string) (historyCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return historyCursor{}, dataError("invalid history cursor")
	}
	var cursor historyCursor
	if err := json.Unmarshal(payload, &cursor); err != nil ||
		cursor.CreatedAt <= 0 {
		return historyCursor{}, dataError("invalid history cursor")
	}
	return cursor, nil
}
