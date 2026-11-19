// iam/login.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"golang.org/x/crypto/bcrypt"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// 票 04（附录 B 2002）：同一业务码按场景区分两种用户文案，经信封 message 透传。
const (
	loginBadCredentialsMessage = "用户名或密码错误"
	loginLockedMessage         = "失败次数过多，请稍后再试"
)

// Login validates credentials, enforces username+IP lockout, and issues a 30-day session.
func (service *IamService) Login(ctx context.Context, request *iamv1.LoginRequest) (*iamv1.LoginResponse, error) {
	if request == nil || request.GetUsername() == "" || request.GetPassword() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}

	ip := clientIP(ctx)
	locked, err := service.sessions.LoginLocked(ctx, request.GetUsername(), ip)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if locked {
		return nil, deal.RPCErrorMessage(deal.CodeLoginFailed, loginLockedMessage)
	}

	user, err := service.repository.UserByUsername(ctx, request.GetUsername())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.rejectLogin(ctx, request.GetUsername(), ip)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.GetPassword())); err != nil {
		return nil, service.rejectLogin(ctx, request.GetUsername(), ip)
	}
	if err := service.sessions.ClearLoginFailures(ctx, request.GetUsername(), ip); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}

	// 票 02（N7）：存量孤立账号（有用户无企业）登录时幂等补建默认企业。补建失败
	// 只记日志不阻断登录，下次登录重试。
	user, err = service.ensureDefaultEnterprise(ctx, user)
	if err != nil {
		slog.Warn("iam: backfill default enterprise failed", "user_id", user.ID, "error", err)
	}

	token, err := newSessionToken()
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.sessions.Save(ctx, token, sessionPrincipal(user)); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &iamv1.LoginResponse{Token: token, User: protoUser(user)}, nil
}

func (service *IamService) rejectLogin(ctx context.Context, username, ip string) error {
	if _, err := service.sessions.RecordLoginFailure(ctx, username, ip); err != nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	return deal.RPCErrorMessage(deal.CodeLoginFailed, loginBadCredentialsMessage)
}
