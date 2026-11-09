package hajimibridge

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

// 毛利报表（合同 §6）：影策已结算订单的实收积分（1 积分 = 1 美元）对比 hajimi 成本范围的实际扣费。

const (
	marginMaxDays       = 92
	marginDefaultDays   = 7
	marginDefaultOffset = 480
	marginMinOffset     = -720
	marginMaxOffset     = 840
	marginDateLayout    = "2006-01-02"
	marginRemoteTimeout = 15 * time.Second
	marginUnknownModel  = "未识别"
)

// hajimi 成本数据的状态。
const (
	MarginHajimiOK            = "ok"
	MarginHajimiDisabled      = "disabled"
	MarginHajimiNotConfigured = "not_configured"
	MarginHajimiUnavailable   = "unavailable"
)

type MarginRequest struct {
	From             string
	To               string
	UTCOffsetMinutes *int
}

// MarginDay 是一天（或合计）的数字。Hajimi* 指针在拿不到 hajimi 成本时为 nil。
type MarginDay struct {
	Date                    string `json:"date"`
	HajimiOrders            int64  `json:"hajimiOrders"`
	HajimiRevenueMicro      int64  `json:"hajimiRevenueMicro"`
	HajimiRequests          *int64 `json:"hajimiRequests"`
	HajimiChargedMicro      *int64 `json:"hajimiChargedMicro"`
	HajimiUpstreamCostMicro *int64 `json:"hajimiUpstreamCostMicro"`
	// GrossProfitMicro = 走 hajimi 渠道的收入 − hajimi 扣费；CombinedProfitMicro = 同一收入 − hajimi 上游成本。
	GrossProfitMicro        *int64 `json:"grossProfitMicro"`
	CombinedProfitMicro     *int64 `json:"combinedProfitMicro"`
	OtherOrders             int64  `json:"otherOrders"`
	OtherRevenueMicro       int64  `json:"otherRevenueMicro"`
	OtherEstimatedCostMicro int64  `json:"otherEstimatedCostMicro"`
	OtherUncostedOrders     int64  `json:"otherUncostedOrders"`
}

type MarginModel struct {
	Model              string `json:"model"`
	ViaHajimi          bool   `json:"viaHajimi"`
	Orders             int64  `json:"orders"`
	RevenueMicro       int64  `json:"revenueMicro"`
	EstimatedCostMicro int64  `json:"estimatedCostMicro"`
	UncostedOrders     int64  `json:"uncostedOrders"`
}

type MarginChannel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	System       bool   `json:"system"`
	ViaHajimi    bool   `json:"viaHajimi"`
	Deleted      bool   `json:"deleted"`
	Orders       int64  `json:"orders"`
	RevenueMicro int64  `json:"revenueMicro"`
}

type MarginCostAmounts struct {
	Requests          int64 `json:"requests"`
	ChargedMicro      int64 `json:"chargedMicro"`
	UpstreamCostMicro int64 `json:"upstreamCostMicro"`
}

type MarginCostKey struct {
	APIKeyID int64  `json:"apiKeyId"`
	Name     string `json:"name"`
	UserID   int64  `json:"userId"`
	MarginCostAmounts
}

type MarginCostModel struct {
	Model string `json:"model"`
	MarginCostAmounts
}

type MarginCostAccount struct {
	UserID       int64 `json:"userId"`
	BalanceMicro int64 `json:"balanceMicro"`
}

type MarginHajimi struct {
	Status         string              `json:"status"`
	Message        string              `json:"message"`
	ScopeUserIDs   []int64             `json:"scopeUserIds"`
	ScopeAPIKeyIDs []int64             `json:"scopeApiKeyIds"`
	Keys           []MarginCostKey     `json:"keys"`
	Models         []MarginCostModel   `json:"models"`
	Accounts       []MarginCostAccount `json:"accounts"`
}

type MarginReport struct {
	From             string          `json:"from"`
	To               string          `json:"to"`
	UTCOffsetMinutes int             `json:"utcOffsetMinutes"`
	ChannelRule      []string        `json:"channelRule"`
	Days             []MarginDay     `json:"days"`
	Totals           MarginDay       `json:"totals"`
	Models           []MarginModel   `json:"models"`
	Channels         []MarginChannel `json:"channels"`
	Hajimi           MarginHajimi    `json:"hajimi"`
	// 区间内尚未结束（预留/执行中/待确认）的订单，不计入收入。
	OpenOrders        int64     `json:"openOrders"`
	OpenReservedMicro int64     `json:"openReservedMicro"`
	GeneratedAt       time.Time `json:"generatedAt"`
}

type marginWindow struct {
	from, to   string
	offset     int
	loc        *time.Location
	start, end time.Time
	dates      []string
}

func parseMarginDate(raw string, loc *time.Location) (time.Time, bool) {
	day, err := time.ParseInLocation(marginDateLayout, raw, loc)
	if err != nil || day.Format(marginDateLayout) != raw || day.Year() < 2000 || day.Year() > 9999 {
		return time.Time{}, false
	}
	return day, true
}

// resolveMarginWindow 校验查询参数；不传日期时默认最近 7 天（含今天），时区默认北京时间。
func resolveMarginWindow(req MarginRequest, now time.Time) (*marginWindow, error) {
	offset := marginDefaultOffset
	if req.UTCOffsetMinutes != nil {
		offset = *req.UTCOffsetMinutes
	}
	if offset < marginMinOffset || offset > marginMaxOffset {
		return nil, kernel.BadAuthRequest("时区偏移无效")
	}
	loc := time.FixedZone("report", offset*60)
	from, to := strings.TrimSpace(req.From), strings.TrimSpace(req.To)
	if from == "" && to == "" {
		today := now.In(loc)
		to = today.Format(marginDateLayout)
		from = today.AddDate(0, 0, -(marginDefaultDays - 1)).Format(marginDateLayout)
	}
	fromDay, okFrom := parseMarginDate(from, loc)
	toDay, okTo := parseMarginDate(to, loc)
	if !okFrom || !okTo {
		return nil, kernel.BadAuthRequest("日期格式应为 YYYY-MM-DD")
	}
	if toDay.Before(fromDay) {
		return nil, kernel.BadAuthRequest("结束日期不能早于开始日期")
	}
	end := toDay.AddDate(0, 0, 1)
	days := int(end.Sub(fromDay) / (24 * time.Hour))
	if days > marginMaxDays {
		return nil, kernel.BadAuthRequest("一次最多查询 92 天")
	}
	window := &marginWindow{from: from, to: to, offset: offset, loc: loc, start: fromDay.UTC(), end: end.UTC()}
	for day := fromDay; day.Before(end); day = day.AddDate(0, 0, 1) {
		window.dates = append(window.dates, day.Format(marginDateLayout))
	}
	return window, nil
}

type marginChannelInfo struct {
	MarginChannel
	seen bool
}

func (s *Service) classifyMarginChannels() (map[string]*marginChannelInfo, error) {
	channels, err := s.repo.HajimiMarginChannels()
	if err != nil {
		return nil, err
	}
	out := make(map[string]*marginChannelInfo, len(channels))
	for _, channel := range channels {
		host := ""
		if parsed, err := url.Parse(strings.TrimSpace(channel.BaseURL)); err == nil {
			host = strings.ToLower(parsed.Hostname())
		}
		system := channel.Scope == model.ChannelScopeSystem
		out[channel.ID] = &marginChannelInfo{MarginChannel: MarginChannel{
			ID: channel.ID, Name: channel.Name, Host: host, System: system,
			// 只有系统渠道使用运营 key；用户自带 key 的渠道即使指向 hajimi，成本也不在影策账上。
			ViaHajimi: system && s.config.IsCostChannelHost(host),
			Deleted:   channel.DeletedAt.Valid,
		}}
	}
	return out, nil
}

// AdminMarginReport 生成毛利报表；hajimi 不可用时仍返回影策这一侧的数字。
func (s *Service) AdminMarginReport(ctx context.Context, actor *model.User, req MarginRequest) (*MarginReport, error) {
	if err := s.host.RequireAdmin(actor); err != nil {
		return nil, err
	}
	window, err := resolveMarginWindow(req, s.now())
	if err != nil {
		return nil, err
	}
	channels, err := s.classifyMarginChannels()
	if err != nil {
		return nil, err
	}
	report := &MarginReport{
		From: window.from, To: window.to, UTCOffsetMinutes: window.offset,
		ChannelRule: s.config.CostChannelRule(),
		Days:        make([]MarginDay, 0, len(window.dates)),
		Models:      []MarginModel{},
		Channels:    []MarginChannel{},
		GeneratedAt: s.now().UTC(),
	}
	dayIndex := make(map[string]int, len(window.dates))
	for index, date := range window.dates {
		report.Days = append(report.Days, MarginDay{Date: date})
		dayIndex[date] = index
	}
	models := map[[2]string]*MarginModel{}
	err = s.repo.VisitHajimiMarginOrders(window.start, window.end, func(batch []model.BillingOrder) error {
		for index := range batch {
			if err := s.addMarginOrder(report, window, dayIndex, channels, models, &batch[index]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, row := range models {
		report.Models = append(report.Models, *row)
	}
	sort.Slice(report.Models, func(i, j int) bool {
		a, b := report.Models[i], report.Models[j]
		if a.RevenueMicro != b.RevenueMicro {
			return a.RevenueMicro > b.RevenueMicro
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.ViaHajimi && !b.ViaHajimi
	})
	for _, info := range channels {
		// 列出所有识别为 hajimi 的现存系统渠道（方便核对规则），以及区间内有订单的渠道。
		if info.seen || (info.ViaHajimi && !info.Deleted) {
			report.Channels = append(report.Channels, info.MarginChannel)
		}
	}
	sort.Slice(report.Channels, func(i, j int) bool {
		a, b := report.Channels[i], report.Channels[j]
		if a.ViaHajimi != b.ViaHajimi {
			return a.ViaHajimi
		}
		if a.RevenueMicro != b.RevenueMicro {
			return a.RevenueMicro > b.RevenueMicro
		}
		return a.Name < b.Name
	})
	s.applyMarginHajimi(ctx, report, window)
	report.Totals = sumMarginDays(report.Days, report.Hajimi.Status == MarginHajimiOK)
	return report, nil
}

func (s *Service) addMarginOrder(report *MarginReport, window *marginWindow, dayIndex map[string]int, channels map[string]*marginChannelInfo, models map[[2]string]*MarginModel, order *model.BillingOrder) error {
	if order.CreatedAt.Before(window.start) || !order.CreatedAt.Before(window.end) {
		return nil
	}
	if order.Status != model.BillingStatusSettled {
		report.OpenOrders++
		report.OpenReservedMicro += order.ReservedAmountMicrocredits
		return nil
	}
	index, ok := dayIndex[order.CreatedAt.In(window.loc).Format(marginDateLayout)]
	if !ok {
		return nil
	}
	info := channels[order.ChannelID]
	if info == nil {
		info = &marginChannelInfo{MarginChannel: MarginChannel{ID: order.ChannelID, Name: "未知渠道", Deleted: true}}
		channels[order.ChannelID] = info
	}
	cost, err := s.host.BillingOrderCost(*order)
	if err != nil {
		return err
	}
	revenue := order.ActualAmountMicrocredits
	day := &report.Days[index]
	if info.ViaHajimi {
		day.HajimiOrders++
		day.HajimiRevenueMicro += revenue
	} else {
		day.OtherOrders++
		day.OtherRevenueMicro += revenue
		if cost == nil {
			day.OtherUncostedOrders++
		} else {
			day.OtherEstimatedCostMicro += *cost
		}
	}
	modelName := strings.TrimSpace(order.Model)
	if modelName == "" {
		modelName = marginUnknownModel
	}
	key := [2]string{modelName, "other"}
	if info.ViaHajimi {
		key[1] = "hajimi"
	}
	row := models[key]
	if row == nil {
		row = &MarginModel{Model: modelName, ViaHajimi: info.ViaHajimi}
		models[key] = row
	}
	row.Orders++
	row.RevenueMicro += revenue
	if cost == nil {
		row.UncostedOrders++
	} else {
		row.EstimatedCostMicro += *cost
	}
	info.seen = true
	info.Orders++
	info.RevenueMicro += revenue
	return nil
}

func (s *Service) applyMarginHajimi(ctx context.Context, report *MarginReport, window *marginWindow) {
	report.Hajimi = MarginHajimi{
		ScopeUserIDs: []int64{}, ScopeAPIKeyIDs: []int64{},
		Keys: []MarginCostKey{}, Models: []MarginCostModel{}, Accounts: []MarginCostAccount{},
	}
	if !s.config.Active() {
		report.Hajimi.Status, report.Hajimi.Message = MarginHajimiDisabled, "hajimi 余额互通未启用，无法读取 hajimi 成本"
		return
	}
	if ctx.Err() != nil {
		// 管理员已离开或换了查询条件，不必再去问 hajimi。
		report.Hajimi.Status, report.Hajimi.Message = MarginHajimiUnavailable, "请求已取消"
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, marginRemoteTimeout)
	remote, err := s.client.CostDaily(callCtx, window.from, window.to, window.offset)
	cancel()
	if err != nil {
		report.Hajimi.Status, report.Hajimi.Message = MarginHajimiUnavailable, "读取 hajimi 成本失败："+safeBridgeError(err)
		return
	}
	if remote.From != window.from || remote.To != window.to || remote.UTCOffsetMinutes != window.offset {
		report.Hajimi.Status, report.Hajimi.Message = MarginHajimiUnavailable, "hajimi 返回的统计区间与请求不一致"
		return
	}
	if remote.Scope.UserIDs != nil {
		report.Hajimi.ScopeUserIDs = remote.Scope.UserIDs
	}
	if remote.Scope.APIKeyIDs != nil {
		report.Hajimi.ScopeAPIKeyIDs = remote.Scope.APIKeyIDs
	}
	if !remote.Configured {
		report.Hajimi.Status = MarginHajimiNotConfigured
		report.Hajimi.Message = "hajimi 未配置成本范围（YINGCE_COST_USER_IDS / YINGCE_COST_API_KEY_IDS），只显示影策收入"
		return
	}
	report.Hajimi.Status = MarginHajimiOK
	remoteDays := make(map[string]RemoteCostAmounts, len(remote.Days))
	for _, day := range remote.Days {
		amounts := remoteDays[day.Date]
		amounts.Requests += day.Requests
		amounts.ChargedMicro += day.ChargedMicro
		amounts.UpstreamCostMicro += day.UpstreamCostMicro
		remoteDays[day.Date] = amounts
	}
	for index := range report.Days {
		day := &report.Days[index]
		amounts := remoteDays[day.Date]
		requests, charged, upstream := amounts.Requests, amounts.ChargedMicro, amounts.UpstreamCostMicro
		gross, combined := day.HajimiRevenueMicro-charged, day.HajimiRevenueMicro-upstream
		day.HajimiRequests, day.HajimiChargedMicro, day.HajimiUpstreamCostMicro = &requests, &charged, &upstream
		day.GrossProfitMicro, day.CombinedProfitMicro = &gross, &combined
	}
	for _, key := range remote.Keys {
		report.Hajimi.Keys = append(report.Hajimi.Keys, MarginCostKey{APIKeyID: key.APIKeyID, Name: key.Name, UserID: key.UserID, MarginCostAmounts: marginCostAmounts(key.RemoteCostAmounts)})
	}
	for _, row := range remote.Models {
		report.Hajimi.Models = append(report.Hajimi.Models, MarginCostModel{Model: row.Model, MarginCostAmounts: marginCostAmounts(row.RemoteCostAmounts)})
	}
	for _, account := range remote.Accounts {
		report.Hajimi.Accounts = append(report.Hajimi.Accounts, MarginCostAccount{UserID: account.UserID, BalanceMicro: account.BalanceMicro})
	}
}

func marginCostAmounts(remote RemoteCostAmounts) MarginCostAmounts {
	return MarginCostAmounts{Requests: remote.Requests, ChargedMicro: remote.ChargedMicro, UpstreamCostMicro: remote.UpstreamCostMicro}
}

func sumMarginDays(days []MarginDay, withHajimi bool) MarginDay {
	var total MarginDay
	var requests, charged, upstream int64
	for _, day := range days {
		total.HajimiOrders += day.HajimiOrders
		total.HajimiRevenueMicro += day.HajimiRevenueMicro
		total.OtherOrders += day.OtherOrders
		total.OtherRevenueMicro += day.OtherRevenueMicro
		total.OtherEstimatedCostMicro += day.OtherEstimatedCostMicro
		total.OtherUncostedOrders += day.OtherUncostedOrders
		if withHajimi && day.HajimiChargedMicro != nil {
			requests += *day.HajimiRequests
			charged += *day.HajimiChargedMicro
			upstream += *day.HajimiUpstreamCostMicro
		}
	}
	if withHajimi {
		gross, combined := total.HajimiRevenueMicro-charged, total.HajimiRevenueMicro-upstream
		total.HajimiRequests, total.HajimiChargedMicro, total.HajimiUpstreamCostMicro = &requests, &charged, &upstream
		total.GrossProfitMicro, total.CombinedProfitMicro = &gross, &combined
	}
	return total
}
