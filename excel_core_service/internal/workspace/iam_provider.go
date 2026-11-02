package workspace

import (
	"context"
	"errors"

	iamv1 "excel/api/iam/v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel_core_service/internal/iam"
)

type iamInvitationProvider struct {
	service *iam.IamService
}

// NewIamInvitationProvider adapts IAM's personal invitation RPC for the home
// aggregate without making IAM depend on workspace.
func NewIamInvitationProvider(service *iam.IamService) InvitationProvider {
	return &iamInvitationProvider{service: service}
}

func (provider *iamInvitationProvider) ListPendingInvitations(
	ctx context.Context,
) ([]*iamv1.Invitation, error) {
	if provider == nil || provider.service == nil {
		return nil, errors.New("IAM service is required")
	}
	response, err := provider.service.ListMyInvitations(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	pending := make([]*iamv1.Invitation, 0, len(response.GetInvitationList()))
	for _, invitation := range response.GetInvitationList() {
		if invitation.GetStatus() == iamv1.InvitationStatus_INVITATION_STATUS_PENDING {
			pending = append(pending, invitation)
		}
	}
	return pending, nil
}
