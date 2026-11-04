// share/add_collaborator.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// AddCollaborator grants one Base role, capping external collaborators at editor.
func (service *Service) AddCollaborator(
	ctx context.Context,
	request *sharev1.AddCollaboratorRequest,
) (*sharev1.AddCollaboratorResponse, error) {
	if request == nil ||
		request.GetBaseId() == "" ||
		strings.TrimSpace(request.GetUsername()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	role := normalizeRole(request.GetRole().String())
	if role != "viewer" && role != "editor" && role != "manager" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if _, err := service.requireAction(
		ctx,
		request.GetBaseId(),
		sharev1.Action_ACTION_MANAGE_SHARE,
	); err != nil {
		return nil, err
	}
	var (
		user *iamv1.User
		err  error
	)
	if service.userLookup == nil {
		user, err = service.findUserByUsername(ctx, request.GetUsername())
		if err != nil {
			return nil, err
		}
	}
	enterpriseID, err := service.bases.EnterpriseID(ctx, request.GetBaseId())
	if err != nil || enterpriseID == "" {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	userID := ""
	internal := false
	if user != nil {
		userID = user.GetId()
		internal = user.GetEnterpriseId() != "" &&
			user.GetEnterpriseId() == enterpriseID
	}
	if service.userLookup != nil {
		if lookup, ok := service.userLookup.(enterpriseUserLookup); ok {
			userID, internal, err = lookup.LookupForEnterprise(
				ctx,
				enterpriseID,
				strings.TrimSpace(request.GetUsername()),
			)
		} else {
			userID, internal, err = service.userLookup.Lookup(
				ctx,
				strings.TrimSpace(request.GetUsername()),
			)
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, deal.RPCError(deal.CodeConflict)
		case err != nil:
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		case userID == "":
			return nil, deal.RPCError(deal.CodeConflict)
		}
	}
	if !internal && role == "manager" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	collaborator := base.ShareCollaborator{
		BaseID:    request.GetBaseId(),
		UserID:    userID,
		Role:      role,
		Internal:  internal,
		Source:    "invite",
		GrantedAt: service.now().UTC().UnixMilli(),
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.InsertCollaborator(ctx, tx, collaborator); err != nil {
		if errors.Is(err, base.ErrShareCollaboratorExists) {
			return nil, deal.RPCError(deal.CodeConflict)
		}
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueShareChanged(
		ctx,
		tx,
		request.GetBaseId(),
		enterpriseID,
		"added",
		userID,
		role,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	response := protoCollaborator(collaborator)
	response.Username = strings.TrimSpace(request.GetUsername())
	response.DisplayName = response.Username
	if user != nil {
		response.Username = user.GetUsername()
		response.DisplayName = user.GetDisplayName()
	}
	return &sharev1.AddCollaboratorResponse{Collaborator: response}, nil
}

func (service *Service) findUserByUsername(ctx context.Context, username string) (*iamv1.User, error) {
	if service.users == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	response, err := service.users.SearchUsers(ctx, &iamv1.SearchUsersRequest{
		Q:     strings.TrimSpace(username),
		Limit: 10,
	})
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	for _, user := range response.GetUserList() {
		if strings.EqualFold(user.GetUsername(), username) {
			return user, nil
		}
	}
	return nil, deal.RPCError(deal.CodeConflict)
}
