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
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if request.GetEnterpriseName() != "" && request.GetLinkToken() != "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	// 票 02 语义决策：默认建企（企业名与链接令牌均空）现在会创建企业，故与带企业名、
	// 链接令牌两条既有受约束路径同口径，注册开关约束全部三条注册路径（开关关闭即
	// 「当前未开放新注册」，既有账号登录不受影响）。
	enabled, err := service.repository.RegisterSwitchEnabled(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if !enabled {
		return nil, deal.RPCErrorMessage(deal.CodeInvalidArgument, "当前未开放新注册")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.GetPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
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
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}

	principal := &commonv1.Principal{UserId: user.ID, Internal: true}
	switch {
	case request.GetLinkToken() != "":
		if service.links == nil {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		linkContext, err := service.links.ResolveLinkToken(ctx, request.GetLinkToken())
		if err != nil || linkContext.GetBaseId() == "" || linkContext.GetLinkToken() == "" || linkContext.GetRole() == "" {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		user.Internal = false
		user.LinkBaseID = linkContext.GetBaseId()
		user.LinkToken = linkContext.GetLinkToken()
		user.LinkRole = linkContext.GetRole()
		user, err = service.repository.CreateUser(ctx, user)
		if isUsernameConflict(err) {
			return nil, deal.RPCError(deal.CodeConflict)
		}
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		principal.Internal = false
		principal.LinkContext = linkContext
	default:
		// 带企业名与默认建企（票 02：企业名为空时自动派生默认企业）共用既有
		// 「用户+企业」同事务落库通道，注册者即为该企业所有者。
		enterpriseName := strings.TrimSpace(request.GetEnterpriseName())
		if enterpriseName != "" {
			if count := utf8.RuneCountInString(enterpriseName); count < 2 || count > 30 {
				return nil, deal.RPCError(deal.CodeInvalidArgument)
			}
		} else {
			enterpriseName = defaultEnterpriseName(user.Username)
		}
		enterprise := base.IAMEnterprise{
			Name:        enterpriseName,
			OwnerID:     user.ID,
			MemberCount: 1,
			CreatedAt:   createdAt,
		}
		enterprise.ID, err = idgen.New()
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		user, _, err = service.repository.CreateUserWithEnterprise(ctx, user, enterprise)
		if isUsernameConflict(err) {
			return nil, deal.RPCError(deal.CodeConflict)
		}
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		principal.EnterpriseId = user.EnterpriseID
	}

	token, err := newSessionToken()
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.sessions.Save(ctx, token, principal); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &iamv1.RegisterResponse{Token: token, User: protoUser(user)}, nil
}
