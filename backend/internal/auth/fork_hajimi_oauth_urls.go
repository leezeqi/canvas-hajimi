package auth

import (
	"net"
	"net/url"
	"strings"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/outbound"
)

// validateLinuxDOEndpointURLs 校验 OAuth 端点：默认必须是 HTTPS。
// Token / 用户信息地址只有回环主机或 CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS 精确放行的主机可用 HTTP（内部桥接网络、本地联调）；
// 浏览器跳转的授权地址只有回环主机可用 HTTP（本地联调）。
func validateLinuxDOEndpointURLs(value linuxDOSettingValue) error {
	if !validOAuthEndpointURL(value.AuthorizationURL, false) {
		return kernel.BadAuthRequest("Linux.do 授权地址必须是有效的 HTTPS URL，本地回环地址可使用 HTTP")
	}
	for _, rawURL := range []string{value.TokenURL, value.UserInfoURL} {
		if !validOAuthEndpointURL(rawURL, true) {
			return kernel.BadAuthRequest("Linux.do Token 和用户信息地址必须是有效的 HTTPS URL；HTTP 仅限本地回环或已配置的可信内网主机")
		}
	}
	return nil
}

func validOAuthEndpointURL(rawURL string, allowTrustedPrivateHTTP bool) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		host := parsed.Hostname()
		if isLoopbackEndpointHost(host) {
			return true
		}
		return allowTrustedPrivateHTTP && outbound.AllowedPrivateUpstreamHost(host)
	default:
		return false
	}
}

func isLoopbackEndpointHost(host string) bool {
	if isLoopbackOAuthHost(host) {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}
