// Package hajimibridge 实现影策与 hajimi 的余额互通（fork 专有）：
// 服务端 HMAC 客户端、划转状态机、用户钱包接口和后台 worker。
package hajimibridge

import (
	"net/url"
	"os"
	"regexp"
	"strings"
)

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Config 由部署者通过环境变量提供；hajimi 地址是可信运维配置，不走用户上游的 SSRF 过滤。
type Config struct {
	Enabled         bool
	BaseURL         string
	KeyID           string
	Secret          string
	ExpectedIdPHost string
	PublicURL       string
	// CostChannelHosts 是毛利报表里"走 hajimi 的系统渠道"的主机名；为空时按 ExpectedIdPHost 及其子域名判断。
	CostChannelHosts []string
	// Problems 只记录缺失或无效的配置项名称，不含任何密钥内容。
	Problems []string
}

// IsCostChannelHost 判断渠道 BaseURL 的主机名是否算作 hajimi 渠道（毛利报表口径）。
func (c Config) IsCostChannelHost(hostname string) bool {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if hostname == "" {
		return false
	}
	if len(c.CostChannelHosts) > 0 {
		for _, host := range c.CostChannelHosts {
			if hostname == host {
				return true
			}
		}
		return false
	}
	expected := c.ExpectedIdPHost
	if host, _, found := strings.Cut(expected, ":"); found {
		expected = host
	}
	return expected != "" && (hostname == expected || strings.HasSuffix(hostname, "."+expected))
}

// CostChannelRule 用于后台展示当前的渠道识别规则。
func (c Config) CostChannelRule() []string {
	if len(c.CostChannelHosts) > 0 {
		return append([]string(nil), c.CostChannelHosts...)
	}
	expected := c.ExpectedIdPHost
	if host, _, found := strings.Cut(expected, ":"); found {
		expected = host
	}
	if expected == "" {
		return []string{}
	}
	return []string{expected, "*." + expected}
}

// Active 表示互通开关已打开且配置完整；配置不全时全部互通能力失败关闭。
func (c Config) Active() bool { return c.Enabled && len(c.Problems) == 0 }

func (c Config) URLHost() string {
	parsed, err := url.Parse(c.BaseURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func LoadConfigFromEnv() Config {
	return ParseConfig(os.Getenv)
}

func ParseConfig(getenv func(string) string) Config {
	config := Config{
		Enabled:         strings.EqualFold(strings.TrimSpace(getenv("HAJIMI_BRIDGE_ENABLED")), "true"),
		KeyID:           strings.TrimSpace(getenv("HAJIMI_BRIDGE_KEY_ID")),
		Secret:          strings.TrimSpace(getenv("HAJIMI_BRIDGE_SECRET")),
		ExpectedIdPHost: strings.ToLower(strings.TrimSuffix(strings.TrimSpace(getenv("HAJIMI_BRIDGE_EXPECTED_IDP_HOST")), ".")),
		PublicURL:       strings.TrimRight(strings.TrimSpace(getenv("HAJIMI_PUBLIC_URL")), "/"),
	}
	if base, ok := normalizeBridgeBaseURL(getenv("HAJIMI_BRIDGE_URL")); ok {
		config.BaseURL = base
	} else {
		config.Problems = append(config.Problems, "HAJIMI_BRIDGE_URL")
	}
	if !keyIDPattern.MatchString(config.KeyID) {
		config.Problems = append(config.Problems, "HAJIMI_BRIDGE_KEY_ID")
	}
	if len(config.Secret) < 32 {
		config.Problems = append(config.Problems, "HAJIMI_BRIDGE_SECRET")
	}
	if config.ExpectedIdPHost == "" || strings.ContainsAny(config.ExpectedIdPHost, "/@ ") {
		config.Problems = append(config.Problems, "HAJIMI_BRIDGE_EXPECTED_IDP_HOST")
	}
	if config.PublicURL != "" {
		if parsed, err := url.Parse(config.PublicURL); err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
			config.Problems = append(config.Problems, "HAJIMI_PUBLIC_URL")
		}
	}
	// 渠道识别只影响报表口径，写错时忽略该项，不关闭互通。
	for _, raw := range strings.Split(getenv("HAJIMI_BRIDGE_COST_CHANNEL_HOSTS"), ",") {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if host == "" || strings.ContainsAny(host, "/@: ") {
			continue
		}
		config.CostChannelHosts = append(config.CostChannelHosts, host)
	}
	return config
}

func normalizeBridgeBaseURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host+parsed.EscapedPath(), "/"), true
}

// idpHostMatches 比较 OAuth 授权地址主机与期望的 hajimi 主机；期望值不带端口时只比较主机名。
func idpHostMatches(expected string, authorizationURL string) bool {
	if expected == "" {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(authorizationURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	actual := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if strings.Contains(expected, ":") {
		actual = strings.ToLower(parsed.Host)
	}
	return actual == expected
}
