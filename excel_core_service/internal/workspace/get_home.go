// workspace/get_home.go：一文件一接口。
package workspace

import (
	"context"
	"strings"

	"excel_core_service/deal"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	workspacev1 "excel/api/workspace/v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel_core_service/base"
)

// HomeResult carries the contract response plus aggregate failure metadata for
// the HTTP partial-response layer.
type HomeResult struct {
	Home          *workspacev1.Home
	IsPartial     bool
	PartialGroups []string
}

// LoadHome assembles the home lists. A failing list or provider contributes an
// empty group and marks the result partial without failing unrelated groups.
func (service *Service) LoadHome(ctx context.Context) (*HomeResult, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	result := &HomeResult{Home: &workspacev1.Home{
		RecentList:  make([]*workspacev1.Base, 0),
		CreatedList: make([]*workspacev1.Base, 0),
		SharedList:  make([]*workspacev1.Base, 0),
		StarList:    make([]*workspacev1.Base, 0),
	}}
	load := func(
		group string,
		fetch func() ([]base.WorkspaceBase, error),
		assign func([]*workspacev1.Base),
	) {
		items, fetchErr := fetch()
		if fetchErr != nil {
			result.IsPartial = true
			result.PartialGroups = append(result.PartialGroups, group)
			assign(make([]*workspacev1.Base, 0))
			return
		}
		projected := make([]*workspacev1.Base, 0, len(items))
		for _, item := range items {
			projected = append(projected, workspaceBaseToProto(item))
		}
		assign(projected)
	}
	if service.repository == nil {
		for _, group := range []string{"recent", "created", "shared", "star", "invitations"} {
			result.IsPartial = true
			result.PartialGroups = append(result.PartialGroups, group)
		}
		return result, nil
	}
	userID := principal.GetUserId()
	load("recent", func() ([]base.WorkspaceBase, error) {
		return service.repository.RecentBases(ctx, userID)
	}, func(items []*workspacev1.Base) { result.Home.RecentList = items })
	load("created", func() ([]base.WorkspaceBase, error) {
		return service.repository.CreatedBases(ctx, userID)
	}, func(items []*workspacev1.Base) { result.Home.CreatedList = items })
	load("shared", func() ([]base.WorkspaceBase, error) {
		return service.repository.SharedBases(ctx, userID)
	}, func(items []*workspacev1.Base) { result.Home.SharedList = items })
	load("star", func() ([]base.WorkspaceBase, error) {
		return service.repository.StarredBases(ctx, userID)
	}, func(items []*workspacev1.Base) { result.Home.StarList = items })

	// 分享链接会话没有协作者行，SharedBases 查不到目标 Base；首页必须补上
	// 链接上下文指向的 Base，否则前端深链 /base/{id} 无法解析 Base 事实源。
	service.appendLinkBase(ctx, principal, result.Home)

	// 缺陷 #28：四列表的 last_modifier 统一解析为可读显示名（缺失回退用户名），
	// 合并去重后一次批量查询。
	allBases := make(
		[]*workspacev1.Base,
		0,
		len(result.Home.GetRecentList())+len(result.Home.GetCreatedList())+
			len(result.Home.GetSharedList())+len(result.Home.GetStarList()),
	)
	allBases = append(allBases, result.Home.GetRecentList()...)
	allBases = append(allBases, result.Home.GetCreatedList()...)
	allBases = append(allBases, result.Home.GetSharedList()...)
	allBases = append(allBases, result.Home.GetStarList()...)
	service.applyReadableModifiers(ctx, allBases)

	if service.invitations == nil {
		result.IsPartial = true
		result.PartialGroups = append(result.PartialGroups, "invitations")
	} else {
		invitations, invitationErr := service.invitations.ListPendingInvitations(ctx)
		if invitationErr != nil {
			result.IsPartial = true
			result.PartialGroups = append(result.PartialGroups, "invitations")
		} else {
			for _, invitation := range invitations {
				if invitation.GetStatus() == iamv1.InvitationStatus_INVITATION_STATUS_PENDING {
					result.Home.PendingInvitationCount++
				}
			}
		}
	}
	return result, nil
}

// appendLinkBase 把链接上下文指向的 Base 补进「与我共享」，并按链接角色
// 覆盖 my_role；导入锁定期内的 Base 不暴露。
func (service *Service) appendLinkBase(
	ctx context.Context,
	principal *commonv1.Principal,
	home *workspacev1.Home,
) {
	if service == nil || service.repository == nil || home == nil {
		return
	}
	link := principal.GetLinkContext()
	baseID := strings.TrimSpace(link.GetBaseId())
	if baseID == "" {
		return
	}
	for _, item := range home.GetSharedList() {
		if item.GetId() == baseID {
			return
		}
	}
	item, err := service.repository.BaseByID(ctx, baseID)
	if err != nil {
		return
	}
	if importing, err := service.repository.Importing(ctx, baseID); err == nil && importing {
		return
	}
	if role := strings.ToLower(strings.TrimSpace(link.GetRole())); role != "" {
		item.MyRole = role
	}
	home.SharedList = append(home.SharedList, workspaceBaseToProto(item))
}

// GetHome returns the four home lists and pending invitation count.
func (service *Service) GetHome(
	ctx context.Context,
	_ *emptypb.Empty,
) (*workspacev1.GetHomeResponse, error) {
	result, err := service.LoadHome(ctx)
	if err != nil {
		return nil, err
	}
	return &workspacev1.GetHomeResponse{Home: result.Home}, nil
}
