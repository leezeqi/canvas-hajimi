package hajimibridge

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	bridgeAPIPrefix         = "/internal/yingce/v1"
	maxBridgeResponseBytes  = 1 << 20
	breakerFailureThreshold = 5
	breakerOpenDuration     = 30 * time.Second
	headerKeyID             = "X-Yingce-Key-Id"
	headerTimestamp         = "X-Yingce-Timestamp"
	headerNonce             = "X-Yingce-Nonce"
	headerSignature         = "X-Yingce-Signature"
	remoteErrorConflict     = "transfer_conflict"
	// 只有 transfer_not_found 表示该划转在 hajimi 确实不存在（§3.5）；
	// 桥接关闭或路由不存在时返回的是 not_found，必须当作“暂时无法确定”。
	remoteErrorTransferNotFound = "transfer_not_found"
	remoteStatusCommitted       = "committed"
	remoteStatusRejected        = "rejected"
	remoteDirectionToCanvas     = "to_canvas"
	remoteDirectionToHajimi     = "to_hajimi"
	defaultTransferPageLimit    = 200
)

// ErrUnavailable 表示熔断打开，本次没有向 hajimi 发出请求。
var ErrUnavailable = errors.New("hajimi 互通暂时不可用")

// RemoteError 是 hajimi 返回的非 2xx 响应；只保留状态码和机器可读错误码。
type RemoteError struct {
	Status int
	Code   string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("hajimi 返回 HTTP %d（%s）", e.Status, e.Code)
}

// isRemoteTransferAbsent 仅在 hajimi 明确答复 404 {"error":"transfer_not_found"} 时成立；
// 其他 404（包括 not_found 和非 JSON 正文）都不能作为“未发生”的定论。
func isRemoteTransferAbsent(err error) bool {
	var remote *RemoteError
	return errors.As(err, &remote) && remote.Status == http.StatusNotFound && remote.Code == remoteErrorTransferNotFound
}

func isRemoteCode(err error, status int, code string) bool {
	var remote *RemoteError
	return errors.As(err, &remote) && remote.Status == status && remote.Code == code
}

// safeBridgeError 只输出错误类别，避免把内部地址或响应正文写进日志和数据库。
func safeBridgeError(err error) string {
	if err == nil {
		return ""
	}
	var remote *RemoteError
	switch {
	case errors.As(err, &remote):
		return remote.Error()
	case errors.Is(err, ErrUnavailable):
		return "熔断中，暂停调用 hajimi"
	case errors.Is(err, context.DeadlineExceeded):
		return "调用 hajimi 超时"
	case errors.Is(err, errBridgeResponseInvalid):
		return "hajimi 响应格式无效"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "调用 hajimi 超时"
	}
	return "连接 hajimi 失败"
}

var errBridgeResponseInvalid = errors.New("hajimi 响应格式无效")

type RemoteLimits struct {
	MinMicro                  int64 `json:"min_micro"`
	MaxMicro                  int64 `json:"max_micro"`
	UserDailyMicro            int64 `json:"user_daily_micro"`
	UserDailyUsedMicro        int64 `json:"user_daily_used_micro"`
	GlobalDailyRemainingMicro int64 `json:"global_daily_remaining_micro"`
}

type RemoteWallet struct {
	HajimiUserID int64        `json:"hajimi_user_id"`
	Status       string       `json:"status"`
	Grant        string       `json:"grant"`
	CanvasUserID string       `json:"canvas_user_id"`
	BalanceMicro int64        `json:"balance_micro"`
	NetInMicro   int64        `json:"net_in_micro"`
	Limits       RemoteLimits `json:"limits"`
}

type RemoteTransferRequest struct {
	TransferID   string `json:"transfer_id"`
	Direction    string `json:"direction"`
	HajimiUserID int64  `json:"hajimi_user_id"`
	CanvasUserID string `json:"canvas_user_id"`
	AmountMicro  int64  `json:"amount_micro"`
}

type RemoteTransfer struct {
	TransferID        string  `json:"transfer_id"`
	Direction         string  `json:"direction"`
	HajimiUserID      int64   `json:"hajimi_user_id"`
	CanvasUserID      string  `json:"canvas_user_id"`
	AmountMicro       int64   `json:"amount_micro"`
	Status            string  `json:"status"`
	Reason            string  `json:"reason"`
	Seq               int64   `json:"seq"`
	BalanceAfterMicro *int64  `json:"balance_after_micro"`
	CreatedAt         string  `json:"created_at"`
	CommittedAt       *string `json:"committed_at"`
}

type RemoteTransferPage struct {
	Items        []RemoteTransfer `json:"items"`
	NextAfterSeq int64            `json:"next_after_seq"`
}

type RemoteUserStatus struct {
	HajimiUserID int64  `json:"hajimi_user_id"`
	Status       string `json:"status"`
	Grant        string `json:"grant"`
	AuthEpoch    string `json:"auth_epoch"`
}

type RemoteSummary struct {
	ToCanvasCommittedMicro int64 `json:"to_canvas_committed_micro"`
	ToHajimiCommittedMicro int64 `json:"to_hajimi_committed_micro"`
	CommittedCount         int64 `json:"committed_count"`
	MaxSeq                 int64 `json:"max_seq"`
}

// Client 调用 hajimi 内部桥接接口（§3），每次发送都重新生成时间戳和 nonce。
type Client struct {
	baseURL string
	keyID   string
	secret  []byte
	http    *http.Client
	now     func() time.Time
	breaker *circuitBreaker
}

func NewClient(config Config, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	transport := &http.Transport{
		// 内部网络直连：环境代理会把请求行改成绝对 URI，破坏签名中的 request URI。
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
	}
	return &Client{
		baseURL: config.BaseURL,
		keyID:   config.KeyID,
		secret:  []byte(config.Secret),
		now:     now,
		breaker: &circuitBreaker{now: now},
		http: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// SigningString 按 §3.2 组装签名原文：各行以 \n 分隔，末尾没有换行。
func SigningString(method string, requestURI string, timestamp string, nonce string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	return strings.Join([]string{"v1", strings.ToUpper(method), requestURI, timestamp, nonce, hex.EncodeToString(bodyHash[:])}, "\n")
}

func Sign(secret []byte, method string, requestURI string, timestamp string, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(SigningString(method, requestURI, timestamp, nonce, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func newNonce() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (c *Client) Available() bool { return c.breaker.allow() }

func (c *Client) Wallet(ctx context.Context, hajimiUserID int64) (*RemoteWallet, error) {
	var out RemoteWallet
	if err := c.do(ctx, http.MethodGet, bridgeAPIPrefix+"/users/"+strconv.FormatInt(hajimiUserID, 10)+"/wallet", "", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateTransfer(ctx context.Context, req RemoteTransferRequest) (*RemoteTransfer, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var out RemoteTransfer
	if err := c.do(ctx, http.MethodPost, bridgeAPIPrefix+"/transfers", "", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetTransfer(ctx context.Context, transferID string) (*RemoteTransfer, error) {
	var out RemoteTransfer
	if err := c.do(ctx, http.MethodGet, bridgeAPIPrefix+"/transfers/"+transferID, "", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListTransfers(ctx context.Context, afterSeq int64, limit int) (*RemoteTransferPage, error) {
	if limit <= 0 || limit > 500 {
		limit = defaultTransferPageLimit
	}
	var out RemoteTransferPage
	query := "after_seq=" + strconv.FormatInt(afterSeq, 10) + "&limit=" + strconv.Itoa(limit)
	if err := c.do(ctx, http.MethodGet, bridgeAPIPrefix+"/transfers", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UsersStatus(ctx context.Context, hajimiUserIDs []int64) ([]RemoteUserStatus, error) {
	body, err := json.Marshal(map[string]any{"hajimi_user_ids": hajimiUserIDs})
	if err != nil {
		return nil, err
	}
	var out struct {
		Users []RemoteUserStatus `json:"users"`
	}
	if err := c.do(ctx, http.MethodPost, bridgeAPIPrefix+"/users/status", "", body, &out); err != nil {
		return nil, err
	}
	return out.Users, nil
}

func (c *Client) ReconcileSummary(ctx context.Context) (*RemoteSummary, error) {
	var out RemoteSummary
	if err := c.do(ctx, http.MethodGet, bridgeAPIPrefix+"/reconcile/summary", "", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoteCostAmounts 是 hajimi 成本报表一个分组的用量（§6.2，单位微美元）。
type RemoteCostAmounts struct {
	Requests          int64 `json:"requests"`
	ChargedMicro      int64 `json:"charged_micro"`
	UpstreamCostMicro int64 `json:"upstream_cost_micro"`
}

type RemoteCostDay struct {
	Date string `json:"date"`
	RemoteCostAmounts
}

type RemoteCostKey struct {
	APIKeyID int64  `json:"api_key_id"`
	Name     string `json:"name"`
	UserID   int64  `json:"user_id"`
	RemoteCostAmounts
}

type RemoteCostModel struct {
	Model string `json:"model"`
	RemoteCostAmounts
}

type RemoteCostAccount struct {
	UserID       int64 `json:"user_id"`
	BalanceMicro int64 `json:"balance_micro"`
}

type RemoteCostReport struct {
	Configured       bool   `json:"configured"`
	From             string `json:"from"`
	To               string `json:"to"`
	UTCOffsetMinutes int    `json:"utc_offset_minutes"`
	Scope            struct {
		UserIDs   []int64 `json:"user_ids"`
		APIKeyIDs []int64 `json:"api_key_ids"`
	} `json:"scope"`
	Days     []RemoteCostDay     `json:"days"`
	Keys     []RemoteCostKey     `json:"keys"`
	Models   []RemoteCostModel   `json:"models"`
	Accounts []RemoteCostAccount `json:"accounts"`
	Totals   RemoteCostAmounts   `json:"totals"`
}

// CostDaily 读取 hajimi 成本范围在 [from, to]（含两端，按固定 UTC 偏移）的逐日用量。
func (c *Client) CostDaily(ctx context.Context, from string, to string, utcOffsetMinutes int) (*RemoteCostReport, error) {
	query := url.Values{}
	query.Set("from", from)
	query.Set("to", to)
	query.Set("utc_offset_minutes", strconv.Itoa(utcOffsetMinutes))
	var out RemoteCostReport
	// 只读报表：受熔断约束，但成败都不计入熔断，避免管理员频繁切换查询条件影响用户划转。
	if err := c.send(ctx, http.MethodGet, bridgeAPIPrefix+"/cost/daily", query.Encode(), nil, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method string, path string, rawQuery string, body []byte, out any) error {
	return c.send(ctx, method, path, rawQuery, body, out, true)
}

// send 发出一次签名请求。trackBreaker=false 时只检查熔断、不更新熔断计数；
// 调用方主动取消（context.Canceled）的请求不说明 hajimi 有故障，也不计为失败。
func (c *Client) send(ctx context.Context, method string, path string, rawQuery string, body []byte, out any, trackBreaker bool) error {
	if !c.breaker.allow() {
		return ErrUnavailable
	}
	failure := func() {
		if trackBreaker && !errors.Is(ctx.Err(), context.Canceled) {
			c.breaker.failure()
		}
	}
	success := func() {
		if trackBreaker {
			c.breaker.success()
		}
	}
	target := c.baseURL + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	// 签名使用实际写在请求行上的 URI（路径 + 原始查询串）。
	requestURI := req.URL.RequestURI()
	req.Header.Set(headerKeyID, c.keyID)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerNonce, nonce)
	req.Header.Set(headerSignature, Sign(c.secret, method, requestURI, timestamp, nonce, body))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		failure()
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBridgeResponseBytes+1))
	if err != nil {
		failure()
		return err
	}
	if len(payload) > maxBridgeResponseBytes {
		failure()
		return errBridgeResponseInvalid
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.Unmarshal(payload, out); err != nil {
			failure()
			return errBridgeResponseInvalid
		}
		success()
		return nil
	}
	var remote struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(payload, &remote)
	remoteErr := &RemoteError{Status: resp.StatusCode, Code: sanitizeRemoteCode(remote.Error)}
	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
		// hajimi 已认证并处理了请求，说明链路可用。
		success()
	default:
		failure()
	}
	return remoteErr
}

// sanitizeRemoteCode 原样保留合法错误码（不裁剪空白），其余一律归为 invalid/unknown，
// 保证只有精确的 transfer_not_found 才会被识别。
func sanitizeRemoteCode(value string) string {
	if len(value) > 48 {
		return "invalid"
	}
	for _, char := range value {
		if !(char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
			return "invalid"
		}
	}
	if value == "" {
		return "unknown"
	}
	return value
}

// circuitBreaker：连续 5 次调用失败后暂停 30 秒；成功一次即复位。
type circuitBreaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
	now       func() time.Time
}

func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures < breakerFailureThreshold || !b.now().Before(b.openUntil)
}

func (b *circuitBreaker) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openUntil = time.Time{}
}

func (b *circuitBreaker) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= breakerFailureThreshold {
		b.openUntil = b.now().Add(breakerOpenDuration)
	}
}
