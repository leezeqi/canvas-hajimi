package hajimibridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 固定向量由独立实现（Node crypto）计算，用于和 hajimi 端互相校验 §3.2 的签名格式。
func TestSignMatchesFixedVector(t *testing.T) {
	secret := []byte("test-secret-0123456789abcdef0123456789")
	body := []byte(`{"transfer_id":"yct_0123456789abcdef0123456789abcdef","amount_micro":5000000}`)
	got := Sign(secret, "post", "/internal/yingce/v1/transfers?after_seq=7&limit=200", "1767225600", "Nonce0123456789abcdefABCDEF", body)
	if got != "8d88a74d7037fc5109f25b107418a1f409053bae0b82223018b1da96e1171506" {
		t.Fatalf("POST signature = %s", got)
	}
	got = Sign(secret, "GET", "/internal/yingce/v1/reconcile/summary", "1767225600", "Nonce0123456789abcdefABCDEF", nil)
	if got != "7b81d9f80daafad64d6800ca24961c56d4898df6c46a0d85240af9125960effa" {
		t.Fatalf("GET signature = %s", got)
	}
	signing := SigningString("GET", "/x", "1", "n", nil)
	if strings.HasSuffix(signing, "\n") || !strings.HasSuffix(signing, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855") {
		t.Fatalf("signing string = %q", signing)
	}
}

func TestClientSignsExactRequestURIWithFreshNonce(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1767225600, 0)}
	fake := newFakeHajimi(t, clock.Now)
	client := NewClient(Config{BaseURL: fake.server.URL, KeyID: fake.keyID, Secret: fake.secret}, clock.Now)
	for range 2 {
		if _, err := client.ListTransfers(context.Background(), 7, 200); err != nil {
			t.Fatal(err)
		}
	}
	if fake.authFailures != 0 {
		t.Fatalf("auth failures = %d", fake.authFailures)
	}
	if got := fake.requests[0]; got != "GET /internal/yingce/v1/transfers?after_seq=7&limit=200" {
		t.Fatalf("request line = %q", got)
	}
	if len(fake.nonces) != 2 {
		t.Fatalf("nonces = %d, want fresh nonce per request", len(fake.nonces))
	}
	// 错误密钥必须被拒绝。
	bad := NewClient(Config{BaseURL: fake.server.URL, KeyID: fake.keyID, Secret: strings.Repeat("x", 40)}, clock.Now)
	if _, err := bad.ReconcileSummary(context.Background()); err == nil || !isRemoteCode(err, http.StatusUnauthorized, "unauthorized") {
		t.Fatalf("bad secret err = %v", err)
	}
	// 时间偏差超过 120 秒必须被拒绝。
	skewed := NewClient(Config{BaseURL: fake.server.URL, KeyID: fake.keyID, Secret: fake.secret}, func() time.Time { return clock.Now().Add(-121 * time.Second) })
	if _, err := skewed.ReconcileSummary(context.Background()); err == nil {
		t.Fatal("skewed timestamp accepted")
	}
}

func TestCircuitBreakerOpensAfterFiveFailures(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1767225600, 0)}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	client := NewClient(Config{BaseURL: server.URL, KeyID: "k", Secret: strings.Repeat("s", 40)}, clock.Now)
	for range 5 {
		if _, err := client.ReconcileSummary(context.Background()); err == nil {
			t.Fatal("expected failure")
		}
	}
	if _, err := client.ReconcileSummary(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("breaker should be open, err = %v", err)
	}
	if calls.Load() != 5 {
		t.Fatalf("calls = %d, breaker must not send while open", calls.Load())
	}
	clock.Advance(31 * time.Second)
	if _, err := client.ReconcileSummary(context.Background()); errors.Is(err, ErrUnavailable) {
		t.Fatal("breaker should half-open after 30s")
	}
	if calls.Load() != 6 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestParseConfigFailsClosed(t *testing.T) {
	env := map[string]string{
		"HAJIMI_BRIDGE_ENABLED": "true", "HAJIMI_BRIDGE_URL": "http://hajimi-bridge:8080/", "HAJIMI_BRIDGE_KEY_ID": "k1",
		"HAJIMI_BRIDGE_SECRET": strings.Repeat("s", 32), "HAJIMI_BRIDGE_EXPECTED_IDP_HOST": "Hajimi.Chat", "HAJIMI_PUBLIC_URL": "https://hajimi.chat/",
	}
	config := ParseConfig(func(key string) string { return env[key] })
	if !config.Active() || config.BaseURL != "http://hajimi-bridge:8080" || config.ExpectedIdPHost != "hajimi.chat" || config.PublicURL != "https://hajimi.chat" || config.URLHost() != "hajimi-bridge:8080" {
		t.Fatalf("config = %#v", config)
	}
	env["HAJIMI_BRIDGE_SECRET"] = "short"
	env["HAJIMI_BRIDGE_URL"] = "ftp://x"
	config = ParseConfig(func(key string) string { return env[key] })
	if config.Active() || len(config.Problems) != 2 {
		t.Fatalf("misconfigured bridge must stay off: %#v", config.Problems)
	}
	for _, problem := range config.Problems {
		if strings.Contains(problem, "short") {
			t.Fatal("problems must not contain secrets")
		}
	}
}

func TestParseHajimiSubjectIsStrict(t *testing.T) {
	valid := map[string]int64{"hajimi:1": 1, "hajimi:42": 42, "hajimi:9223372036854775807": 9223372036854775807}
	for subject, want := range valid {
		if got, ok := ParseHajimiSubject(subject); !ok || got != want {
			t.Errorf("%q => %d %v", subject, got, ok)
		}
	}
	for _, subject := range []string{"", "hajimi:", "hajimi:0", "hajimi:007", "hajimi:-1", "hajimi:+1", "hajimi: 1", "hajimi:1 ", "hajimi:1a", "HAJIMI:1", "linuxdo:1", "hajimi:9223372036854775808", "hajimi:1.0"} {
		if _, ok := ParseHajimiSubject(subject); ok {
			t.Errorf("%q must be rejected", subject)
		}
	}
}

func TestIdPHostMatching(t *testing.T) {
	cases := []struct {
		expected, url string
		want          bool
	}{
		{"hajimi.chat", "https://hajimi.chat/yingce/authorize", true},
		{"hajimi.chat", "https://HAJIMI.chat./yingce/authorize", true},
		{"hajimi.chat", "https://hajimi.chat:8443/yingce/authorize", true},
		{"hajimi.chat:8443", "https://hajimi.chat/yingce/authorize", false},
		{"hajimi.chat", "https://evil.example/yingce/authorize", false},
		{"hajimi.chat", "https://hajimi.chat.evil.example/", false},
		{"hajimi.chat", "", false},
		{"", "https://hajimi.chat/", false},
	}
	for _, item := range cases {
		if got := idpHostMatches(item.expected, item.url); got != item.want {
			t.Errorf("idpHostMatches(%q, %q) = %v", item.expected, item.url, got)
		}
	}
}

func TestLinkedUserDetection(t *testing.T) {
	env := newTestEnv(t)
	id, linked, err := env.svc.linkedHajimiUser(env.user.ID)
	if err != nil || !linked || id != testHajimiUserID {
		t.Fatalf("linked user = %d %v %v", id, linked, err)
	}
	if _, linked, _ := env.svc.linkedHajimiUser(env.admin.ID); linked {
		t.Fatal("user without hajimi identity must not be linked")
	}
	invalid := env.createUser(t, "canvas-user-invalid", "bob", "user", "hajimi:0042")
	if _, linked, _ := env.svc.linkedHajimiUser(invalid.ID); linked {
		t.Fatal("non-canonical subject must not be linked")
	}
	linuxDOUser := env.createUser(t, "canvas-user-linuxdo", "carol", "user", "12345")
	if _, linked, _ := env.svc.linkedHajimiUser(linuxDOUser.ID); linked {
		t.Fatal("plain linuxdo subject must not be linked")
	}
	// 通用 OAuth 槽位被改指到其他主机后，所有关联判断失败关闭。
	env.setIdPAuthorizationURL(t, "https://connect.linux.do/oauth2/authorize")
	if _, linked, _ := env.svc.linkedHajimiUser(env.user.ID); linked {
		t.Fatal("IdP host guard must reject repointed slot")
	}
	wallet, err := env.svc.Wallet(context.Background(), env.user)
	if err != nil || !wallet.Enabled || wallet.Linked || wallet.Reachable || wallet.HajimiBalanceMicro != nil {
		t.Fatalf("wallet with repointed IdP = %#v err=%v", wallet, err)
	}
}

// 管理员切换报表条件会取消进行中的请求；报表读取和被取消的请求都不能让熔断影响用户划转。
func TestCircuitBreakerIgnoresCancelledAndReportCalls(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1767225600, 0)}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	client := NewClient(Config{BaseURL: server.URL, KeyID: "k", Secret: strings.Repeat("s", 40)}, clock.Now)

	for range 8 {
		if _, err := client.CostDaily(context.Background(), "2026-11-01", "2026-11-07", 480); err == nil {
			t.Fatal("expected failure")
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for range 8 {
		if _, err := client.ReconcileSummary(cancelled); err == nil {
			t.Fatal("expected cancellation error")
		}
	}
	if !client.Available() {
		t.Fatal("report reads and cancelled calls must not open the breaker")
	}
	before := calls.Load()
	if _, err := client.ReconcileSummary(context.Background()); errors.Is(err, ErrUnavailable) {
		t.Fatal("a real call must still reach hajimi")
	}
	if calls.Load() != before+1 {
		t.Fatalf("calls = %d, want %d", calls.Load(), before+1)
	}

	// Real failures still trip it, and an open breaker also blocks report reads.
	for range 4 {
		_, _ = client.ReconcileSummary(context.Background())
	}
	if client.Available() {
		t.Fatal("five real failures must open the breaker")
	}
	before = calls.Load()
	if _, err := client.CostDaily(context.Background(), "2026-11-01", "2026-11-07", 480); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("report read while open: err = %v", err)
	}
	if calls.Load() != before {
		t.Fatal("no request may be sent while the breaker is open")
	}
}
