// iam/default_enterprise.go：票 02（N7）默认企业派生与孤立账号补建归口。
// 注册页承诺「注册即为你新建一个企业」——默认分支（企业名与链接令牌均空）与
// 孤立账号补建共用本文件的名称派生规则。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"fmt"

	"excel-libs/idgen"
	"excel_core_service/base"
)

// defaultEnterpriseNameFallback is the fixed name used when a derived default
// enterprise name would violate the 2-character minimum (defensive; usernames
// validated by validUsername can never trigger it).
const defaultEnterpriseNameFallback = "我的企业"

// defaultEnterpriseNameMaxRunes mirrors the enterprise-name length cap (2–30).
const defaultEnterpriseNameMaxRunes = 30

// defaultEnterpriseName derives the default enterprise name from a username:
// "{username} 的企业"，超出企业名 30 字符上限时按 rune 截断，结果不足 2 字符时
// 以固定文案兜底。企业名无唯一约束，不做查重。
func defaultEnterpriseName(username string) string {
	runes := []rune(username + " 的企业")
	if len(runes) > defaultEnterpriseNameMaxRunes {
		runes = runes[:defaultEnterpriseNameMaxRunes]
	}
	if len(runes) < 2 {
		return defaultEnterpriseNameFallback
	}
	return string(runes)
}

// ensureDefaultEnterprise backfills a default enterprise for an internal user
// that has none（存量孤立账号，票 02 前注册）. Idempotent: a user that gained an
// enterprise meanwhile is returned unchanged. 失败不阻断调用方（登录/鉴权读取照常
// 继续，仅记日志，下次登录再试）。
func (service *IamService) ensureDefaultEnterprise(
	ctx context.Context,
	user base.IAMUser,
) (base.IAMUser, error) {
	if !user.Internal || user.EnterpriseID != "" {
		return user, nil
	}
	enterprise := base.IAMEnterprise{
		Name:        defaultEnterpriseName(user.Username),
		OwnerID:     user.ID,
		MemberCount: 1,
		CreatedAt:   service.now().UTC().UnixMilli(),
	}
	enterpriseID, err := idgen.New()
	if err != nil {
		return user, fmt.Errorf("generate default enterprise id: %w", err)
	}
	enterprise.ID = enterpriseID
	updated, _, err := service.repository.EnsureDefaultEnterprise(ctx, user.ID, enterprise)
	return updated, err
}
