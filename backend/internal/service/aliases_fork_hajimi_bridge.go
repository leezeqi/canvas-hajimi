package service

import "yingce/backend/internal/app"

type (
	HajimiBridgeWallet                = app.HajimiBridgeWallet
	HajimiBridgeTransfer              = app.HajimiBridgeTransfer
	HajimiBridgeTransferPage          = app.HajimiBridgeTransferPage
	HajimiBridgeCreateTransferRequest = app.HajimiBridgeCreateTransferRequest
	HajimiBridgeAutoTopup             = app.HajimiBridgeAutoTopup
	HajimiBridgeAutoTopupRequest      = app.HajimiBridgeAutoTopupRequest
	HajimiBridgeOverview              = app.HajimiBridgeOverview
	HajimiBridgeAdminTransfer         = app.HajimiBridgeAdminTransfer
	HajimiBridgeMarginReport          = app.HajimiBridgeMarginReport
	HajimiBridgeMarginRequest         = app.HajimiBridgeMarginRequest
)

var InsufficientCredits = app.InsufficientCredits
