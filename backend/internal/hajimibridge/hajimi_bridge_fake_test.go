package hajimibridge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// fakeHajimi 按合同 §3 实现 hajimi 内部桥接接口，并校验 §3.2 的 HMAC 签名。
type fakeHajimi struct {
	t      *testing.T
	mu     sync.Mutex
	keyID  string
	secret string
	now    func() time.Time
	server *httptest.Server

	nonces    map[string]bool
	users     map[int64]*fakeUser
	transfers map[string]*RemoteTransfer
	prints    map[string]string
	seq       int64
	limits    RemoteLimits
	global    int64

	failNext          int  // 接下来 N 个请求直接 500
	commitThenFail    int  // 接下来 N 个转账 POST 正常落库，但响应 502（模拟响应丢失）
	bridgeDisabled    bool // 整个桥接返回 404（关闭状态）
	failTransferPosts bool // 转账 POST 一律 500，且不落库
	// lookupOverride 非空时，查询单笔划转的接口改为返回 404 + 该正文（模拟路由不存在或非 JSON 404）。
	lookupOverride   string
	authFailures     int
	requests         []string
	transferPostSeen int
	// cost 为 nil 时成本报表返回 configured=false。
	cost        *RemoteCostReport
	costQueries []string
}

type fakeUser struct {
	status       string
	grant        string
	canvasUserID string
	balance      int64
	epoch        string
}

var fakeNoncePattern = regexp.MustCompile(`^[A-Za-z0-9]{16,64}$`)

func newFakeHajimi(t *testing.T, now func() time.Time) *fakeHajimi {
	fake := &fakeHajimi{
		t: t, keyID: "canvas-k1", secret: strings.Repeat("s", 40), now: now,
		nonces: map[string]bool{}, users: map[int64]*fakeUser{}, transfers: map[string]*RemoteTransfer{}, prints: map[string]string{},
		limits: RemoteLimits{MinMicro: 1_000_000, MaxMicro: 200_000_000, UserDailyMicro: 200_000_000}, global: 3_000_000_000,
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeHajimi) addUser(id int64, user fakeUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := user
	f.users[id] = &copied
}

func (f *fakeHajimi) user(id int64) *fakeUser {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[id]
}

func (f *fakeHajimi) setUser(id int64, update func(*fakeUser)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	update(f.users[id])
}

func (f *fakeHajimi) set(update func(*fakeHajimi)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	update(f)
}

// injectCommitted 直接在 hajimi 侧落一笔已提交划转（模拟本地缺记录或本地状态矛盾）。
func (f *fakeHajimi) injectCommitted(item RemoteTransfer) RemoteTransfer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	now := f.now().UTC().Format(time.RFC3339)
	item.Status, item.Seq, item.CreatedAt, item.CommittedAt = remoteStatusCommitted, f.seq, now, &now
	f.transfers[item.TransferID] = &item
	f.prints[item.TransferID] = fmt.Sprintf("%s|%d|%s|%d", item.Direction, item.HajimiUserID, item.CanvasUserID, item.AmountMicro)
	return item
}

func (f *fakeHajimi) transferPosts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transferPostSeen
}

func (f *fakeHajimi) committedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, item := range f.transfers {
		if item.Status == remoteStatusCommitted {
			count++
		}
	}
	return count
}

func writeFakeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (f *fakeHajimi) authenticate(r *http.Request, body []byte) bool {
	if r.Header.Get(headerKeyID) != f.keyID {
		return false
	}
	ts, err := strconv.ParseInt(r.Header.Get(headerTimestamp), 10, 64)
	if err != nil {
		return false
	}
	if delta := f.now().Unix() - ts; delta > 120 || delta < -120 {
		return false
	}
	nonce := r.Header.Get(headerNonce)
	if !fakeNoncePattern.MatchString(nonce) {
		return false
	}
	bodyHash := sha256.Sum256(body)
	signing := "v1\n" + r.Method + "\n" + r.RequestURI + "\n" + r.Header.Get(headerTimestamp) + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, []byte(f.secret))
	mac.Write([]byte(signing))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(r.Header.Get(headerSignature))) {
		return false
	}
	if f.nonces[f.keyID+":"+nonce] {
		return false
	}
	f.nonces[f.keyID+":"+nonce] = true
	return true
}

func (f *fakeHajimi) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.RequestURI)
	if f.bridgeDisabled {
		writeFakeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if !f.authenticate(r, body) {
		f.authFailures++
		writeFakeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if f.failNext > 0 {
		f.failNext--
		writeFakeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(path, bridgeAPIPrefix+"/users/") && strings.HasSuffix(path, "/wallet"):
		id, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(path, bridgeAPIPrefix+"/users/"), "/wallet"), 10, 64)
		user := f.users[id]
		if user == nil {
			writeFakeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		limits := f.limits
		limits.UserDailyUsedMicro = f.dailyUsed(id)
		writeFakeJSON(w, http.StatusOK, RemoteWallet{HajimiUserID: id, Status: user.status, Grant: user.grant, CanvasUserID: user.canvasUserID, BalanceMicro: user.balance, NetInMicro: f.netIn(id), Limits: limits})
	case r.Method == http.MethodPost && path == bridgeAPIPrefix+"/transfers":
		f.transferPostSeen++
		if f.failTransferPosts {
			writeFakeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		f.createTransfer(w, body)
	case r.Method == http.MethodGet && path == bridgeAPIPrefix+"/transfers":
		after, _ := strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 200
		}
		items := []RemoteTransfer{}
		for seq := after + 1; seq <= f.seq && len(items) < limit; seq++ {
			for _, item := range f.transfers {
				if item.Seq == seq && item.Status == remoteStatusCommitted {
					items = append(items, *item)
				}
			}
		}
		next := after
		if len(items) > 0 {
			next = items[len(items)-1].Seq
		}
		writeFakeJSON(w, http.StatusOK, RemoteTransferPage{Items: items, NextAfterSeq: next})
	case r.Method == http.MethodGet && strings.HasPrefix(path, bridgeAPIPrefix+"/transfers/"):
		if f.lookupOverride != "" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(f.lookupOverride))
			return
		}
		item := f.transfers[strings.TrimPrefix(path, bridgeAPIPrefix+"/transfers/")]
		if item == nil {
			writeFakeJSON(w, http.StatusNotFound, map[string]string{"error": "transfer_not_found"})
			return
		}
		writeFakeJSON(w, http.StatusOK, item)
	case r.Method == http.MethodPost && path == bridgeAPIPrefix+"/users/status":
		var req struct {
			IDs []int64 `json:"hajimi_user_ids"`
		}
		_ = json.Unmarshal(body, &req)
		users := []RemoteUserStatus{}
		for _, id := range req.IDs {
			user := f.users[id]
			if user == nil {
				users = append(users, RemoteUserStatus{HajimiUserID: id, Status: "missing", Grant: "none", AuthEpoch: "0000000000000000"})
				continue
			}
			users = append(users, RemoteUserStatus{HajimiUserID: id, Status: user.status, Grant: user.grant, AuthEpoch: user.epoch})
		}
		writeFakeJSON(w, http.StatusOK, map[string]any{"users": users})
	case r.Method == http.MethodGet && path == bridgeAPIPrefix+"/reconcile/summary":
		summary := RemoteSummary{MaxSeq: f.seq}
		for _, item := range f.transfers {
			if item.Status != remoteStatusCommitted {
				continue
			}
			summary.CommittedCount++
			if item.Direction == remoteDirectionToCanvas {
				summary.ToCanvasCommittedMicro += item.AmountMicro
			} else {
				summary.ToHajimiCommittedMicro += item.AmountMicro
			}
		}
		writeFakeJSON(w, http.StatusOK, summary)
	case r.Method == http.MethodGet && path == bridgeAPIPrefix+"/cost/daily":
		f.costQueries = append(f.costQueries, r.URL.RawQuery)
		query := r.URL.Query()
		offset, _ := strconv.Atoi(query.Get("utc_offset_minutes"))
		out := RemoteCostReport{Days: []RemoteCostDay{}, Keys: []RemoteCostKey{}, Models: []RemoteCostModel{}, Accounts: []RemoteCostAccount{}}
		if f.cost != nil {
			out = *f.cost
			out.Configured = true
		}
		out.From, out.To, out.UTCOffsetMinutes = query.Get("from"), query.Get("to"), offset
		writeFakeJSON(w, http.StatusOK, out)
	default:
		writeFakeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	}
}

func (f *fakeHajimi) netIn(id int64) int64 {
	var total int64
	for _, item := range f.transfers {
		if item.HajimiUserID != id || item.Status != remoteStatusCommitted {
			continue
		}
		if item.Direction == remoteDirectionToCanvas {
			total += item.AmountMicro
		} else {
			total -= item.AmountMicro
		}
	}
	return total
}

func (f *fakeHajimi) dailyUsed(id int64) int64 {
	var total int64
	for _, item := range f.transfers {
		if item.HajimiUserID == id && item.Status == remoteStatusCommitted && item.Direction == remoteDirectionToCanvas {
			total += item.AmountMicro
		}
	}
	return total
}

var fakeTransferIDPattern = regexp.MustCompile(`^yct_[0-9a-f]{32}$`)

func (f *fakeHajimi) createTransfer(w http.ResponseWriter, body []byte) {
	var req RemoteTransferRequest
	if err := json.Unmarshal(body, &req); err != nil || !fakeTransferIDPattern.MatchString(req.TransferID) || req.AmountMicro <= 0 || req.HajimiUserID <= 0 || req.CanvasUserID == "" ||
		(req.Direction != remoteDirectionToCanvas && req.Direction != remoteDirectionToHajimi) {
		writeFakeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}
	print := fmt.Sprintf("%s|%d|%s|%d", req.Direction, req.HajimiUserID, req.CanvasUserID, req.AmountMicro)
	if existing := f.transfers[req.TransferID]; existing != nil {
		if f.prints[req.TransferID] != print {
			writeFakeJSON(w, http.StatusConflict, map[string]string{"error": "transfer_conflict"})
			return
		}
		writeFakeJSON(w, http.StatusOK, existing)
		return
	}
	now := f.now().UTC().Format(time.RFC3339)
	result := &RemoteTransfer{TransferID: req.TransferID, Direction: req.Direction, HajimiUserID: req.HajimiUserID, CanvasUserID: req.CanvasUserID, AmountMicro: req.AmountMicro, CreatedAt: now}
	reject := func(reason string) {
		result.Status, result.Reason = remoteStatusRejected, reason
	}
	user := f.users[req.HajimiUserID]
	switch {
	case user == nil || user.status != "active":
		reject("user_inactive")
	case user.grant != "active":
		reject("grant_inactive")
	case user.canvasUserID != "" && user.canvasUserID != req.CanvasUserID:
		reject("canvas_user_mismatch")
	case req.Direction == remoteDirectionToCanvas && (req.AmountMicro < f.limits.MinMicro || req.AmountMicro > f.limits.MaxMicro):
		reject("amount_out_of_range")
	case req.Direction == remoteDirectionToCanvas && f.dailyUsed(req.HajimiUserID)+req.AmountMicro > f.limits.UserDailyMicro:
		reject("user_daily_limit")
	case req.Direction == remoteDirectionToCanvas && user.balance < req.AmountMicro:
		reject("insufficient_balance")
	case req.Direction == remoteDirectionToHajimi && (req.AmountMicro < 10_000 || req.AmountMicro > f.limits.MaxMicro):
		reject("amount_out_of_range")
	case req.Direction == remoteDirectionToHajimi && req.AmountMicro > f.netIn(req.HajimiUserID):
		reject("net_in_exceeded")
	default:
		if user.canvasUserID == "" {
			user.canvasUserID = req.CanvasUserID
		}
		if req.Direction == remoteDirectionToCanvas {
			user.balance -= req.AmountMicro
		} else {
			user.balance += req.AmountMicro
		}
		f.seq++
		after := user.balance
		result.Status, result.Seq, result.BalanceAfterMicro, result.CommittedAt = remoteStatusCommitted, f.seq, &after, &now
	}
	f.transfers[req.TransferID] = result
	f.prints[req.TransferID] = print
	if f.commitThenFail > 0 {
		f.commitThenFail--
		writeFakeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream"})
		return
	}
	writeFakeJSON(w, http.StatusOK, result)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type testHost struct{ creditsOff bool }

func (*testHost) RequireAdmin(user *model.User) error {
	if user == nil || user.Role != model.UserRoleAdmin {
		return errDisabled()
	}
	return nil
}

func (h *testHost) RequireCreditsFeature() error {
	if h.creditsOff {
		return errDisabled()
	}
	return nil
}

// BillingOrderCost 在测试里按"已配置成本时单价 × 1"估算，足以区分有无成本。
func (*testHost) BillingOrderCost(order model.BillingOrder) (*int64, error) {
	if !order.CostPricing.Configured || order.Status != model.BillingStatusSettled {
		return nil, nil
	}
	cost := order.CostPricing.UnitPriceMicrocredits
	return &cost, nil
}

type testEnv struct {
	host  *testHost
	db    *gorm.DB
	repo  *repository.Repository
	svc   *Service
	fake  *fakeHajimi
	clock *fakeClock
	user  *model.User
	admin *model.User
}

const testHajimiUserID int64 = 42

// openTestDB 默认使用临时 SQLite 文件；设置 HAJIMI_BRIDGE_TEST_POSTGRES=1 且提供 CANVAS_TEST_POSTGRES_DSN 时
// 每个用例在独立 schema 中运行同一套断言。
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN"))
	if os.Getenv("HAJIMI_BRIDGE_TEST_POSTGRES") == "1" {
		if dsn == "" {
			t.Skip("CANVAS_TEST_POSTGRES_DSN is not configured")
		}
		return openPostgresTestDB(t, dsn)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bridge.db")+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func openPostgresTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	base, err := database.Open(database.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("hajimi_bridge_%d", time.Now().UnixNano())
	if err := base.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	db, err := database.Open(database.Config{Driver: "postgres", DSN: dsn + separator + "search_path=" + schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = base.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error
		if sqlDB, err := base.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db := openTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.UserIdentity{}, &model.AuthSession{}, &model.CreditAccount{}, &model.CreditLedgerEntry{}, &model.SystemSetting{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureForkHajimiBridgeTables(db); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	fake := newFakeHajimi(t, clock.Now)
	config := Config{Enabled: true, BaseURL: fake.server.URL, KeyID: fake.keyID, Secret: fake.secret, ExpectedIdPHost: "hajimi.chat", PublicURL: "https://hajimi.chat"}
	repo := repository.New(db)
	host := &testHost{}
	svc := newService(repo, host, config, clock.Now)
	svc.syncTimeout, svc.workerTimeout, svc.walletTimeout = 3*time.Second, 3*time.Second, 3*time.Second
	env := &testEnv{host: host, db: db, repo: repo, svc: svc, fake: fake, clock: clock}
	env.setIdPAuthorizationURL(t, "https://hajimi.chat/yingce/authorize")
	env.user = env.createUser(t, "canvas-user-1", "alice", model.UserRoleUser, "hajimi:42")
	env.admin = env.createUser(t, "canvas-admin-1", "admin", model.UserRoleAdmin, "")
	fake.addUser(testHajimiUserID, fakeUser{status: "active", grant: "active", balance: 100_000_000, epoch: "aaaaaaaaaaaaaaaa"})
	return env
}

func (e *testEnv) setIdPAuthorizationURL(t *testing.T, authorizationURL string) {
	t.Helper()
	e.setOAuthSlot(t, authorizationURL, "sub")
}

func (e *testEnv) setOAuthSlot(t *testing.T, authorizationURL string, subjectField string) {
	t.Helper()
	encoded, _ := json.Marshal(map[string]any{"enabled": true, "authorizationUrl": authorizationURL, "clientId": "yingce", "subjectField": subjectField})
	if err := e.repo.SaveSystemSetting(&model.SystemSetting{Key: linuxDOSettingKey, ValueJSON: string(encoded)}); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) createUser(t *testing.T, id string, username string, role model.UserRole, subject string) *model.User {
	t.Helper()
	user := &model.User{ID: id, Username: username, DisplayName: username, Role: role, Status: model.UserStatusActive}
	if err := e.db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if subject != "" {
		if err := e.db.Create(&model.UserIdentity{ID: "identity-" + id, UserID: id, Provider: "linuxdo", Subject: subject}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := e.db.Create(&model.AuthSession{ID: "session-" + id, UserID: id, TokenHash: "hash", ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	return user
}

func (e *testEnv) available(t *testing.T, userID string) int64 {
	t.Helper()
	account, err := e.repo.CreditAccount(userID)
	if err != nil {
		t.Fatal(err)
	}
	return account.AvailableMicrocredits
}

func (e *testEnv) setAvailable(t *testing.T, userID string, amount int64) {
	t.Helper()
	if _, err := e.repo.CreditAccount(userID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Model(&model.CreditAccount{}).Where("user_id = ?", userID).Update("available_microcredits", amount).Error; err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) transfer(t *testing.T, id string) *model.HajimiBridgeTransfer {
	t.Helper()
	row, err := e.repo.HajimiBridgeTransfer(id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func (e *testEnv) ledgerCount(t *testing.T, referenceKey string) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.CreditLedgerEntry{}).Where("reference_key = ?", referenceKey).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func (e *testEnv) sessionCount(t *testing.T, userID string) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.AuthSession{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func idemKey(suffix string) string {
	return "idem-key-0000000000-" + suffix
}
