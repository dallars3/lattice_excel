// iam/register.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// Register creates an account through one of the three ownership paths.
func (service *IamService) Register(ctx context.Context, request *iamv1.RegisterRequest) (*iamv1.RegisterResponse, error) {
	if request == nil || validateRegistration(request.GetUsername(), request.GetPassword()) != nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if request.GetEnterpriseName() != "" && request.GetLinkToken() != "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.GetPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	createdAt := service.now().UnixMilli()
	user := base.IAMUser{
		Username:     request.GetUsername(),
		DisplayName:  request.GetUsername(),
		PasswordHash: string(passwordHash),
		Internal:     true,
		CreatedAt:    createdAt,
	}
	user.ID, err = idgen.New()
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}

	principal := &commonv1.Principal{UserId: user.ID, Internal: true}
	switch {
	case request.GetEnterpriseName() != "":
		enterpriseName := strings.TrimSpace(request.GetEnterpriseName())
		if count := utf8.RuneCountInString(enterpriseName); count < 2 || count > 30 {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		enterprise := base.IAMEnterprise{
			Name:        enterpriseName,
			OwnerID:     user.ID,
			MemberCount: 1,
			CreatedAt:   createdAt,
		}
		enterprise.ID, err = idgen.New()
		if err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		user, _, err = service.repository.CreateUserWithEnterprise(ctx, user, enterprise)
		if isUsernameConflict(err) {
			return nil, rpcError(deal.CodeConflict)
		}
		if err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		principal.EnterpriseId = user.EnterpriseID
	case request.GetLinkToken() != "":
		if service.links == nil {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		linkContext, err := service.links.ResolveLinkToken(ctx, request.GetLinkToken())
		if err != nil || linkContext.GetBaseId() == "" || linkContext.GetLinkToken() == "" || linkContext.GetRole() == "" {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		user.Internal = false
		user.LinkBaseID = linkContext.GetBaseId()
		user.LinkToken = linkContext.GetLinkToken()
		user.LinkRole = linkContext.GetRole()
		user, err = service.repository.CreateUser(ctx, user)
		if isUsernameConflict(err) {
			return nil, rpcError(deal.CodeConflict)
		}
		if err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		principal.Internal = false
		principal.LinkContext = &linkContext
	default:
		user, err = service.repository.CreateUser(ctx, user)
		if isUsernameConflict(err) {
			return nil, rpcError(deal.CodeConflict)
		}
		if err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
	}

	token, err := newSessionToken()
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.sessions.Save(ctx, token, principal); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &iamv1.RegisterResponse{Token: token, User: protoUser(user)}, nil
}
