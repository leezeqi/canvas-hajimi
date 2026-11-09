package auth

import "testing"

func TestLinuxDOEndpointURLValidation(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "hajimi-bridge, internal.example")
	base := linuxDOSettingValue{
		Enabled: true, ClientID: "yingce", ClientSecret: "secret",
		AuthorizationURL: "https://hajimi.chat/yingce/authorize",
		TokenURL:         "https://hajimi.chat/api/v1/yingce/oauth/token",
		UserInfoURL:      "https://hajimi.chat/api/v1/yingce/oauth/userinfo",
		RedirectURL:      "https://u-gatewayapi.asia/oauth/linuxdo/callback",
		ClientAuthMethod: "client_secret_basic",
	}
	cases := []struct {
		name   string
		mutate func(*linuxDOSettingValue)
		ok     bool
	}{
		{"all https", func(*linuxDOSettingValue) {}, true},
		{"token http allow-listed bridge host", func(v *linuxDOSettingValue) {
			v.TokenURL = "http://hajimi-bridge:8080/api/v1/yingce/oauth/token"
			v.UserInfoURL = "http://HAJIMI-BRIDGE:8080/api/v1/yingce/oauth/userinfo"
		}, true},
		{"token http loopback", func(v *linuxDOSettingValue) { v.TokenURL = "http://127.0.0.1:9000/token" }, true},
		{"userinfo http localhost", func(v *linuxDOSettingValue) { v.UserInfoURL = "http://localhost:9000/userinfo" }, true},
		{"userinfo http ipv6 loopback", func(v *linuxDOSettingValue) { v.UserInfoURL = "http://[::1]:9000/userinfo" }, true},
		{"authorize http loopback", func(v *linuxDOSettingValue) { v.AuthorizationURL = "http://127.0.0.1:5173/yingce/authorize" }, true},
		{"token http public host", func(v *linuxDOSettingValue) { v.TokenURL = "http://hajimi.chat/token" }, false},
		{"token http unlisted private host", func(v *linuxDOSettingValue) { v.TokenURL = "http://10.0.0.5/token" }, false},
		{"token http allow-list suffix trick", func(v *linuxDOSettingValue) { v.TokenURL = "http://hajimi-bridge.evil.example/token" }, false},
		{"authorize http allow-listed host is browser-facing", func(v *linuxDOSettingValue) { v.AuthorizationURL = "http://hajimi-bridge:8080/yingce/authorize" }, false},
		{"authorize http public", func(v *linuxDOSettingValue) { v.AuthorizationURL = "http://hajimi.chat/yingce/authorize" }, false},
		{"userinfo ftp", func(v *linuxDOSettingValue) { v.UserInfoURL = "ftp://hajimi.chat/userinfo" }, false},
		{"token missing host", func(v *linuxDOSettingValue) { v.TokenURL = "https:///token" }, false},
		{"token with credentials", func(v *linuxDOSettingValue) { v.TokenURL = "https://user:pass@hajimi.chat/token" }, false},
	}
	for _, item := range cases {
		value := base
		item.mutate(&value)
		err := validateLinuxDOSetting(value)
		if (err == nil) != item.ok {
			t.Errorf("%s: err = %v", item.name, err)
		}
	}
}
