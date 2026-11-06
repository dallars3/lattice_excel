// share/common.go：模块内可复用逻辑归口。
package share

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"

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

// UserProfileProvider resolves display identities (username/display_name) for
// collaborator entries. Implemented by iam over account persistence; the wire
// contract requires real identities（冒烟 H-63：条目三字段不得落 user_id）。
type UserProfileProvider interface {
	ProfilesByIDs(ctx context.Context, userIDs []string) (map[string]*iamv1.User, error)
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
) (*commonv1.LinkContext, error) {
	if resolver == nil || resolver.repository == nil || strings.TrimSpace(token) == "" {
		return nil, ErrShareLinkUnavailable
	}
	link, err := resolver.repository.LinkByToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrShareLinkUnavailable
		}
		return nil, err
	}
	if !validShareLinkRow(link) {
		return nil, ErrShareLinkUnavailable
	}
	return &commonv1.LinkContext{
		BaseId:    link.BaseID,
		LinkToken: link.Token,
		Role:      normalizeRole(link.Role),
	}, nil
}

// validShareLinkRow 是链接 token 有效性的唯一口径：链接启用、Base 寻址完整、
// 范围与角色格式合法。/s/{token} 匿名探测（CheckAccess）与 IAM 注册通道
// （ResolveLinkToken）共用同一谓词，保证入口与注册对同一 token 结论一致。
func validShareLinkRow(link base.ShareLink) bool {
	role := normalizeRole(link.Role)
	return link.Enabled &&
		link.BaseID != "" &&
		(link.Scope == "enterprise" || link.Scope == "anyone") &&
		(role == "viewer" || role == "filler")
}

// ImportLockProvider reports whether a Base is locked by an in-flight import.
type ImportLockProvider interface {
	// Importing returns true while the Base is being bootstrapped by an import
	// and is inaccessible to everyone（ticket 16）。
	Importing(ctx context.Context, baseID string) (bool, error)
}

// ServiceDeps contains the public seams used by the share service.
type ServiceDeps struct {
	Repository           *base.ShareRepository
	Outbox               *bus.Outbox
	Bases                BaseResolver
	BaseOwner            BaseOwnerProvider
	ImportLock           ImportLockProvider
	Members              MemberRoleProvider
	UserLookup           UserLookup
	Profiles             UserProfileProvider
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
	importLock ImportLockProvider
	members    MemberRoleProvider
	userLookup UserLookup
	profiles   UserProfileProvider
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
		importLock: deps.ImportLock,
		members:    deps.Members,
		userLookup: deps.UserLookup,
		profiles:   deps.Profiles,
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
	// Token 是被关闭/轮换的旧链接 token：网关据此立即清除 token→Base 缓存，
	// 使旧链在缓存 TTL 内也落到失效页（ticket 05）。重置时携带轮换前的 token。
	Token         string `json:"token"`
	CreatorID     string `json:"creator_id,omitempty"`
	ManagerUserID string `json:"manager_user_id,omitempty"`
}

func (service *Service) enqueueLinkClosed(
	ctx context.Context,
	tx *sql.Tx,
	payload linkClosedPayload,
) (string, error) {
	eventID, err := idgen.New()
	if err != nil {
		return "", fmt.Errorf("generate link.closed event id: %w", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal link.closed payload: %w", err)
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
		return "", fmt.Errorf("enqueue link.closed: %w", err)
	}
	return eventID, nil
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

// enqueueAuditAppend publishes an explicit audit.append event. auditEventID is
// the audit idempotency key: for force-close it reuses the link.closed event id
// so both paths collapse to one AUDIT_ACTION_LINK_FORCE_CLOSED row.
func (service *Service) enqueueAuditAppend(
	ctx context.Context,
	tx *sql.Tx,
	auditEventID, baseID, enterpriseID, action, operator, target string,
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
		AuditEventID string         `json:"audit_event_id,omitempty"`
		DetailMap    map[string]any `json:"detail_map"`
	}{
		BaseID:       baseID,
		EnterpriseID: enterpriseID,
		Action:       action,
		Operator:     operator,
		Target:       target,
		AuditEventID: auditEventID,
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
		return accessDecision{}, deal.RPCError(deal.CodeInvalidArgument)
	}
	enterpriseID, err := service.bases.EnterpriseID(ctx, baseID)
	if err != nil {
		// Base 不存在（缺陷 #18 TC-000-07）：按 4002/codes.NotFound 上抛而不是
		// 9001，WS 握手据此以 404 语义拒绝；HTTP 路由按契约「不存在→4002」响应。
		if errors.Is(err, base.ErrWorkspaceNotFound) {
			return accessDecision{}, deal.RPCError(deal.CodeDeletedResource)
		}
		return accessDecision{}, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if enterpriseID == "" {
		return accessDecision{}, deal.RPCError(deal.CodeDeletedResource)
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
		return accessDecision{}, deal.RPCError(deal.CodeServiceUnavailable)
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
			return accessDecision{}, deal.RPCError(deal.CodeServiceUnavailable)
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

// profilesFor resolves display identities for one batch of collaborator user
// IDs. An unwired provider degrades to user_id 兜底；wired provider failures are
// contract errors（§3 错误一律走契约错误码）。
func (service *Service) profilesFor(
	ctx context.Context,
	userIDs []string,
) (map[string]*iamv1.User, error) {
	if service == nil || service.profiles == nil || len(userIDs) == 0 {
		return map[string]*iamv1.User{}, nil
	}
	return service.profiles.ProfilesByIDs(ctx, userIDs)
}

// collaboratorEntry builds the wire shape for one grant: stable entry id（协作者
// 记录主键 (base_id, user_id) 在 Base 内的寻址键，等于 user_id）加上真实
// username/display_name；identity 缺失时才兜底 user_id（账号已注销等边缘）。
func collaboratorEntry(
	collaborator base.ShareCollaborator,
	profiles map[string]*iamv1.User,
) *sharev1.Collaborator {
	username := collaborator.UserID
	displayName := collaborator.UserID
	if profile := profiles[collaborator.UserID]; profile != nil {
		if profile.GetUsername() != "" {
			username = profile.GetUsername()
		}
		if profile.GetDisplayName() != "" {
			displayName = profile.GetDisplayName()
		}
	}
	return &sharev1.Collaborator{
		Id:          collaborator.UserID,
		UserId:      collaborator.UserID,
		Username:    username,
		DisplayName: displayName,
		Role:        protoRole(collaborator.Role),
		Internal:    collaborator.Internal,
		Source:      collaborator.Source,
		GrantedAt:   collaborator.GrantedAt,
	}
}

// resolveCollaboratorTarget addresses one grant by its stable entry id（即
// user_id），用户名寻址兜底（列表展示真实用户名后，管理方可直接按人操作）。
// 错误已按契约映射：目标不存在→1001（CodeInvalidArgument），寻址依赖故障→9001
// （CodeServiceUnavailable）；调用方直接透传即可。
func (service *Service) resolveCollaboratorTarget(
	ctx context.Context,
	collaborators []base.ShareCollaborator,
	identifier string,
) (base.ShareCollaborator, error) {
	for _, collaborator := range collaborators {
		if collaborator.UserID == identifier {
			return collaborator, nil
		}
	}
	if service.userLookup == nil {
		return base.ShareCollaborator{}, deal.RPCError(deal.CodeInvalidArgument)
	}
	userID, _, err := service.userLookup.Lookup(ctx, identifier)
	if errors.Is(err, sql.ErrNoRows) {
		return base.ShareCollaborator{}, deal.RPCError(deal.CodeInvalidArgument)
	}
	if err != nil {
		return base.ShareCollaborator{}, deal.RPCError(deal.CodeServiceUnavailable)
	}
	for _, collaborator := range collaborators {
		if collaborator.UserID == userID {
			return collaborator, nil
		}
	}
	return base.ShareCollaborator{}, deal.RPCError(deal.CodeInvalidArgument)
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
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	decision, err := service.resolveDecision(ctx, principal, baseID, principal.GetLinkContext())
	if err != nil {
		return nil, err
	}
	roles, err := service.policy.current()
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if !service.decisionAllows(action, decision, roles) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	return principal, nil
}
