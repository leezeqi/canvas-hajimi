package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/auth"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

type oauthStateFixture struct {
	router *gin.Engine
	db     *gorm.DB
}

func newOAuthStateFixture(t *testing.T) *oauthStateFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if user, pass, ok := r.BasicAuth(); !ok || user != "yingce" || pass != "client-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"access-token","token_type":"Bearer","expires_in":300}`))
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]any{"sub": "hajimi:42", "username": "alice_42", "name": "alice"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(idp.Close)
	// OAuth Token/用户信息请求仍经过出站 SSRF 校验：本地联调需要显式放行回环主机。
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	dir := t.TempDir()
	// 预置设置加密密钥：Windows 上首次生成密钥时同步目录会失败，与本测试无关。
	if err := os.WriteFile(filepath.Join(dir, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "fixture.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "oauth-admin", Username: "admin", DisplayName: "Admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	member := &model.User{ID: "oauth-member", Username: "alice_42", DisplayName: "alice", Role: model.UserRoleUser, Status: model.UserStatusActive}
	for _, user := range []*model.User{admin, member} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.UserIdentity{ID: "oauth-identity", UserID: member.ID, Provider: "linuxdo", Subject: "hajimi:42"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), dir)
	ConfigureRuntime(svc)
	t.Cleanup(func() { ConfigureRuntime(nil); svc.Close(); sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if _, err := svc.UpdateLinuxDOSetting(admin, service.LinuxDOSettingRequest{
		Enabled: true, ClientID: "yingce", ClientSecret: "client-secret",
		AuthorizationURL: idp.URL + "/authorize", TokenURL: idp.URL + "/token", UserInfoURL: idp.URL + "/userinfo",
		RedirectURL: "http://127.0.0.1/oauth/linuxdo/callback", Scopes: []string{"profile"}, ClientAuthMethod: "client_secret_basic",
		SubjectField: "sub", UsernameField: "username", DisplayNameField: "name", EmailField: "email", AvatarField: "avatar_url",
	}); err != nil {
		t.Fatalf("configure oauth: %v", err)
	}
	router := gin.New()
	RegisterAuthRoutes(router.Group("/api"), svc)
	RegisterOAuthCallbackRoutes(router, svc)
	return &oauthStateFixture{router: router, db: db}
}

func (f *oauthStateFixture) do(method string, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func responseCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func (f *oauthStateFixture) start(t *testing.T, headers map[string]string) (string, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/linuxdo/start?next=/create", nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("start status = %d body=%s", rec.Code, rec.Body.String())
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")
	cookie := responseCookie(rec, oauthStateCookieName)
	if state == "" || cookie == nil || cookie.Value != state {
		t.Fatalf("state=%q cookie=%#v", state, cookie)
	}
	return state, cookie
}

func assertStateCookieCleared(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	cookie := responseCookie(rec, oauthStateCookieName)
	if cookie == nil || cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("state cookie not cleared: %#v", cookie)
	}
}

func TestOAuthStartBindsStateCookie(t *testing.T) {
	fixture := newOAuthStateFixture(t)
	_, cookie := fixture.start(t, nil)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 600 || cookie.Secure {
		t.Fatalf("cookie attributes = %#v", cookie)
	}
	_, secure := fixture.start(t, map[string]string{"X-Forwarded-Proto": "https"})
	if !secure.Secure {
		t.Fatal("state cookie must be Secure behind HTTPS like the session cookie")
	}
}

func TestOAuthCallbackRequiresMatchingStateCookie(t *testing.T) {
	fixture := newOAuthStateFixture(t)
	state, cookie := fixture.start(t, nil)
	callback := "/oauth/linuxdo/callback?code=auth-code&state=" + url.QueryEscape(state)

	missing := fixture.do(http.MethodGet, callback)
	if missing.Code != http.StatusFound || !strings.HasPrefix(missing.Header().Get("Location"), "/login?oauth_error=") ||
		!strings.Contains(missing.Header().Get("Location"), url.QueryEscape(oauthStateMismatchMessage)) {
		t.Fatalf("missing cookie: %d %s", missing.Code, missing.Header().Get("Location"))
	}
	assertStateCookieCleared(t, missing)
	if responseCookie(missing, auth.SessionCookieName) != nil {
		t.Fatal("missing cookie must not create a session")
	}

	mismatched := fixture.do(http.MethodGet, "/api/auth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state), &http.Cookie{Name: oauthStateCookieName, Value: strings.Repeat("0", len(state))})
	if mismatched.Code != http.StatusFound || !strings.Contains(mismatched.Header().Get("Location"), url.QueryEscape(oauthStateMismatchMessage)) {
		t.Fatalf("mismatched cookie: %d %s", mismatched.Code, mismatched.Header().Get("Location"))
	}
	assertStateCookieCleared(t, mismatched)
	var rejected int64
	fixture.db.Model(&model.LoginLog{}).Where("method = ? AND result <> ?", "linuxdo", "success").Count(&rejected)
	if rejected < 2 {
		t.Fatalf("rejected callbacks must be audited, got %d", rejected)
	}

	// 前两次拒绝发生在消费 state 之前，合法浏览器仍能完成登录。
	matched := fixture.do(http.MethodGet, callback, cookie)
	if matched.Code != http.StatusFound || matched.Header().Get("Location") != "/create" {
		t.Fatalf("matched cookie: %d %s", matched.Code, matched.Header().Get("Location"))
	}
	assertStateCookieCleared(t, matched)
	if session := responseCookie(matched, auth.SessionCookieName); session == nil || session.Value == "" {
		t.Fatal("matched callback must create a session")
	}
	var sessions int64
	fixture.db.Model(&model.AuthSession{}).Where("user_id = ?", "oauth-member").Count(&sessions)
	if sessions != 1 {
		t.Fatalf("sessions = %d", sessions)
	}
	// state 已被消费，重放同一回调失败。
	replay := fixture.do(http.MethodGet, callback, cookie)
	if !strings.HasPrefix(replay.Header().Get("Location"), "/login?oauth_error=") {
		t.Fatalf("replayed callback: %s", replay.Header().Get("Location"))
	}
}

func TestOAuthReauthenticationCallbackRejectsMissingStateCookie(t *testing.T) {
	fixture := newOAuthStateFixture(t)
	hash := auth.HashToken("member-token")
	if err := fixture.db.Create(&model.AuthSession{ID: "member-session", UserID: "oauth-member", TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: auth.SessionCookieName, Value: "member-session.member-token"}
	start := fixture.do(http.MethodGet, "/api/auth/profile/linuxdo/start", session)
	if start.Code != http.StatusFound {
		t.Fatalf("reauth start = %d %s", start.Code, start.Body.String())
	}
	location, _ := url.Parse(start.Header().Get("Location"))
	state := location.Query().Get("state")
	stateCookie := responseCookie(start, oauthStateCookieName)
	if stateCookie == nil || stateCookie.Value != state {
		t.Fatalf("reauth state cookie = %#v", stateCookie)
	}
	rejected := fixture.do(http.MethodGet, "/oauth/linuxdo/callback?code=c&state="+url.QueryEscape(state), session)
	if rejected.Header().Get("Location") != "/settings?section=profile&reauth=error" {
		t.Fatalf("reauth without state cookie: %s", rejected.Header().Get("Location"))
	}
	assertStateCookieCleared(t, rejected)
	accepted := fixture.do(http.MethodGet, "/oauth/linuxdo/callback?code=c&state="+url.QueryEscape(state), session, stateCookie)
	if accepted.Header().Get("Location") != "/settings?section=profile&reauth=success" {
		t.Fatalf("reauth with state cookie: %s", accepted.Header().Get("Location"))
	}
}

// 用户在 hajimi 同意页点“取消”：回到登录页给出明确提示，清除 state Cookie，且不消费 state。
func TestOAuthCallbackProviderErrorRedirectsWithClearMessage(t *testing.T) {
	fixture := newOAuthStateFixture(t)
	state, cookie := fixture.start(t, nil)
	cases := map[string]string{"access_denied": "已取消 hajimi 授权", "server_error": "hajimi 授权失败，请重试"}
	for providerError, message := range cases {
		rec := fixture.do(http.MethodGet, "/oauth/linuxdo/callback?error="+providerError+"&state="+url.QueryEscape(state), cookie)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?oauth_error="+url.QueryEscape(message) {
			t.Fatalf("%s: %d %s", providerError, rec.Code, rec.Header().Get("Location"))
		}
		assertStateCookieCleared(t, rec)
		if responseCookie(rec, auth.SessionCookieName) != nil {
			t.Fatalf("%s must not create a session", providerError)
		}
	}
	// state 未被消费：同一浏览器随后完成授权仍可登录。
	matched := fixture.do(http.MethodGet, "/oauth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state), cookie)
	if matched.Code != http.StatusFound || matched.Header().Get("Location") != "/create" || responseCookie(matched, auth.SessionCookieName) == nil {
		t.Fatalf("login after cancel: %d %s", matched.Code, matched.Header().Get("Location"))
	}
}
