// gateway/share_link.go：分享链接重定向入口 GET /s/{link_token}。
//
// 前端规范路由约定：
//   - 未登录 → 302 到 /register?link_token=…（复用「持链接注册」入口，token 由注册通道携带）；
//   - 已登录解析成功 → 302 到 /base/{base_id}（与前端 /base/:id 对齐，H-95）；
//   - 无法解析/失效 token → 302 到 /err?kind=share-link-unavailable（失效页，不回退首页）。
//
// 链接解析无独立 rpc（计数保持 111 不变）：已登录访客先命中 55s 的 token→Base
// 短期缓存；缓存未命中时回源 core.share.CheckAccess 校验链接当前仍有效，失效
// token 必须落到失效页（重置/关闭后缓存已被清除）。
package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	authredis "excel_web_api/redis"
	"excel_web_api/rpc"
)

const (
	shareLinkRegisterPath = "/register"
	shareLinkBasePrefix   = "/base/"
	shareLinkErrorPath    = "/err?kind=share-link-unavailable"
)

// shareLinkStore caches link_token -> base_id for the share-link entry.
// *authredis.Cache satisfies it；tests inject an in-memory fake.
type shareLinkStore interface {
	ShareLinkBase(context.Context, string) (string, error)
	SetShareLinkBase(context.Context, string, string) error
	DeleteShareLinkBase(context.Context, string) error
}

// ShareLinkHandler serves GET /s/{link_token} and redirects the visitor to the
// linked Base, the link-aware register page, or an unavailable guide page.
type ShareLinkHandler struct {
	iam    rpc.IamRPC
	share  rpc.ShareRPC
	links  shareLinkStore
	logger *slog.Logger
}

// NewShareLinkHandler wires the share-link redirect entry point with the
// real Redis-backed cache and the core share verifier.
func NewShareLinkHandler(
	iam rpc.IamRPC,
	share rpc.ShareRPC,
	cache *authredis.Cache,
	logger *slog.Logger,
) *ShareLinkHandler {
	return newShareLinkHandler(iam, share, cache, logger)
}

func newShareLinkHandler(
	iam rpc.IamRPC,
	share rpc.ShareRPC,
	links shareLinkStore,
	logger *slog.Logger,
) *ShareLinkHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ShareLinkHandler{iam: iam, share: share, links: links, logger: logger}
}

func (handler *ShareLinkHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	token := strings.TrimSpace(request.PathValue("link_token"))
	if token == "" {
		http.Redirect(response, request, shareLinkErrorPath, http.StatusFound)
		return
	}

	session := sessionToken(request)
	if session == "" {
		http.Redirect(response, request, registerRedirect(token), http.StatusFound)
		return
	}

	if baseID, ok := handler.resolveCached(request.Context(), token); ok {
		http.Redirect(response, request, baseRedirect(baseID), http.StatusFound)
		return
	}

	principal, err := handler.resolveSession(request.Context(), session)
	if err != nil {
		handler.logger.Warn("share link session resolution failed; redirecting to guide page",
			"error", err)
		http.Redirect(response, request, shareLinkErrorPath, http.StatusFound)
		return
	}
	if principal == nil {
		http.Redirect(response, request, shareLinkErrorPath, http.StatusFound)
		return
	}

	link := principal.GetLinkContext()
	if link == nil || link.GetLinkToken() != token || strings.TrimSpace(link.GetBaseId()) == "" {
		http.Redirect(response, request, shareLinkErrorPath, http.StatusFound)
		return
	}
	if !handler.verifyLink(request.Context(), principal, link) {
		http.Redirect(response, request, shareLinkErrorPath, http.StatusFound)
		return
	}
	handler.storeLink(request.Context(), token, link.GetBaseId())
	http.Redirect(response, request, baseRedirect(link.GetBaseId()), http.StatusFound)
}

// verifyLink 在缓存未命中时回源 core 校验链接当前仍有效（未重置/关闭）。
// 旧 token 校验失败必须落到失效页，不能再凭账号上残留的 link_context 放行。
func (handler *ShareLinkHandler) verifyLink(
	ctx context.Context,
	principal *commonv1.Principal,
	link *commonv1.LinkContext,
) bool {
	if handler.share == nil {
		return false
	}
	decision, err := handler.share.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal:   principal,
		BaseId:      link.GetBaseId(),
		Action:      sharev1.Action_ACTION_VIEW,
		LinkContext: link,
	})
	if err != nil {
		handler.logger.Warn("share link verification failed; redirecting to guide page",
			"error", err)
		return false
	}
	return decision.GetAllow()
}

func (handler *ShareLinkHandler) resolveCached(ctx context.Context, token string) (string, bool) {
	if handler.links == nil {
		return "", false
	}
	baseID, err := handler.links.ShareLinkBase(ctx, token)
	if err != nil {
		handler.logger.Warn("share link cache unavailable; falling back to ExchangeSession",
			"error", err)
		return "", false
	}
	baseID = strings.TrimSpace(baseID)
	return baseID, baseID != ""
}

func (handler *ShareLinkHandler) resolveSession(
	ctx context.Context,
	token string,
) (*commonv1.Principal, error) {
	if handler.iam == nil {
		return nil, nil
	}
	response, err := handler.iam.ExchangeSession(ctx, &iamv1.ExchangeSessionRequest{Token: token})
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetPrincipal() == nil || response.GetPrincipal().GetUserId() == "" {
		return nil, nil
	}
	return response.GetPrincipal(), nil
}

func (handler *ShareLinkHandler) storeLink(ctx context.Context, token, baseID string) {
	if handler.links == nil {
		return
	}
	if err := handler.links.SetShareLinkBase(ctx, token, baseID); err != nil {
		handler.logger.Warn("write share link cache failed", "error", err)
	}
}

func registerRedirect(token string) string {
	return shareLinkRegisterPath + "?link_token=" + url.QueryEscape(token)
}

func baseRedirect(baseID string) string {
	return shareLinkBasePrefix + baseID
}
