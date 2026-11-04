// workspace/common.go：模块内可复用逻辑归口。
package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	workspacev1 "excel/api/workspace/v1"
	"google.golang.org/grpc"

	"excel_core_service/base"
	"excel_core_service/deal"
	"excel_core_service/internal/structure"
)

const defaultBaseName = "未命名多维表格"

// RoleProvider is the read-only enterprise-membership seam.
type RoleProvider interface {
	Role(ctx context.Context, enterpriseID, userID string) (iamv1.MemberRole, error)
}

// InvitationProvider supplies the current user's pending enterprise invitations.
type InvitationProvider interface {
	ListPendingInvitations(ctx context.Context) ([]*iamv1.Invitation, error)
}

// RecordsDuplicator copies record rows between two table identities.
type RecordsDuplicator interface {
	DuplicateTableRows(ctx context.Context, sourceTableID, targetTableID string) error
}

// RecordsDeleter deletes all record rows for a Base.
type RecordsDeleter interface {
	DeleteBaseRows(ctx context.Context, baseID string, tableIDList []string) error
}

// CommentsDeleter removes comment-owned data for a Base.
type CommentsDeleter interface {
	DeleteBaseComments(ctx context.Context, baseID string) error
}

// LinksCleaner removes share collaborators and links for a Base.
type LinksCleaner interface {
	DeleteAllForBase(ctx context.Context, baseID string) error
}

// EventOutbox is the transactional event seam used by DeleteBase.
type EventOutbox interface {
	Enqueue(ctx context.Context, tx *sql.Tx, event eventbus.Event) error
}

// StructureBootstrapFunc creates the default structure for one Base.
type StructureBootstrapFunc func(ctx context.Context, baseID string) (tableID string, err error)

// StructureCleanupFunc removes all structure rows owned by one Base.
type StructureCleanupFunc func(ctx context.Context, baseID string) error

// StructureDuplicator copies structure from one Base to another and returns
// the old-table-id to new-table-id mapping.
type StructureDuplicator interface {
	DuplicateBase(
		ctx context.Context,
		enterpriseID, sourceBaseID, targetBaseID string,
	) (map[string]string, error)
}

// Service implements WorkspaceService.
type Service struct {
	workspacev1.UnimplementedWorkspaceServiceServer

	repository         *base.WorkspaceRepository
	roles              RoleProvider
	invitations        InvitationProvider
	records            RecordsDuplicator
	recordDeleter      RecordsDeleter
	comments           CommentsDeleter
	structureBootstrap StructureBootstrapFunc
	structureCleanup   StructureCleanupFunc
	linksCleaner       LinksCleaner
	outbox             EventOutbox
	now                func() time.Time
	structure          *base.StructureRepository
	duplicator         StructureDuplicator
}

// NewService constructs the workspace RPC service with explicit cross-module
// seams. Optional dependencies may include *base.StructureRepository and a
// StructureDuplicator.
func NewService(
	repository *base.WorkspaceRepository,
	roles RoleProvider,
	invitations InvitationProvider,
	records RecordsDuplicator,
	recordDeleter RecordsDeleter,
	comments CommentsDeleter,
	structureBootstrap StructureBootstrapFunc,
	structureCleanup StructureCleanupFunc,
	linksCleaner LinksCleaner,
	outbox EventOutbox,
	now func() time.Time,
	dependencies ...any,
) *Service {
	if now == nil {
		now = time.Now
	}
	service := &Service{
		repository:         repository,
		roles:              roles,
		invitations:        invitations,
		records:            records,
		recordDeleter:      recordDeleter,
		comments:           comments,
		structureBootstrap: structureBootstrap,
		structureCleanup:   structureCleanup,
		linksCleaner:       linksCleaner,
		outbox:             outbox,
		now:                now,
	}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case *base.StructureRepository:
			service.structure = value
		case StructureDuplicator:
			service.duplicator = value
		}
	}
	return service
}

// Register installs the workspace service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	workspacev1.RegisterWorkspaceServiceServer(registrar, service)
}

// NewStructureBootstrap adapts the structure module facade to this service.
func NewStructureBootstrap(repository *base.StructureRepository) StructureBootstrapFunc {
	return func(ctx context.Context, baseID string) (string, error) {
		return structure.BootstrapDefaultStructure(ctx, repository, baseID)
	}
}

// NewStructureCleanup adapts the structure module cleanup facade.
func NewStructureCleanup(repository *base.StructureRepository) StructureCleanupFunc {
	return func(ctx context.Context, baseID string) error {
		return structure.DeleteAllForBase(ctx, repository, baseID)
	}
}

func (service *Service) requireManager(
	ctx context.Context,
	principal *commonv1.Principal,
) error {
	if service == nil || service.roles == nil ||
		principal.GetEnterpriseId() == "" {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	role, err := service.roles.Role(
		ctx,
		principal.GetEnterpriseId(),
		principal.GetUserId(),
	)
	if err != nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	if role != iamv1.MemberRole_MEMBER_ROLE_OWNER &&
		role != iamv1.MemberRole_MEMBER_ROLE_MANAGER {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	return nil
}

func (service *Service) requireBaseManager(
	ctx context.Context,
	principal *commonv1.Principal,
	item base.WorkspaceBase,
) error {
	if item.OwnerID == principal.GetUserId() {
		return nil
	}
	return service.requireManager(ctx, principal)
}

func normalizeBaseName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultBaseName
	}
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 50 {
		return "", deal.RPCError(deal.CodeInvalidArgument)
	}
	return name, nil
}

func truncatedCopyWithSuffix(name, suffix string) string {
	runes := []rune(strings.TrimSpace(name))
	limit := 50 - utf8.RuneCountInString(suffix)
	if limit < 1 {
		limit = 1
	}
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes) + suffix
}

func workspaceBaseToProto(item base.WorkspaceBase) *workspacev1.Base {
	return &workspacev1.Base{
		Id:             item.ID,
		Name:           item.Name,
		TableCount:     int32(item.TableCount),
		MyRole:         item.MyRole,
		Starred:        item.Starred,
		LastModifiedAt: item.LastModifiedAt,
		LastModifier:   item.LastModifier,
	}
}

func mapWorkspaceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, base.ErrWorkspaceNotFound) {
		return deal.RPCError(deal.CodeDeletedResource)
	}
	return fmt.Errorf("workspace operation: %w", err)
}
