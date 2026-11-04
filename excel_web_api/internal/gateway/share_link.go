// gateway/share_link.go：分享链接重定向入口 GET /s/{link_token}。
//
// 无现成前端路由约定（web/ 尚未接入 Router），本票约定：
//   - 未登录 → 302 到 /register?link_token=…（复用「持链接注册」入口，token 由注册通道携带）；
//   - 已登录解析成功 → 302 到 /bases/{base_id}；
//   - 无法解析/失效 token → 302 到 /err?kind=share-link-unavailable（引导页，含组件自解释）。
//
// 链接解析无独立 rpc（计数保持 111 不变）：已登录访客命中 token 的当下仅能通过
// 会话 ExchangeSession 返回的 principal.link_context 判定本 token 是否即其持链接
// 注册所得；其余 token 一律走引导页。详见票 10 与实现裁定。
package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	authredis "excel_web_api/redis"
	"excel_web_api/rpc"
)

const (
	shareLinkRegisterPath = "/register"
	shareLinkBasePrefix   = "/bases/"
	shareLinkErrorPath    = "/err?kind=share-link-unavailable"
)

// shareLinkStore caches link_token -> base_id for the share-link entry.
// *authredis.Cache satisfies it；tests inject an in-memory fake.
type shareLinkStore interface {
	ShareLinkBase(context.Context, string) (string, error)
	SetShareLinkBase(context.Context, string, string) error
}

// ShareLinkHandler serves GET /s/{link_token} and redirects the visitor to the
// linked Base, the link-aware register page, or an unavailable guide page.
type ShareLinkHandler struct {
	iam    rpc.IamRPC
	links  shareLinkStore
	logger *slog.Logger
}

// NewShareLinkHandler wires the share-link redirect entry point with the
// real Redis-backed cache.
func NewShareLinkHandler(
	iam rpc.IamRPC,
	cache *authredis.Cache,
	logger *slog.Logger,
) *ShareLinkHandler {
	return newShareLinkHandler(iam, cache, logger)
}

func newShareLinkHandler(
	iam rpc.IamRPC,
	links shareLinkStore,
	logger *slog.Logger,
) *ShareLinkHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ShareLinkHandler{iam: iam, links: links, logger: logger}
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
	handler.storeLink(request.Context(), token, link.GetBaseId())
	http.Redirect(response, request, baseRedirect(link.GetBaseId()), http.StatusFound)
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
