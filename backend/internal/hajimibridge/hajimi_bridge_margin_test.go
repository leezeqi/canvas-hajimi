package hajimibridge

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

type marginFixture struct {
	*testEnv
	orderN int
}

func newMarginFixture(t *testing.T) *marginFixture {
	t.Helper()
	env := newTestEnv(t)
	if err := env.db.AutoMigrate(&model.ModelChannel{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	return &marginFixture{testEnv: env}
}

func (f *marginFixture) channel(t *testing.T, id string, baseURL string, scope model.ChannelScope, deleted bool) {
	t.Helper()
	row := &model.ModelChannel{ID: id, Name: "渠道-" + id, BaseURL: baseURL, Scope: scope, Enabled: true}
	if deleted {
		row.DeletedAt = gorm.DeletedAt{Time: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	}
	if err := f.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
}

func (f *marginFixture) order(t *testing.T, channelID string, status model.BillingStatus, actual int64, reserved int64, modelName string, costUnit *int64, at time.Time) {
	t.Helper()
	f.orderN++
	row := &model.BillingOrder{
		ID: "order-" + strconv.Itoa(f.orderN), UserID: f.user.ID, IdempotencyKey: "idem-" + strconv.Itoa(f.orderN),
		ChannelID: channelID, Model: modelName, Status: status,
		ActualAmountMicrocredits: actual, ReservedAmountMicrocredits: reserved, CreatedAt: at, UpdatedAt: at,
	}
	if costUnit != nil {
		row.CostPricing = model.CreditCostPricing{Configured: true, UnitPriceMicrocredits: *costUnit}
		row.CostBillingMode = "fixed_request"
		row.CostQuantity = 10_000
	}
	if err := f.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
}

func int64Ptr(value int64) *int64 { return &value }

func intPtr(value int) *int { return &value }

func requireMicro(t *testing.T, label string, got *int64, want int64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", label, got, want)
	}
}

func seedMarginData(t *testing.T, f *marginFixture) {
	t.Helper()
	f.channel(t, "ch-hajimi", "https://api.hajimi.chat/v1", model.ChannelScopeSystem, false)
	f.channel(t, "ch-root", "https://hajimi.chat", model.ChannelScopeSystem, false)
	f.channel(t, "ch-other", "https://video.example.com/api", model.ChannelScopeSystem, false)
	f.channel(t, "ch-user", "https://api.hajimi.chat", model.ChannelScopeUser, false)
	f.channel(t, "ch-old", "https://api.hajimi.chat", model.ChannelScopeSystem, true)
	f.channel(t, "ch-old-unused", "https://api.hajimi.chat", model.ChannelScopeSystem, true)
	f.channel(t, "ch-lookalike", "https://hajimi.chat.evil.example", model.ChannelScopeSystem, false)

	// 23:59:59 and 00:00:00 at UTC+8 straddle the 11-01 / 11-02 boundary.
	f.order(t, "ch-hajimi", model.BillingStatusSettled, 2_000_000, 2_000_000, "img-a", nil, time.Date(2026, 11, 1, 15, 59, 59, 0, time.UTC))
	f.order(t, "ch-hajimi", model.BillingStatusSettled, 3_000_000, 3_000_000, "img-a", int64Ptr(1_000_000), time.Date(2026, 11, 1, 16, 0, 0, 0, time.UTC))
	f.order(t, "ch-other", model.BillingStatusSettled, 5_000_000, 6_000_000, "vid", int64Ptr(4_000_000), time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC))
	f.order(t, "ch-other", model.BillingStatusSettled, 1_000_000, 1_000_000, "vid", nil, time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC))
	f.order(t, "ch-user", model.BillingStatusSettled, 700_000, 700_000, "img-a", nil, time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC))
	f.order(t, "ch-old", model.BillingStatusSettled, 1_000_000, 1_000_000, "", nil, time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC))
	f.order(t, "ch-hajimi", model.BillingStatusUncertain, 0, 9_000_000, "img-a", nil, time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC))
	f.order(t, "ch-hajimi", model.BillingStatusRefunded, 0, 4_000_000, "img-a", nil, time.Date(2026, 11, 2, 6, 0, 0, 0, time.UTC))
	// Just before the window (10-31 23:59:59 at UTC+8) and just after it.
	f.order(t, "ch-hajimi", model.BillingStatusSettled, 8_000_000, 8_000_000, "img-a", nil, time.Date(2026, 10, 31, 15, 59, 59, 0, time.UTC))
	f.order(t, "ch-hajimi", model.BillingStatusSettled, 8_000_000, 8_000_000, "img-a", nil, time.Date(2026, 11, 3, 16, 0, 0, 0, time.UTC))
}

func TestMarginReportCombinesRevenueAndHajimiCost(t *testing.T) {
	f := newMarginFixture(t)
	seedMarginData(t, f)
	f.fake.set(func(fake *fakeHajimi) {
		fake.cost = &RemoteCostReport{
			Days: []RemoteCostDay{
				{Date: "2026-11-01", RemoteCostAmounts: RemoteCostAmounts{Requests: 1, ChargedMicro: 1_500_000, UpstreamCostMicro: 600_000}},
				{Date: "2026-11-02", RemoteCostAmounts: RemoteCostAmounts{Requests: 2, ChargedMicro: 2_000_000, UpstreamCostMicro: 900_000}},
				{Date: "2026-12-25", RemoteCostAmounts: RemoteCostAmounts{Requests: 9, ChargedMicro: 9_000_000}},
			},
			Keys:     []RemoteCostKey{{APIKeyID: 12, Name: "影策-生图", UserID: 5, RemoteCostAmounts: RemoteCostAmounts{Requests: 3, ChargedMicro: 3_500_000, UpstreamCostMicro: 1_500_000}}},
			Models:   []RemoteCostModel{{Model: "gpt-image-1", RemoteCostAmounts: RemoteCostAmounts{Requests: 3, ChargedMicro: 3_500_000, UpstreamCostMicro: 1_500_000}}},
			Accounts: []RemoteCostAccount{{UserID: 5, BalanceMicro: 42_000_000}},
		}
		fake.cost.Scope.UserIDs = []int64{5}
		fake.cost.Scope.APIKeyIDs = []int64{12}
	})

	report, err := f.svc.AdminMarginReport(context.Background(), f.admin, MarginRequest{From: "2026-11-01", To: "2026-11-03", UTCOffsetMinutes: intPtr(480)})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.fake.costQueries; len(got) != 1 || got[0] != "from=2026-11-01&to=2026-11-03&utc_offset_minutes=480" {
		t.Fatalf("cost queries = %v", got)
	}
	if report.Hajimi.Status != MarginHajimiOK || len(report.Days) != 3 {
		t.Fatalf("status=%s days=%d", report.Hajimi.Status, len(report.Days))
	}

	day := report.Days[0]
	if day.Date != "2026-11-01" || day.HajimiOrders != 1 || day.HajimiRevenueMicro != 2_000_000 || day.OtherOrders != 0 {
		t.Fatalf("day 11-01 = %+v", day)
	}
	requireMicro(t, "11-01 charged", day.HajimiChargedMicro, 1_500_000)
	requireMicro(t, "11-01 gross", day.GrossProfitMicro, 500_000)
	requireMicro(t, "11-01 combined", day.CombinedProfitMicro, 1_400_000)

	day = report.Days[1]
	if day.HajimiOrders != 1 || day.HajimiRevenueMicro != 3_000_000 || day.OtherOrders != 1 || day.OtherRevenueMicro != 5_000_000 || day.OtherEstimatedCostMicro != 4_000_000 || day.OtherUncostedOrders != 0 {
		t.Fatalf("day 11-02 = %+v", day)
	}
	requireMicro(t, "11-02 requests", day.HajimiRequests, 2)
	requireMicro(t, "11-02 gross", day.GrossProfitMicro, 1_000_000)

	day = report.Days[2]
	// ch-old is a deleted hajimi system channel: still hajimi revenue. ch-user is user scope: other.
	if day.HajimiOrders != 1 || day.HajimiRevenueMicro != 1_000_000 || day.OtherOrders != 2 || day.OtherRevenueMicro != 1_700_000 || day.OtherUncostedOrders != 2 {
		t.Fatalf("day 11-03 = %+v", day)
	}
	requireMicro(t, "11-03 charged (no hajimi usage)", day.HajimiChargedMicro, 0)
	requireMicro(t, "11-03 gross", day.GrossProfitMicro, 1_000_000)

	totals := report.Totals
	if totals.HajimiOrders != 3 || totals.HajimiRevenueMicro != 6_000_000 || totals.OtherRevenueMicro != 6_700_000 || totals.OtherEstimatedCostMicro != 4_000_000 {
		t.Fatalf("totals = %+v", totals)
	}
	requireMicro(t, "total charged ignores out-of-range hajimi days", totals.HajimiChargedMicro, 3_500_000)
	requireMicro(t, "total gross", totals.GrossProfitMicro, 2_500_000)
	requireMicro(t, "total upstream", totals.HajimiUpstreamCostMicro, 1_500_000)
	requireMicro(t, "total combined", totals.CombinedProfitMicro, 4_500_000)
	if report.OpenOrders != 1 || report.OpenReservedMicro != 9_000_000 {
		t.Fatalf("open orders = %d / %d", report.OpenOrders, report.OpenReservedMicro)
	}

	channels := map[string]MarginChannel{}
	for _, channel := range report.Channels {
		channels[channel.ID] = channel
	}
	for id, wantVia := range map[string]bool{"ch-hajimi": true, "ch-root": true, "ch-old": true, "ch-other": false, "ch-user": false} {
		channel, ok := channels[id]
		if !ok || channel.ViaHajimi != wantVia {
			t.Fatalf("channel %s = %+v (present=%v), want viaHajimi=%v", id, channel, ok, wantVia)
		}
	}
	if _, ok := channels["ch-old-unused"]; ok {
		t.Fatal("deleted channels without orders are not listed")
	}
	if _, ok := channels["ch-lookalike"]; ok {
		t.Fatal("lookalike host must not be a hajimi channel and has no orders")
	}
	if !channels["ch-old"].Deleted || channels["ch-hajimi"].Orders != 2 || channels["ch-hajimi"].RevenueMicro != 5_000_000 {
		t.Fatalf("channel stats = %+v / %+v", channels["ch-old"], channels["ch-hajimi"])
	}
	if !report.Channels[0].ViaHajimi {
		t.Fatal("hajimi channels are listed first")
	}

	models := map[[2]any]MarginModel{}
	for _, row := range report.Models {
		models[[2]any{row.Model, row.ViaHajimi}] = row
	}
	if row := models[[2]any{"img-a", true}]; row.Orders != 2 || row.RevenueMicro != 5_000_000 || row.EstimatedCostMicro != 1_000_000 || row.UncostedOrders != 1 {
		t.Fatalf("img-a via hajimi = %+v", row)
	}
	if row := models[[2]any{"img-a", false}]; row.Orders != 1 || row.RevenueMicro != 700_000 {
		t.Fatalf("img-a other = %+v", row)
	}
	if row := models[[2]any{marginUnknownModel, true}]; row.Orders != 1 {
		t.Fatalf("unknown model = %+v", row)
	}

	if len(report.Hajimi.Keys) != 1 || report.Hajimi.Keys[0].Name != "影策-生图" || report.Hajimi.Keys[0].ChargedMicro != 3_500_000 {
		t.Fatalf("keys = %+v", report.Hajimi.Keys)
	}
	if len(report.Hajimi.Accounts) != 1 || report.Hajimi.Accounts[0].BalanceMicro != 42_000_000 {
		t.Fatalf("accounts = %+v", report.Hajimi.Accounts)
	}
	if len(report.Hajimi.ScopeUserIDs) != 1 || len(report.Hajimi.ScopeAPIKeyIDs) != 1 {
		t.Fatalf("scope = %+v / %+v", report.Hajimi.ScopeUserIDs, report.Hajimi.ScopeAPIKeyIDs)
	}
	if len(report.ChannelRule) != 2 || report.ChannelRule[0] != "hajimi.chat" || report.ChannelRule[1] != "*.hajimi.chat" {
		t.Fatalf("channel rule = %v", report.ChannelRule)
	}
}

func TestMarginReportWithoutHajimiCost(t *testing.T) {
	f := newMarginFixture(t)
	seedMarginData(t, f)
	req := MarginRequest{From: "2026-11-01", To: "2026-11-03", UTCOffsetMinutes: intPtr(480)}
	check := func(label string, wantStatus string) {
		t.Helper()
		report, err := f.svc.AdminMarginReport(context.Background(), f.admin, req)
		if err != nil {
			t.Fatal(err)
		}
		if report.Hajimi.Status != wantStatus || report.Hajimi.Message == "" {
			t.Fatalf("%s: status=%s message=%q", label, report.Hajimi.Status, report.Hajimi.Message)
		}
		if report.Totals.HajimiRevenueMicro != 6_000_000 || report.Totals.HajimiChargedMicro != nil || report.Totals.GrossProfitMicro != nil {
			t.Fatalf("%s: canvas revenue must stay, hajimi columns must be null: %+v", label, report.Totals)
		}
		for _, day := range report.Days {
			if day.HajimiChargedMicro != nil || day.GrossProfitMicro != nil {
				t.Fatalf("%s: day %s has hajimi numbers", label, day.Date)
			}
		}
	}
	// hajimi answers configured=false.
	check("not configured", MarginHajimiNotConfigured)
	// hajimi unreachable / failing.
	f.fake.set(func(fake *fakeHajimi) { fake.failNext = 1 })
	check("unavailable", MarginHajimiUnavailable)
	// bridge disabled locally: no call at all.
	before := len(f.fake.costQueries)
	f.svc.config.Enabled = false
	check("disabled", MarginHajimiDisabled)
	if len(f.fake.costQueries) != before {
		t.Fatal("a disabled bridge must not call hajimi")
	}
}

func TestMarginReportValidationAndDefaults(t *testing.T) {
	f := newMarginFixture(t)
	if _, err := f.svc.AdminMarginReport(context.Background(), f.user, MarginRequest{}); err == nil {
		t.Fatal("non-admin must be refused")
	}
	for _, req := range []MarginRequest{
		{From: "2026-11-03", To: "2026-11-01"},
		{From: "2026-11-01"},
		{To: "2026-11-01"},
		{From: "2026-1-01", To: "2026-11-01"},
		{From: "2026-01-01", To: "2026-04-03"},
		{From: "2026-11-01", To: "2026-11-02", UTCOffsetMinutes: intPtr(900)},
	} {
		_, err := f.svc.AdminMarginReport(context.Background(), f.admin, req)
		var appErr *kernel.AppError
		if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest {
			t.Fatalf("%+v: err = %v", req, err)
		}
	}
	// Default: last 7 days including today at UTC+8 (clock is 2026-11-08 10:00 UTC = 18:00 +8).
	report, err := f.svc.AdminMarginReport(context.Background(), f.admin, MarginRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if report.From != "2026-11-02" || report.To != "2026-11-08" || report.UTCOffsetMinutes != 480 || len(report.Days) != 7 {
		t.Fatalf("default window = %s..%s offset=%d days=%d", report.From, report.To, report.UTCOffsetMinutes, len(report.Days))
	}
	// 92 days is the maximum.
	report, err = f.svc.AdminMarginReport(context.Background(), f.admin, MarginRequest{From: "2026-01-01", To: "2026-04-02", UTCOffsetMinutes: intPtr(0)})
	if err != nil || len(report.Days) != 92 {
		t.Fatalf("92-day window: err=%v", err)
	}
}

func TestCostChannelHostRule(t *testing.T) {
	config := ParseConfig(func(key string) string {
		return map[string]string{"HAJIMI_BRIDGE_EXPECTED_IDP_HOST": "hajimi.chat"}[key]
	})
	for host, want := range map[string]bool{
		"hajimi.chat": true, "api.hajimi.chat": true, "API.Hajimi.Chat.": true,
		"hajimi.chat.evil.example": false, "nothajimi.chat": false, "": false,
	} {
		if got := config.IsCostChannelHost(host); got != want {
			t.Fatalf("%q: got %v want %v", host, got, want)
		}
	}
	config = ParseConfig(func(key string) string {
		return map[string]string{
			"HAJIMI_BRIDGE_EXPECTED_IDP_HOST":  "hajimi.chat",
			"HAJIMI_BRIDGE_COST_CHANNEL_HOSTS": " 127.0.0.1 , Hajimi-Bridge., bad/host, a:1 ,",
		}[key]
	})
	if len(config.CostChannelHosts) != 2 || !config.IsCostChannelHost("127.0.0.1") || !config.IsCostChannelHost("hajimi-bridge") || config.IsCostChannelHost("api.hajimi.chat") {
		t.Fatalf("explicit hosts = %v", config.CostChannelHosts)
	}
	if rule := config.CostChannelRule(); len(rule) != 2 || rule[0] != "127.0.0.1" {
		t.Fatalf("rule = %v", rule)
	}
	// Explicit hosts never disable the bridge, even when some entries are invalid.
	for _, problem := range config.Problems {
		if problem == "HAJIMI_BRIDGE_COST_CHANNEL_HOSTS" {
			t.Fatal("cost channel hosts must not be a config problem")
		}
	}
}

func TestMarginReportSkipsHajimiWhenRequestCancelled(t *testing.T) {
	f := newMarginFixture(t)
	seedMarginData(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := f.svc.AdminMarginReport(ctx, f.admin, MarginRequest{From: "2026-11-01", To: "2026-11-03", UTCOffsetMinutes: intPtr(480)})
	if err != nil {
		t.Fatal(err)
	}
	if report.Hajimi.Status != MarginHajimiUnavailable || len(f.fake.costQueries) != 0 {
		t.Fatalf("status=%s queries=%v", report.Hajimi.Status, f.fake.costQueries)
	}
	if !f.svc.client.Available() {
		t.Fatal("a cancelled report must not affect the breaker")
	}
}
