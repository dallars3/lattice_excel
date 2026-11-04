// share/update_link.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"excel-libs/idgen"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// UpdateLink changes the scope and role while preserving the URL when reopened.
func (service *Service) UpdateLink(
	ctx context.Context,
	request *sharev1.UpdateLinkRequest,
) (*sharev1.UpdateLinkResponse, error) {
	if request == nil || strings.TrimSpace(request.GetBaseId()) == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	scope, ok := linkScopeName(request.GetScope())
	if !ok {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	role := normalizeRole(request.GetRole().String())
	enabled := scope != "none"
	if enabled && role != "viewer" && role != "filler" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if !enabled && request.GetRole() != sharev1.Role_ROLE_UNSPECIFIED &&
		role != "viewer" && role != "filler" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := service.requireAction(
		ctx,
		request.GetBaseId(),
		sharev1.Action_ACTION_MANAGE_SHARE,
	)
	if err != nil {
		return nil, err
	}
	enterpriseID, err := service.bases.EnterpriseID(ctx, request.GetBaseId())
	if err != nil || enterpriseID == "" {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()

	link, err := service.repository.LinkByBaseIDForUpdate(ctx, tx, request.GetBaseId())
	oldLink := link
	switch {
	case errors.Is(err, sql.ErrNoRows):
		link, err = service.newLink(request.GetBaseId())
	case err != nil:
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	link.Scope = scope
	link.Enabled = enabled
	if role != "" {
		link.Role = role
	} else if link.Role == "" {
		link.Role = "viewer"
	}
	closureReason := linkClosureReason(oldLink, link)
	changed := oldLink.BaseID == "" ||
		oldLink.Scope != link.Scope ||
		oldLink.Role != link.Role
	link.UpdatedAt = service.now().UTC().UnixMilli()
	if err := service.repository.UpsertLink(ctx, tx, link); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if closureReason != "" {
		if _, err := service.enqueueLinkClosed(ctx, tx, linkClosedPayload{
			BaseID:       link.BaseID,
			LinkID:       link.BaseID,
			EnterpriseID: enterpriseID,
			Operator:     principal.GetUserId(),
			Reason:       closureReason,
		}); err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
	}
	if changed {
		if err := service.enqueueShareChanged(
			ctx,
			tx,
			link.BaseID,
			enterpriseID,
			"link",
			principal.GetUserId(),
			link.Role,
		); err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &sharev1.UpdateLinkResponse{LinkConfig: protoLinkConfig(link)}, nil
}

func linkClosureReason(before, after base.ShareLink) string {
	if !before.Enabled {
		return ""
	}
	if !after.Enabled {
		return "closed"
	}
	if before.Scope == "anyone" && after.Scope == "enterprise" {
		return "narrowing"
	}
	return ""
}

func (service *Service) newLink(baseID string) (base.ShareLink, error) {
	token, err := idgen.New()
	if err != nil {
		return base.ShareLink{}, err
	}
	return base.ShareLink{
		BaseID: baseID,
		Token:  token,
		Scope:  "none",
		Role:   "viewer",
		URL:    "/s/" + token,
	}, nil
}

func linkScopeName(scope sharev1.LinkScope) (string, bool) {
	switch scope {
	case sharev1.LinkScope_LINK_SCOPE_NONE:
		return "none", true
	case sharev1.LinkScope_LINK_SCOPE_ENTERPRISE:
		return "enterprise", true
	case sharev1.LinkScope_LINK_SCOPE_ANYONE:
		return "anyone", true
	default:
		return "", false
	}
}

func protoLinkConfig(link base.ShareLink) *sharev1.LinkConfig {
	return &sharev1.LinkConfig{
		Enabled:   link.Enabled,
		Scope:     protoLinkScope(link.Scope),
		Role:      protoRole(link.Role),
		Url:       link.URL,
		UpdatedAt: link.UpdatedAt,
	}
}

func protoLinkScope(scope string) sharev1.LinkScope {
	switch scope {
	case "none":
		return sharev1.LinkScope_LINK_SCOPE_NONE
	case "enterprise":
		return sharev1.LinkScope_LINK_SCOPE_ENTERPRISE
	case "anyone":
		return sharev1.LinkScope_LINK_SCOPE_ANYONE
	default:
		return sharev1.LinkScope_LINK_SCOPE_UNSPECIFIED
	}
}
