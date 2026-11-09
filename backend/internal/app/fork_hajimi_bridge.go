// hajimi 余额互通（fork 专有）的组合根：把 internal/hajimibridge 域服务接到 app.Service。

package app

import (
	"context"
	"sync"

	"yingce/backend/internal/hajimibridge"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

type (
	HajimiBridgeWallet                = hajimibridge.Wallet
	HajimiBridgeTransfer              = hajimibridge.Transfer
	HajimiBridgeTransferPage          = hajimibridge.TransferPage
	HajimiBridgeCreateTransferRequest = hajimibridge.CreateTransferRequest
	HajimiBridgeAutoTopup             = hajimibridge.AutoTopup
	HajimiBridgeAutoTopupRequest      = hajimibridge.AutoTopupRequest
	HajimiBridgeOverview              = hajimibridge.Overview
	HajimiBridgeAdminTransfer         = hajimibridge.AdminTransfer
	HajimiBridgeMarginReport          = hajimibridge.MarginReport
	HajimiBridgeMarginRequest         = hajimibridge.MarginRequest
)

// forkHajimiBridgeHolder 延迟创建域服务；零值可用，测试直接构造 Service 字面量时也不需要初始化。
type forkHajimiBridgeHolder struct {
	once sync.Once
	svc  *hajimibridge.Service
}

type hajimiBridgeHost struct{ svc *Service }

func (h hajimiBridgeHost) RequireAdmin(user *model.User) error { return h.svc.RequireAdmin(user) }

func (h hajimiBridgeHost) RequireCreditsFeature() error { return h.svc.RequireFeature(FeatureCredits) }

func (h hajimiBridgeHost) BillingOrderCost(order model.BillingOrder) (*int64, error) {
	return billingCreditCost(order)
}

func (s *Service) hajimiBridgeDomain() *hajimibridge.Service {
	s.hajimiBridge.once.Do(func() {
		s.hajimiBridge.svc = hajimibridge.New(s.repo, hajimiBridgeHost{svc: s}, hajimibridge.LoadConfigFromEnv())
	})
	return s.hajimiBridge.svc
}

func (s *Service) startHajimiBridgeWorker() {
	s.hajimiBridgeDomain().StartWorkers(s.runWorkerLoop)
}

// InsufficientCredits 是积分不足的统一错误（reason=insufficient_credits）。
func InsufficientCredits(message string) *AppError {
	return kernel.InsufficientCredits(message)
}

func (s *Service) HajimiBridgeWallet(ctx context.Context, user *model.User) (*HajimiBridgeWallet, error) {
	return s.hajimiBridgeDomain().Wallet(ctx, user)
}

func (s *Service) CreateHajimiBridgeTransfer(ctx context.Context, user *model.User, req HajimiBridgeCreateTransferRequest) (*HajimiBridgeTransfer, error) {
	return s.hajimiBridgeDomain().CreateTransfer(ctx, user, req)
}

func (s *Service) HajimiBridgeTransfers(user *model.User, page int, pageSize int) (*HajimiBridgeTransferPage, error) {
	return s.hajimiBridgeDomain().Transfers(user, page, pageSize)
}

func (s *Service) UpdateHajimiBridgeAutoTopup(ctx context.Context, user *model.User, req HajimiBridgeAutoTopupRequest) (*HajimiBridgeAutoTopup, error) {
	return s.hajimiBridgeDomain().UpdateAutoTopup(ctx, user, req)
}

func (s *Service) AdminHajimiBridgeOverview(ctx context.Context, actor *model.User) (*HajimiBridgeOverview, error) {
	return s.hajimiBridgeDomain().AdminOverview(ctx, actor)
}

func (s *Service) AdminRetryHajimiBridgeTransfer(ctx context.Context, actor *model.User, id string) (*HajimiBridgeAdminTransfer, error) {
	return s.hajimiBridgeDomain().AdminRetryTransfer(ctx, actor, id)
}

func (s *Service) AdminHajimiBridgeMargin(ctx context.Context, actor *model.User, req HajimiBridgeMarginRequest) (*HajimiBridgeMarginReport, error) {
	return s.hajimiBridgeDomain().AdminMarginReport(ctx, actor, req)
}
