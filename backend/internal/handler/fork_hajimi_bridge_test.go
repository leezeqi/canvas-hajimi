package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func hajimiBridgeRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "bridge.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// MigrateSchema 会在上游迁移之后由 applyForkSchema 建好 hajimi 互通表。
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "bridge-user", Username: "bridge-user", DisplayName: "Bridge", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserIdentity{ID: "bridge-identity", UserID: user.ID, Provider: "linuxdo", Subject: "hajimi:42"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuthSession{ID: "bridge-session", UserID: user.ID, TokenHash: auth.HashToken("bridge-token"), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: "linuxdo_oauth", ValueJSON: `{"enabled":true,"authorizationUrl":"https://hajimi.chat/yingce/authorize","subjectField":"sub"}`}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), dir)
	ConfigureRuntime(svc)
	t.Cleanup(func() { ConfigureRuntime(nil); svc.Close(); sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	router := gin.New()
	RegisterCanvasAPI(router.Group("/api"), svc)
	return router, db
}

func bridgeRequest(router http.Handler, method string, path string, body string, authenticated bool) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "bridge-session.bridge-token"})
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var envelope map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	return rec, envelope
}

func TestHajimiBridgeRoutesRequireLoginAndFailClosedWhenDisabled(t *testing.T) {
	t.Setenv("HAJIMI_BRIDGE_ENABLED", "")
	router, _ := hajimiBridgeRouter(t)
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/hajimi-bridge/wallet", ""},
		{"POST", "/api/hajimi-bridge/transfers", `{}`},
		{"GET", "/api/hajimi-bridge/transfers", ""},
		{"PUT", "/api/hajimi-bridge/auto-topup", `{}`},
		{"GET", "/api/admin/hajimi-bridge/overview", ""},
		{"POST", "/api/admin/hajimi-bridge/transfers/yct_x/retry", ""},
	} {
		if rec, _ := bridgeRequest(router, route.method, route.path, route.body, false); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d", route.method, route.path, rec.Code)
		}
	}
	rec, envelope := bridgeRequest(router, "GET", "/api/hajimi-bridge/wallet", "", true)
	data, _ := envelope["data"].(map[string]any)
	if rec.Code != http.StatusOK || envelope["code"] != float64(0) || data["enabled"] != false || data["linked"] != false || data["hajimiBalanceMicro"] != nil {
		t.Fatalf("disabled wallet = %d %s", rec.Code, rec.Body.String())
	}
	rec, envelope = bridgeRequest(router, "POST", "/api/hajimi-bridge/transfers", `{"direction":"in","amountMicro":5000000,"idempotencyKey":"0123456789abcdef"}`, true)
	if rec.Code != http.StatusForbidden || envelope["reason"] != "hajimi_bridge_disabled" {
		t.Fatalf("disabled transfer = %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = bridgeRequest(router, "GET", "/api/admin/hajimi-bridge/overview", "", true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin overview = %d", rec.Code)
	}
	if !storageReadOnlyExempt(http.MethodPost, "/api/hajimi-bridge/transfers") || !storageReadOnlyExempt(http.MethodPut, "/api/hajimi-bridge/auto-topup") {
		t.Fatal("bridge money routes must stay usable while storage is read-only")
	}
}

func TestHajimiBridgeWalletEnvelopeWhenEnabled(t *testing.T) {
	hajimi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Yingce-Signature") == "" || r.URL.Path != "/internal/yingce/v1/users/42/wallet" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hajimi_user_id":42,"status":"active","grant":"active","canvas_user_id":"bridge-user","balance_micro":12345678,"net_in_micro":0,
			"limits":{"min_micro":1000000,"max_micro":200000000,"user_daily_micro":200000000,"user_daily_used_micro":0,"global_daily_remaining_micro":3000000000}}`))
	}))
	t.Cleanup(hajimi.Close)
	t.Setenv("HAJIMI_BRIDGE_ENABLED", "true")
	t.Setenv("HAJIMI_BRIDGE_URL", hajimi.URL)
	t.Setenv("HAJIMI_BRIDGE_KEY_ID", "k1")
	t.Setenv("HAJIMI_BRIDGE_SECRET", strings.Repeat("s", 40))
	t.Setenv("HAJIMI_BRIDGE_EXPECTED_IDP_HOST", "hajimi.chat")
	t.Setenv("HAJIMI_PUBLIC_URL", "https://hajimi.chat")
	router, _ := hajimiBridgeRouter(t)
	rec, envelope := bridgeRequest(router, "GET", "/api/hajimi-bridge/wallet", "", true)
	data, _ := envelope["data"].(map[string]any)
	limits, _ := data["limits"].(map[string]any)
	if rec.Code != http.StatusOK || data["enabled"] != true || data["linked"] != true || data["reachable"] != true || data["hajimiBalanceMicro"] != float64(12345678) ||
		data["grant"] != "active" || limits["minMicro"] != float64(1000000) || data["hajimiPublicUrl"] != "https://hajimi.chat" || data["pendingCount"] != float64(0) {
		t.Fatalf("wallet = %d %s", rec.Code, rec.Body.String())
	}
	autoTopup, _ := data["autoTopup"].(map[string]any)
	if autoTopup["enabled"] != false || autoTopup["thresholdMicro"] != float64(1000000) || autoTopup["amountMicro"] != float64(5000000) || autoTopup["dailyLimitMicro"] != float64(20000000) {
		t.Fatalf("autoTopup defaults = %#v", autoTopup)
	}
	rec, envelope = bridgeRequest(router, "GET", "/api/hajimi-bridge/transfers?page=1&pageSize=20", "", true)
	page, _ := envelope["data"].(map[string]any)
	if rec.Code != http.StatusOK || page["total"] != float64(0) {
		t.Fatalf("transfers = %d %s", rec.Code, rec.Body.String())
	}
	rec, envelope = bridgeRequest(router, "POST", "/api/hajimi-bridge/transfers", `{"direction":"in","amountMicro":5000000,"idempotencyKey":"bad"}`, true)
	if rec.Code != http.StatusBadRequest || envelope["reason"] != "invalid_argument" {
		t.Fatalf("invalid key = %d %s", rec.Code, rec.Body.String())
	}
}
