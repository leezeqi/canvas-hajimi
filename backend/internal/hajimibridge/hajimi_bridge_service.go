package hajimibridge

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/gorm"
)

const (
	linuxDOSettingKey = "linuxdo_oauth"
	hajimiSubjectPref = "hajimi:"

	defaultAutoTopupThreshold = 1_000_000
	defaultAutoTopupAmount    = 5_000_000
	defaultAutoTopupDaily     = 20_000_000
	// hajimi to_hajimi 的最小金额（§3.4），本地先拦截避免无意义的扣款与退款。
	minWithdrawMicro = 10_000
	// 防御性上限：远大于任何合理的单笔划转，用于拦截溢出或误输入。
	maxSaneMicro = 1_000_000_000_000
)

// 前端按 reason 判断错误类型，不解析 msg。
const (
	ReasonBridgeDisabled    kernel.ErrorReason = "hajimi_bridge_disabled"
	ReasonNotLinked         kernel.ErrorReason = "hajimi_not_linked"
	ReasonUnavailable       kernel.ErrorReason = "hajimi_unavailable"
	ReasonNetInExceeded     kernel.ErrorReason = "hajimi_withdraw_exceeds_net_in"
	ReasonTransferFinished  kernel.ErrorReason = "hajimi_transfer_finished"
	ReasonNeedsManualReview kernel.ErrorReason = "hajimi_needs_manual_review"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

type Host interface {
	RequireAdmin(*model.User) error
	RequireCreditsFeature() error
	// BillingOrderCost 返回订单按影策后台配置的成本价估算的成本；未配置成本时为 nil。
	BillingOrderCost(order model.BillingOrder) (*int64, error)
}

type Service struct {
	repo   *repository.Repository
	host   Host
	config Config
	client *Client
	now    func() time.Time

	syncTimeout       time.Duration
	workerTimeout     time.Duration
	walletTimeout     time.Duration
	resolveAfter      time.Duration
	signatureGrace    time.Duration
	autoTopupCooldown time.Duration
	reconcileInterval time.Duration
	fullReconcileGap  time.Duration
	transferPageLimit int
	fullScanMaxPages  int
}

func New(repo *repository.Repository, host Host, config Config) *Service {
	return newService(repo, host, config, time.Now)
}

func newService(repo *repository.Repository, host Host, config Config, now func() time.Time) *Service {
	return &Service{
		repo: repo, host: host, config: config, client: NewClient(config, now), now: now,
		syncTimeout: 8 * time.Second, workerTimeout: 15 * time.Second, walletTimeout: 5 * time.Second,
		resolveAfter: 10 * time.Minute, signatureGrace: 3 * time.Minute, autoTopupCooldown: 30 * time.Minute,
		reconcileInterval: time.Hour, fullReconcileGap: 24 * time.Hour,
		transferPageLimit: defaultTransferPageLimit, fullScanMaxPages: defaultFullScanMaxPages,
	}
}

func (s *Service) Config() Config { return s.config }

type Transfer struct {
	ID          string     `json:"id"`
	Direction   string     `json:"direction"`
	AmountMicro int64      `json:"amountMicro"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason"`
	Source      string     `json:"source"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

type TransferPage struct {
	Items []Transfer `json:"items"`
	Total int64      `json:"total"`
}

type WalletLimits struct {
	MinMicro           int64 `json:"minMicro"`
	MaxMicro           int64 `json:"maxMicro"`
	UserDailyMicro     int64 `json:"userDailyMicro"`
	UserDailyUsedMicro int64 `json:"userDailyUsedMicro"`
}

type AutoTopup struct {
	Enabled         bool  `json:"enabled"`
	ThresholdMicro  int64 `json:"thresholdMicro"`
	AmountMicro     int64 `json:"amountMicro"`
	DailyLimitMicro int64 `json:"dailyLimitMicro"`
}

type Wallet struct {
	Enabled            bool          `json:"enabled"`
	Linked             bool          `json:"linked"`
	Reachable          bool          `json:"reachable"`
	HajimiPublicURL    string        `json:"hajimiPublicUrl"`
	HajimiBalanceMicro *int64        `json:"hajimiBalanceMicro"`
	NetInMicro         *int64        `json:"netInMicro"`
	Grant              *string       `json:"grant"`
	Limits             *WalletLimits `json:"limits"`
	LocalNetInMicro    int64         `json:"localNetInMicro"`
	AutoTopup          AutoTopup     `json:"autoTopup"`
	PendingCount       int64         `json:"pendingCount"`
}

type CreateTransferRequest struct {
	Direction      string `json:"direction"`
	AmountMicro    int64  `json:"amountMicro"`
	IdempotencyKey string `json:"idempotencyKey"`
	Source         string `json:"source"`
}

type AutoTopupRequest struct {
	Enabled         bool  `json:"enabled"`
	ThresholdMicro  int64 `json:"thresholdMicro"`
	AmountMicro     int64 `json:"amountMicro"`
	DailyLimitMicro int64 `json:"dailyLimitMicro"`
}

type AdminConfig struct {
	Enabled         bool   `json:"enabled"`
	URLHost         string `json:"urlHost"`
	KeyID           string `json:"keyId"`
	ExpectedIdPHost string `json:"expectedIdpHost"`
	IdPHostMatches  bool   `json:"idpHostMatches"`
	// SubjectFieldOK 表示 OAuth 槽位的 subjectField 为 "sub"；为 false 时所有账号都不视为关联。
	SubjectFieldOK bool `json:"subjectFieldOk"`
}

type RemoteSummaryView struct {
	ToCanvasCommittedMicro int64 `json:"toCanvasCommittedMicro"`
	ToHajimiCommittedMicro int64 `json:"toHajimiCommittedMicro"`
	CommittedCount         int64 `json:"committedCount"`
	MaxSeq                 int64 `json:"maxSeq"`
}

type ReconcileResult struct {
	At                  time.Time `json:"at"`
	OK                  bool      `json:"ok"`
	Full                bool      `json:"full"`
	LocalInMicro        int64     `json:"localInMicro"`
	LocalOutMicro       int64     `json:"localOutMicro"`
	RemoteToCanvasMicro int64     `json:"remoteToCanvasMicro"`
	RemoteToHajimiMicro int64     `json:"remoteToHajimiMicro"`
	Mismatches          []string  `json:"mismatches"`
}

type AdminTransfer struct {
	Transfer
	UserID       string `json:"userId"`
	Username     string `json:"username"`
	HajimiUserID int64  `json:"hajimiUserId"`
	HajimiSeq    int64  `json:"hajimiSeq"`
	Attempts     int    `json:"attempts"`
	LastError    string `json:"lastError"`
}

type Overview struct {
	Config        AdminConfig        `json:"config"`
	Reachable     bool               `json:"reachable"`
	RemoteSummary *RemoteSummaryView `json:"remoteSummary"`
	LastReconcile *ReconcileResult   `json:"lastReconcile"`
	Attention     []AdminTransfer    `json:"attention"`
	PendingCount  int64              `json:"pendingCount"`
	LinkedUsers   int64              `json:"linkedUsers"`
}

func errDisabled() error {
	return &kernel.AppError{Status: http.StatusForbidden, Code: http.StatusForbidden, Reason: ReasonBridgeDisabled, Message: "hajimi 余额互通未启用"}
}

func errNotLinked() error {
	return &kernel.AppError{Status: http.StatusForbidden, Code: http.StatusForbidden, Reason: ReasonNotLinked, Message: "使用 hajimi 账号登录的影策账号才能使用 hajimi 余额"}
}

func errUnavailable() error {
	return &kernel.AppError{Status: http.StatusServiceUnavailable, Code: http.StatusServiceUnavailable, Reason: ReasonUnavailable, Message: "hajimi 暂时无法连接，稍后再试", Retryable: true}
}

func errIdempotencyConflict() error {
	return &kernel.AppError{Status: http.StatusConflict, Code: kernel.CodeIdempotencyConflict, Reason: kernel.ReasonConflict, Message: "该幂等键已用于另一笔划转"}
}

func errManualReview(message string) error {
	return &kernel.AppError{Status: http.StatusConflict, Code: http.StatusConflict, Reason: ReasonNeedsManualReview, Message: message}
}

func toTransfer(row *model.HajimiBridgeTransfer) Transfer {
	return Transfer{ID: row.ID, Direction: row.Direction, AmountMicro: row.AmountMicrocredits, Status: row.Status, Reason: row.Reason, Source: row.Source, CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt}
}

func toAdminTransfer(row *model.HajimiBridgeTransfer, username string) AdminTransfer {
	return AdminTransfer{Transfer: toTransfer(row), UserID: row.UserID, Username: username, HajimiUserID: row.HajimiUserID, HajimiSeq: row.HajimiSeq, Attempts: row.Attempts, LastError: row.LastError}
}

// ParseHajimiSubject 严格解析 "hajimi:<正 int64>"，拒绝前导零、符号和空白。
func ParseHajimiSubject(subject string) (int64, bool) {
	rest, ok := strings.CutPrefix(subject, hajimiSubjectPref)
	if !ok || rest == "" || len(rest) > 19 || rest[0] == '0' {
		return 0, false
	}
	for _, char := range rest {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// slotCheck 描述通用 OAuth 槽位是否仍按 hajimi 方式配置。
type slotCheck struct {
	hostMatches  bool
	subjectIsSub bool
}

// readSlot 读取通用 OAuth 槽位：授权地址主机必须是期望的 hajimi 主机，且 subject 必须取自 hajimi 的 sub 字段，
// 防止槽位被改指或从用户可控字段映射出 "hajimi:<id>" 后误认身份。
func (s *Service) readSlot() (slotCheck, error) {
	if s.config.ExpectedIdPHost == "" {
		return slotCheck{}, nil
	}
	setting, err := s.repo.SystemSettingOptional(linuxDOSettingKey)
	if err != nil || setting == nil {
		return slotCheck{}, err
	}
	var value struct {
		AuthorizationURL string `json:"authorizationUrl"`
		SubjectField     string `json:"subjectField"`
	}
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return slotCheck{}, nil
	}
	return slotCheck{hostMatches: idpHostMatches(s.config.ExpectedIdPHost, value.AuthorizationURL), subjectIsSub: value.SubjectField == "sub"}, nil
}

// IdPHostMatches 只检查授权地址主机（管理端展示用）。
func (s *Service) IdPHostMatches() (bool, error) {
	check, err := s.readSlot()
	return check.hostMatches, err
}

// slotTrusted 要求主机匹配且 subjectField == "sub"；关联判断和所有 worker 都以它为前提。
func (s *Service) slotTrusted() (bool, error) {
	check, err := s.readSlot()
	return check.hostMatches && check.subjectIsSub, err
}

// linkedHajimiUser 判断影策用户是否为 hajimi 关联账号：互通已启用、槽位可信、且恰好有一个合法 hajimi 身份。
func (s *Service) linkedHajimiUser(userID string) (int64, bool, error) {
	if !s.config.Active() {
		return 0, false, nil
	}
	trusted, err := s.slotTrusted()
	if err != nil || !trusted {
		return 0, false, err
	}
	subjects, err := s.repo.HajimiLinkedSubjects(userID)
	if err != nil {
		return 0, false, err
	}
	return singleHajimiID(subjects)
}

func singleHajimiID(subjects []string) (int64, bool, error) {
	var found int64
	for _, subject := range subjects {
		id, ok := ParseHajimiSubject(subject)
		if !ok {
			continue
		}
		if found != 0 && found != id {
			return 0, false, nil
		}
		found = id
	}
	return found, found > 0, nil
}

func (s *Service) autoTopupFor(userID string) (AutoTopup, error) {
	settings, err := s.repo.HajimiBridgeUserSettings(userID)
	if err != nil {
		return AutoTopup{}, err
	}
	if settings == nil {
		return AutoTopup{ThresholdMicro: defaultAutoTopupThreshold, AmountMicro: defaultAutoTopupAmount, DailyLimitMicro: defaultAutoTopupDaily}, nil
	}
	return AutoTopup{Enabled: settings.AutoTopupEnabled, ThresholdMicro: settings.ThresholdMicrocredits, AmountMicro: settings.AmountMicrocredits, DailyLimitMicro: settings.DailyLimitMicrocredits}, nil
}

// Wallet 汇总互通钱包；hajimi 不可达时返回 reachable=false 和空值，不让整个接口失败。
func (s *Service) Wallet(ctx context.Context, user *model.User) (*Wallet, error) {
	if user == nil {
		return nil, kernel.Unauthorized("请先登录")
	}
	result := &Wallet{HajimiPublicURL: s.config.PublicURL}
	autoTopup, err := s.autoTopupDefaultsIfAbsent(user.ID)
	if err != nil {
		return nil, err
	}
	result.AutoTopup = autoTopup
	if s.host.RequireCreditsFeature() != nil || !s.config.Active() {
		return result, nil
	}
	result.Enabled = true
	if result.LocalNetInMicro, err = s.repo.HajimiBridgeLocalNetIn(user.ID); err != nil {
		return nil, err
	}
	if result.PendingCount, err = s.repo.HajimiBridgePendingCount(user.ID); err != nil {
		return nil, err
	}
	hajimiUserID, linked, err := s.linkedHajimiUser(user.ID)
	if err != nil {
		return nil, err
	}
	result.Linked = linked
	if !linked {
		return result, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, s.walletTimeout)
	defer cancel()
	remote, err := s.client.Wallet(callCtx, hajimiUserID)
	if err != nil {
		slog.Warn("hajimi wallet unavailable", "user_id", user.ID, "error", safeBridgeError(err))
		return result, nil
	}
	result.Reachable = true
	balance, netIn, grant := remote.BalanceMicro, remote.NetInMicro, remote.Grant
	result.HajimiBalanceMicro, result.NetInMicro, result.Grant = &balance, &netIn, &grant
	result.Limits = &WalletLimits{MinMicro: remote.Limits.MinMicro, MaxMicro: remote.Limits.MaxMicro, UserDailyMicro: remote.Limits.UserDailyMicro, UserDailyUsedMicro: remote.Limits.UserDailyUsedMicro}
	return result, nil
}

func (s *Service) autoTopupDefaultsIfAbsent(userID string) (AutoTopup, error) {
	if !s.config.Active() {
		return AutoTopup{ThresholdMicro: defaultAutoTopupThreshold, AmountMicro: defaultAutoTopupAmount, DailyLimitMicro: defaultAutoTopupDaily}, nil
	}
	return s.autoTopupFor(userID)
}

func (s *Service) Transfers(user *model.User, page int, pageSize int) (*TransferPage, error) {
	if user == nil {
		return nil, kernel.Unauthorized("请先登录")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	// 互通关闭后仍展示历史记录；只有 fork 表尚未迁移时返回空列表。
	if ready, err := s.repo.ForkHajimiSchemaReady(); err != nil || !ready {
		return &TransferPage{Items: []Transfer{}}, err
	}
	rows, total, err := s.repo.HajimiBridgeTransfersForUser(user.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]Transfer, 0, len(rows))
	for index := range rows {
		items = append(items, toTransfer(&rows[index]))
	}
	return &TransferPage{Items: items, Total: total}, nil
}

// UpdateAutoTopup 校验并保存自动转入设置；开启时金额必须落在 hajimi 当前单笔限额内。
func (s *Service) UpdateAutoTopup(ctx context.Context, user *model.User, req AutoTopupRequest) (*AutoTopup, error) {
	if user == nil {
		return nil, kernel.Unauthorized("请先登录")
	}
	if err := s.host.RequireCreditsFeature(); err != nil {
		return nil, err
	}
	if !s.config.Active() {
		return nil, errDisabled()
	}
	hajimiUserID, linked, err := s.linkedHajimiUser(user.ID)
	if err != nil {
		return nil, err
	}
	if !linked {
		return nil, errNotLinked()
	}
	if req.ThresholdMicro < 0 || req.ThresholdMicro > maxSaneMicro {
		return nil, kernel.BadAuthRequest("自动转入阈值无效")
	}
	if req.AmountMicro <= 0 || req.AmountMicro > maxSaneMicro {
		return nil, kernel.BadAuthRequest("每次自动转入金额必须大于 0")
	}
	if req.DailyLimitMicro < req.AmountMicro || req.DailyLimitMicro > maxSaneMicro {
		return nil, kernel.BadAuthRequest("每日自动转入上限不能小于单次金额")
	}
	if req.Enabled {
		callCtx, cancel := context.WithTimeout(ctx, s.walletTimeout)
		remote, err := s.client.Wallet(callCtx, hajimiUserID)
		cancel()
		if err != nil {
			return nil, errUnavailable()
		}
		if req.AmountMicro < remote.Limits.MinMicro || (remote.Limits.MaxMicro > 0 && req.AmountMicro > remote.Limits.MaxMicro) {
			return nil, kernel.BadAuthRequest("每次自动转入金额超出 hajimi 单笔限额")
		}
	}
	settings := model.HajimiBridgeUserSettings{UserID: user.ID, AutoTopupEnabled: req.Enabled, ThresholdMicrocredits: req.ThresholdMicro, AmountMicrocredits: req.AmountMicro, DailyLimitMicrocredits: req.DailyLimitMicro, UpdatedAt: s.now()}
	if err := s.repo.SaveHajimiBridgeUserSettings(&settings); err != nil {
		return nil, err
	}
	return &AutoTopup{Enabled: req.Enabled, ThresholdMicro: req.ThresholdMicro, AmountMicro: req.AmountMicro, DailyLimitMicro: req.DailyLimitMicro}, nil
}

func (s *Service) AdminOverview(ctx context.Context, actor *model.User) (*Overview, error) {
	if err := s.host.RequireAdmin(actor); err != nil {
		return nil, err
	}
	slot, err := s.readSlot()
	if err != nil {
		return nil, err
	}
	overview := &Overview{
		Config:    AdminConfig{Enabled: s.config.Active(), URLHost: s.config.URLHost(), KeyID: s.config.KeyID, ExpectedIdPHost: s.config.ExpectedIdPHost, IdPHostMatches: slot.hostMatches, SubjectFieldOK: slot.subjectIsSub},
		Attention: []AdminTransfer{},
	}
	ready, err := s.repo.ForkHajimiSchemaReady()
	if err != nil {
		return nil, err
	}
	if !ready {
		return overview, nil
	}
	if s.config.Active() {
		callCtx, cancel := context.WithTimeout(ctx, s.walletTimeout)
		summary, err := s.client.ReconcileSummary(callCtx)
		cancel()
		if err == nil {
			overview.Reachable = true
			overview.RemoteSummary = &RemoteSummaryView{ToCanvasCommittedMicro: summary.ToCanvasCommittedMicro, ToHajimiCommittedMicro: summary.ToHajimiCommittedMicro, CommittedCount: summary.CommittedCount, MaxSeq: summary.MaxSeq}
		}
	}
	if overview.LastReconcile, err = s.loadReconcileResult(); err != nil {
		return nil, err
	}
	rows, err := s.repo.HajimiBridgeAttentionTransfers(200)
	if err != nil {
		return nil, err
	}
	for index := range rows {
		overview.Attention = append(overview.Attention, toAdminTransfer(&rows[index].HajimiBridgeTransfer, rows[index].Username))
	}
	if overview.PendingCount, err = s.repo.HajimiBridgePendingCount(""); err != nil {
		return nil, err
	}
	if overview.LinkedUsers, err = s.repo.HajimiLinkedUserCount(); err != nil {
		return nil, err
	}
	return overview, nil
}

func (s *Service) loadReconcileResult() (*ReconcileResult, error) {
	raw, found, err := s.repo.HajimiBridgeStateValue(stateKeyReconcile)
	if err != nil || !found {
		return nil, err
	}
	var result ReconcileResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, nil
	}
	if result.Mismatches == nil {
		result.Mismatches = []string{}
	}
	return &result, nil
}

func notFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
