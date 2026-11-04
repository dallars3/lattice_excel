// workspace/get_home.go：一文件一接口。
package workspace

import (
	"context"
	"excel_core_service/deal"

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
