// iam/list_dead_letters.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// deadLetterLimitCeiling caps one dead-letter query regardless of request.
const deadLetterLimitCeiling = 1000

// ListDeadLetters lists events that exhausted consumer retries，死信可查
// （ticket 02）。RPC-only admin 通道，不经 HTTP、不外露。
func (service *IamService) ListDeadLetters(
	ctx context.Context,
	request *iamv1.ListDeadLettersRequest,
) (*iamv1.ListDeadLettersResponse, error) {
	if err := service.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if service.outbox == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	limit := 0
	if request != nil && request.GetLimit() > 0 {
		limit = int(request.GetLimit())
	}
	if limit > deadLetterLimitCeiling {
		limit = deadLetterLimitCeiling
	}
	letters, err := service.outbox.DeadLetters(ctx, limit)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	entries := make([]*iamv1.DeadLetterEntry, 0, len(letters))
	for _, letter := range letters {
		entries = append(entries, &iamv1.DeadLetterEntry{
			EventId:    letter.EventID,
			Type:       letter.Type,
			Attempts:   int32(letter.Attempts),
			LastError:  letter.LastError,
			OccurredAt: letter.OccurredAt.UTC().UnixMilli(),
			DeadAt:     letter.DeadAt.UTC().UnixMilli(),
			Payload:    string(letter.Payload),
		})
	}
	return &iamv1.ListDeadLettersResponse{EntryList: entries}, nil
}
