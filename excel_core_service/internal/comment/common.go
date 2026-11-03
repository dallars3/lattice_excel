// comment/common.go：评论共享身份、校验、投影与 outbox 事件。
package comment

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	commentv1 "excel/api/comment/v1"
	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
)

const (
	defaultCommentLimit = 50
	maxCommentLimit     = 100
	maxCommentLength    = 2000
)

// Service implements the comment RPC surface.
type Service struct {
	commentv1.UnimplementedCommentServiceServer

	repository *base.CommentRepository
	outbox     *bus.Outbox
	now        func() time.Time
}

// NewService constructs the comment service with its persistence and outbox seams.
func NewService(
	repository *base.CommentRepository,
	outbox *bus.Outbox,
	now func() time.Time,
) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		repository: repository,
		outbox:     outbox,
		now:        now,
	}
}

// Register installs the comment service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	commentv1.RegisterCommentServiceServer(registrar, service)
}

func principalFromContext(ctx context.Context) (*commonv1.Principal, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	values := incoming.Get("x-principal")
	if len(values) == 0 {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	payload, err := base64.StdEncoding.DecodeString(values[0])
	if err != nil {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	var principal commonv1.Principal
	if err := proto.Unmarshal(payload, &principal); err != nil ||
		principal.GetUserId() == "" {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	return &principal, nil
}

func validateContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return rpcError(deal.CodeInvalidArgument)
	}
	if utf8.RuneCountInString(content) > maxCommentLength {
		return rpcError(deal.CodeInvalidArgument)
	}
	return nil
}

func normalizeMentions(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	mentions := make([]string, 0, len(values))
	for _, raw := range values {
		userID := strings.TrimSpace(raw)
		if userID == "" {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		mentions = append(mentions, userID)
	}
	return mentions, nil
}

func normalizeCommentLimit(limit int32) (int, error) {
	if limit < 0 {
		return 0, rpcError(deal.CodeInvalidArgument)
	}
	if limit == 0 {
		return defaultCommentLimit, nil
	}
	if limit > maxCommentLimit {
		return maxCommentLimit, nil
	}
	return int(limit), nil
}

func commentToProto(comment base.Comment) *commentv1.Comment {
	status := commentv1.CommentStatus_COMMENT_STATUS_UNSPECIFIED
	if comment.Status == "OPEN" {
		status = commentv1.CommentStatus_COMMENT_STATUS_OPEN
	} else if comment.Status == "RESOLVED" {
		status = commentv1.CommentStatus_COMMENT_STATUS_RESOLVED
	}
	return &commentv1.Comment{
		Id:          comment.ID,
		RecordId:    comment.RecordID,
		ParentId:    comment.ParentID,
		Content:     comment.Content,
		MentionList: append([]string(nil), comment.Mentions...),
		Status:      status,
		Author:      comment.Author,
		CreatedAt:   comment.CreatedAt,
	}
}

func (service *Service) enqueueCreatedEvents(
	ctx context.Context,
	tx *sql.Tx,
	baseID string,
	comment base.Comment,
	parentAuthorID string,
) error {
	if service.outbox == nil {
		return errors.New("comment outbox is required")
	}
	payload := map[string]any{
		"base_id":    baseID,
		"record_id":  comment.RecordID,
		"comment_id": comment.ID,
		"author":     comment.Author,
	}
	if parentAuthorID != "" {
		payload["parent_author_id"] = parentAuthorID
	}
	if err := service.enqueueEvent(ctx, tx, baseID, "comment.created", payload); err != nil {
		return err
	}
	if len(comment.Mentions) == 0 {
		return nil
	}
	return service.enqueueEvent(ctx, tx, baseID, "comment.mentioned", map[string]any{
		"base_id":                baseID,
		"record_id":              comment.RecordID,
		"comment_id":             comment.ID,
		"mentioned_user_id_list": append([]string(nil), comment.Mentions...),
		"author":                 comment.Author,
	})
}

func (service *Service) enqueueEvent(
	ctx context.Context,
	tx *sql.Tx,
	baseID, topic string,
	payloadMap map[string]any,
) error {
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", topic, err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate %s event id: %w", topic, err)
	}
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       topic,
			Version:    1,
			BaseID:     baseID,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "comment",
		},
		Payload: payload,
	}
	if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
		return fmt.Errorf("enqueue %s: %w", topic, err)
	}
	return nil
}

func mapRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, base.ErrCommentNotFound),
		errors.Is(err, base.ErrCommentRecordNotFound):
		return rpcError(deal.CodeDeletedResource)
	default:
		return err
	}
}

func rpcError(code int) error {
	spec, ok := deal.LookupError(code)
	if !ok {
		spec, _ = deal.LookupError(deal.CodeServiceUnavailable)
		code = deal.CodeServiceUnavailable
	}
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(
		&errdetails.ErrorInfo{Reason: strconv.Itoa(code)},
	)
	if err != nil {
		return status.Error(spec.GRPCCode, spec.Message)
	}
	return withDetails.Err()
}
