// share/common.go：模块内可复用逻辑归口。
package share

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

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
)

// BaseResolver is implemented by workspace after ticket 09 lands.
type BaseResolver interface {
	EnterpriseID(ctx context.Context, baseID string) (string, error)
}

// UserLookup resolves a registered username and whether the account is internal.
type UserLookup interface {
	Lookup(ctx context.Context, username string) (userID string, internal bool, err error)
}

type enterpriseUserLookup interface {
	LookupForEnterprise(
		ctx context.Context,
		enterpriseID, username string,
	) (userID string, internal bool, err error)
}

// BaseOwnerProvider resolves the workspace owner of a Base.
type BaseOwnerProvider interface {
	OwnerID(ctx context.Context, baseID string) (string, error)
}

// MemberRoleProvider is implemented by IAM after ticket 06 lands.
type MemberRoleProvider interface {
	Role(ctx context.Context, enterpriseID, userID string) (iamv1.MemberRole, error)
}

// ErrShareLinkUnavailable reports a missing or disabled link token.
var ErrShareLinkUnavailable = errors.New("share link unavailable")

// LinkResolver is the production adapter for IAM's LinkResolver seam.
type LinkResolver struct {
	repository *base.ShareRepository
}

// NewLinkResolver creates the IAM-facing share-link adapter.
func NewLinkResolver(repository *base.ShareRepository) *LinkResolver {
	return &LinkResolver{repository: repository}
}

// ResolveLinkToken returns only a currently enabled, well-formed link context.
func (resolver *LinkResolver) ResolveLinkToken(
	ctx context.Context,
	token string,
) (commonv1.LinkContext, error) {
	if resolver == nil || resolver.repository == nil || strings.TrimSpace(token) == "" {
		return commonv1.LinkContext{}, ErrShareLinkUnavailable
	}
	link, err := resolver.repository.LinkByToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return commonv1.LinkContext{}, ErrShareLinkUnavailable
		}
		return commonv1.LinkContext{}, err
	}
	role := normalizeRole(link.Role)
	if !link.Enabled ||
		link.BaseID == "" ||
		(link.Scope != "enterprise" && link.Scope != "anyone") ||
		(role != "viewer" && role != "filler") {
		return commonv1.LinkContext{}, ErrShareLinkUnavailable
	}
	return commonv1.LinkContext{
		BaseId:    link.BaseID,
		LinkToken: link.Token,
		Role:      role,
	}, nil
}

// ServiceDeps contains the public seams used by the share service.
type ServiceDeps struct {
	Repository           *base.ShareRepository
	Outbox               *bus.Outbox
	Bases                BaseResolver
	BaseOwner            BaseOwnerProvider
	Members              MemberRoleProvider
	UserLookup           UserLookup
	Users                iamv1.IamServiceClient
	PermissionConfigPath string
}

// Service implements the share RPC surface.
type Service struct {
	sharev1.UnimplementedShareServiceServer

	repository *base.ShareRepository
	outbox     *bus.Outbox
	bases      BaseResolver
	baseOwner  BaseOwnerProvider
	members    MemberRoleProvider
	userLookup UserLookup
	users      iamv1.IamServiceClient
	policy     *permissionPolicy
	now        func() time.Time
}

// NewService constructs the share service and loads its permission policy.
func NewService(deps ServiceDeps) (*Service, error) {
	if deps.Repository == nil {
		return nil, errors.New("share repository is required")
	}
	if deps.Outbox == nil {
		return nil, errors.New("share outbox is required")
	}
	if deps.Bases == nil {
		return nil, errors.New("base resolver is required")
	}
	if deps.Members == nil {
		return nil, errors.New("member role provider is required")
	}
	policy, err := newPermissionPolicy(deps.PermissionConfigPath)
	if err != nil {
		return nil, err
	}
	return &Service{
		repository: deps.Repository,
		outbox:     deps.Outbox,
		bases:      deps.Bases,
		baseOwner:  deps.BaseOwner,
		members:    deps.Members,
		userLookup: deps.UserLookup,
		users:      deps.Users,
		policy:     policy,
		now:        time.Now,
	}, nil
}

type linkClosedPayload struct {
	BaseID        string `json:"base_id"`
	LinkID        string `json:"link_id"`
	EnterpriseID  string `json:"enterprise_id"`
	Operator      string `json:"operator"`
	Reason        string `json:"reason"`
	CreatorID     string `json:"creator_id,omitempty"`
	ManagerUserID string `json:"manager_user_id,omitempty"`
}

func (service *Service) enqueueLinkClosed(
	ctx context.Context,
	tx *sql.Tx,
	payload linkClosedPayload,
) error {
	eventID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate link.closed event id: %w", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal link.closed payload: %w", err)
	}
	err = service.outbox.Enqueue(ctx, tx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "link.closed",
			Version:    1,
			BaseID:     payload.BaseID,
			Seq:        0,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "share",
		},
		Payload: encoded,
	})
	if err != nil {
		return fmt.Errorf("enqueue link.closed: %w", err)
	}
	return nil
}

func (service *Service) enqueueShareChanged(
	ctx context.Context,
	tx *sql.Tx,
	baseID, enterpriseID, changeType, userID, role string,
) error {
	eventID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate share.changed event id: %w", err)
	}
	payload, err := json.Marshal(struct {
		BaseID       string `json:"base_id"`
		EnterpriseID string `json:"enterprise_id"`
		ChangeType   string `json:"change_type"`
		UserID       string `json:"user_id"`
		Role         string `json:"role,omitempty"`
	}{
		BaseID:       baseID,
		EnterpriseID: enterpriseID,
		ChangeType:   changeType,
		UserID:       userID,
		Role:         role,
	})
	if err != nil {
		return fmt.Errorf("marshal share.changed payload: %w", err)
	}
	err = service.outbox.Enqueue(ctx, tx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "share.changed",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "share",
		},
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("enqueue share.changed: %w", err)
	}
	return nil
}

func (service *Service) enqueueAuditAppend(
	ctx context.Context,
	tx *sql.Tx,
	baseID, enterpriseID, action, operator, target string,
) error {
	eventID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate audit.append event id: %w", err)
	}
	payload, err := json.Marshal(struct {
		BaseID       string         `json:"base_id"`
		EnterpriseID string         `json:"enterprise_id"`
		Action       string         `json:"action"`
		Operator     string         `json:"operator"`
		Target       string         `json:"target"`
		DetailMap    map[string]any `json:"detail_map"`
	}{
		BaseID:       baseID,
		EnterpriseID: enterpriseID,
		Action:       action,
		Operator:     operator,
		Target:       target,
		DetailMap: map[string]any{
			"enterprise_id": enterpriseID,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal audit.append payload: %w", err)
	}
	err = service.outbox.Enqueue(ctx, tx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "audit.append",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "share",
		},
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("enqueue audit.append: %w", err)
	}
	return nil
}

// Register installs the share service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	sharev1.RegisterShareServiceServer(registrar, service)
}

type accessDecision struct {
	role     string
	internal bool
}

func (service *Service) resolveDecision(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
	linkContext *commonv1.LinkContext,
) (accessDecision, error) {
	if principal == nil || principal.GetUserId() == "" || strings.TrimSpace(baseID) == "" {
		return accessDecision{}, rpcError(deal.CodeInvalidArgument)
	}
	enterpriseID, err := service.bases.EnterpriseID(ctx, baseID)
	if err != nil || enterpriseID == "" {
		return accessDecision{}, rpcError(deal.CodeServiceUnavailable)
	}

	internal := principal.GetEnterpriseId() != "" && principal.GetEnterpriseId() == enterpriseID
	if internal {
		memberRole, err := service.members.Role(ctx, enterpriseID, principal.GetUserId())
		if err != nil || memberRole == iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED {
			return accessDecision{internal: true}, nil
		}
	}

	role := ""
	collaborator, err := service.repository.Collaborator(ctx, baseID, principal.GetUserId())
	switch {
	case err == nil:
		role = normalizeRole(collaborator.Role)
	case errors.Is(err, sql.ErrNoRows):
	default:
		return accessDecision{}, rpcError(deal.CodeServiceUnavailable)
	}
	if internal && service.baseOwner != nil {
		ownerID, err := service.baseOwner.OwnerID(ctx, baseID)
		if err == nil && ownerID == principal.GetUserId() {
			role = "manager"
		}
	}

	if linkContext == nil {
		linkContext = principal.GetLinkContext()
	}
	if linkContext != nil && linkContext.GetLinkToken() != "" {
		link, err := service.repository.LinkByToken(ctx, linkContext.GetLinkToken())
		switch {
		case err == nil:
			linkRole := normalizeRole(link.Role)
			contextRole := normalizeRole(linkContext.GetRole())
			scope := strings.ToLower(strings.TrimSpace(link.Scope))
			validLink := link.Enabled &&
				link.BaseID == baseID &&
				linkContext.GetBaseId() == baseID &&
				(contextRole == "viewer" || contextRole == "filler") &&
				linkRole != "" &&
				(scope == "enterprise" || scope == "anyone") &&
				(scope != "anyone" || linkRole == "viewer" || linkRole == "filler")
			if scope == "enterprise" && !internal {
				validLink = false
			}
			if validLink && compareRoles(linkRole, role) > 0 {
				role = linkRole
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return accessDecision{}, rpcError(deal.CodeServiceUnavailable)
		}
	}
	if !internal {
		role = capExternalRole(role)
	}
	return accessDecision{role: role, internal: internal}, nil
}

func normalizeRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	role = strings.TrimPrefix(role, "role_")
	switch role {
	case "viewer", "filler", "editor", "manager":
		return role
	default:
		return ""
	}
}

func compareRoles(left, right string) int {
	order := map[string]int{"": 0, "viewer": 1, "filler": 2, "editor": 3, "manager": 4}
	return order[left] - order[right]
}

func capExternalRole(role string) string {
	if compareRoles(role, "editor") > 0 {
		return "editor"
	}
	return role
}

func protoRole(role string) sharev1.Role {
	switch role {
	case "viewer":
		return sharev1.Role_ROLE_VIEWER
	case "filler":
		return sharev1.Role_ROLE_FILLER
	case "editor":
		return sharev1.Role_ROLE_EDITOR
	case "manager":
		return sharev1.Role_ROLE_MANAGER
	default:
		return sharev1.Role_ROLE_UNSPECIFIED
	}
}

func rowScope(role string) sharev1.RowScope {
	if role == "filler" {
		return sharev1.RowScope_ROW_SCOPE_OWN
	}
	return sharev1.RowScope_ROW_SCOPE_ALL
}

func protoCollaborator(collaborator base.ShareCollaborator) *sharev1.Collaborator {
	return &sharev1.Collaborator{
		UserId:      collaborator.UserID,
		Username:    collaborator.UserID,
		DisplayName: collaborator.UserID,
		Role:        protoRole(collaborator.Role),
		Internal:    collaborator.Internal,
		Source:      collaborator.Source,
		GrantedAt:   collaborator.GrantedAt,
	}
}

func actionName(action sharev1.Action) string {
	switch action {
	case sharev1.Action_ACTION_VIEW:
		return "view"
	case sharev1.Action_ACTION_CREATE_RECORD:
		return "create_record"
	case sharev1.Action_ACTION_EDIT_ALL:
		return "edit_all"
	case sharev1.Action_ACTION_EDIT_OWN:
		return "edit_own"
	case sharev1.Action_ACTION_DELETE_RECORD:
		return "delete_record"
	case sharev1.Action_ACTION_MANAGE_VIEW:
		return "manage_view"
	case sharev1.Action_ACTION_MANAGE_STRUCTURE:
		return "manage_structure"
	case sharev1.Action_ACTION_EXPORT:
		return "export"
	case sharev1.Action_ACTION_MANAGE_SHARE:
		return "manage_share"
	case sharev1.Action_ACTION_COPY_BASE:
		return "copy_base"
	case sharev1.Action_ACTION_IMPORT:
		return "import"
	case sharev1.Action_ACTION_COMMENT:
		return "comment"
	default:
		return ""
	}
}

func (service *Service) decisionAllows(
	action sharev1.Action,
	decision accessDecision,
	roles map[string]map[string]struct{},
) bool {
	if decision.role == "" {
		return false
	}
	if action == sharev1.Action_ACTION_EXPORT && !decision.internal {
		return false
	}
	actions := roles[decision.role]
	if _, ok := actions[actionName(action)]; ok {
		return true
	}
	if action == sharev1.Action_ACTION_VIEW && decision.role == "filler" {
		_, ok := actions["view_own"]
		return ok
	}
	return false
}

func (service *Service) requireAction(
	ctx context.Context,
	baseID string,
	action sharev1.Action,
) (*commonv1.Principal, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	decision, err := service.resolveDecision(ctx, principal, baseID, principal.GetLinkContext())
	if err != nil {
		return nil, err
	}
	roles, err := service.policy.current()
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if !service.decisionAllows(action, decision, roles) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	return principal, nil
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
	if err := proto.Unmarshal(payload, &principal); err != nil || principal.GetUserId() == "" {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	return &principal, nil
}

func rpcError(code int) error {
	spec, _ := deal.LookupError(code)
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(&errdetails.ErrorInfo{
		Reason: strconv.Itoa(code),
	})
	if err != nil {
		return status.Error(spec.GRPCCode, spec.Message)
	}
	return withDetails.Err()
}
