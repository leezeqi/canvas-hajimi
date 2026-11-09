package handler

import (
	"crypto/subtle"
	"log"
	"net/http"
	"net/url"
	"strings"

	"yingce/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// 登录 CSRF 防护：OAuth state 必须和发起登录的浏览器绑定。
// 发起时把 state 写入短期 HttpOnly Cookie，回调时要求 Cookie 与 query 中的 state 一致后才消费 state。
const oauthStateCookieName = "open_ai_canvas_oauth_state"

const oauthStateCookieMaxAge = 600

const oauthStateMismatchMessage = "登录状态已失效，请重新登录"

func oauthCookieSecure(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}

// setOAuthStateCookie 从即将跳转的授权地址里取出 state 并写入 Cookie。
func setOAuthStateCookie(c *gin.Context, authorizeURL string) {
	state := ""
	if parsed, err := url.Parse(authorizeURL); err == nil {
		state = parsed.Query().Get("state")
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   oauthStateCookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   oauthCookieSecure(c),
	})
}

func clearOAuthStateCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   oauthCookieSecure(c),
	})
}

// consumeOAuthStateCookie 总是清除 Cookie，并以常量时间比较 Cookie 与回调 state。
func consumeOAuthStateCookie(c *gin.Context, state string) bool {
	cookie, _ := c.Cookie(oauthStateCookieName)
	clearOAuthStateCookie(c)
	state = strings.TrimSpace(state)
	if cookie == "" || state == "" || len(cookie) != len(state) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(state)) == 1
}

// redirectOAuthProviderError 处理授权方回传的 error（例如用户在 hajimi 同意页点“取消”得到 access_denied）：
// 不消费 state、不调用 Token 接口，直接回到登录页并给出明确提示；重新验证流程回到资料页。
// 提示文案固定，不回显授权方传来的任何内容。
func redirectOAuthProviderError(c *gin.Context, svc *service.Service, reauthentication bool) bool {
	providerError := strings.TrimSpace(c.Query("error"))
	if providerError == "" {
		return false
	}
	if reauthentication {
		c.Redirect(http.StatusFound, "/settings?section=profile&reauth=error")
		return true
	}
	message := "hajimi 授权失败，请重试"
	if providerError == "access_denied" {
		message = "已取消 hajimi 授权"
	}
	if err := svc.RecordRejectedLogin("linuxdo", loginRequestContext(c), http.StatusBadRequest); err != nil {
		log.Printf("OAuth rejected login audit write failed: error=%v", err)
	}
	c.Redirect(http.StatusFound, "/login?oauth_error="+url.QueryEscape(message))
	return true
}

// rejectOAuthStateMismatch 在消费 state 之前拒绝回调，沿用原有的错误跳转。
func rejectOAuthStateMismatch(c *gin.Context, svc *service.Service, reauthentication bool) {
	if reauthentication {
		c.Redirect(http.StatusFound, "/settings?section=profile&reauth=error")
		return
	}
	if err := svc.RecordRejectedLogin("linuxdo", loginRequestContext(c), http.StatusBadRequest); err != nil {
		log.Printf("OAuth rejected login audit write failed: error=%v", err)
	}
	c.Redirect(http.StatusFound, "/login?oauth_error="+url.QueryEscape(oauthStateMismatchMessage))
}
