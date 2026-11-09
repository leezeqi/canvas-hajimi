// hajimi 互通（fork 专有）：划转记录、用户设置与同步状态。
//
// 这些表由 internal/database/fork_hajimi_schema.go 的独立迁移创建，不占用上游 CurrentSchemaVersion。

package model

import "time"

const (
	// 账本类型：金额按 1 积分 = 1 美元、微积分 = micro-USD 固定 1:1 记账。
	CreditLedgerHajimiTransferIn        CreditLedgerType = "hajimi_transfer_in"
	CreditLedgerHajimiTransferOut       CreditLedgerType = "hajimi_transfer_out"
	CreditLedgerHajimiTransferOutRefund CreditLedgerType = "hajimi_transfer_out_refund"

	HajimiTransferDirectionIn  = "in"
	HajimiTransferDirectionOut = "out"

	HajimiTransferSourceManual    = "manual"
	HajimiTransferSourcePrompt    = "prompt"
	HajimiTransferSourceAuto      = "auto"
	HajimiTransferSourceReconcile = "reconcile"

	HajimiTransferPending   = "pending"
	HajimiTransferCredited  = "credited"
	HajimiTransferRejected  = "rejected"
	HajimiTransferFailed    = "failed"
	HajimiTransferAttention = "attention"
	HajimiTransferCompleted = "completed"
	HajimiTransferRefunded  = "refunded"
)

// HajimiBridgeTransfer 的 ID 同时是 hajimi 侧 transfer_id（yct_ + 32 位小写十六进制）。
type HajimiBridgeTransfer struct {
	ID                 string     `json:"id" gorm:"primaryKey;size:40"`
	UserID             string     `json:"userId" gorm:"size:36;not null;index:idx_hajimi_bridge_transfers_user_created,priority:1;uniqueIndex:idx_hajimi_bridge_transfers_user_client_key,priority:1"`
	HajimiUserID       int64      `json:"hajimiUserId" gorm:"not null;index"`
	Direction          string     `json:"direction" gorm:"size:8;not null"`
	AmountMicrocredits int64      `json:"amountMicrocredits" gorm:"not null"`
	ClientKey          *string    `json:"-" gorm:"size:80;uniqueIndex:idx_hajimi_bridge_transfers_user_client_key,priority:2"`
	Source             string     `json:"source" gorm:"size:16;not null"`
	Status             string     `json:"status" gorm:"size:16;not null;index:idx_hajimi_bridge_transfers_status_next,priority:1"`
	Reason             string     `json:"reason" gorm:"size:64;not null;default:''"`
	HajimiSeq          int64      `json:"hajimiSeq" gorm:"not null;default:0"`
	Attempts           int        `json:"attempts" gorm:"not null;default:0"`
	NextAttemptAt      *time.Time `json:"nextAttemptAt" gorm:"index:idx_hajimi_bridge_transfers_status_next,priority:2"`
	LastAttemptAt      *time.Time `json:"lastAttemptAt"`
	LastError          string     `json:"lastError" gorm:"size:500;not null;default:''"`
	CreatedAt          time.Time  `json:"createdAt" gorm:"index:idx_hajimi_bridge_transfers_user_created,priority:2"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	CompletedAt        *time.Time `json:"completedAt"`
}

func (HajimiBridgeTransfer) TableName() string { return "hajimi_bridge_transfers" }

type HajimiBridgeUserSettings struct {
	UserID                 string    `json:"userId" gorm:"primaryKey;size:36"`
	AutoTopupEnabled       bool      `json:"autoTopupEnabled" gorm:"not null;default:false;index"`
	ThresholdMicrocredits  int64     `json:"thresholdMicrocredits" gorm:"not null;default:0"`
	AmountMicrocredits     int64     `json:"amountMicrocredits" gorm:"not null;default:0"`
	DailyLimitMicrocredits int64     `json:"dailyLimitMicrocredits" gorm:"not null;default:0"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

func (HajimiBridgeUserSettings) TableName() string { return "hajimi_bridge_user_settings" }

type HajimiBridgeUserState struct {
	UserID           string     `json:"userId" gorm:"primaryKey;size:36"`
	HajimiUserID     int64      `json:"hajimiUserId" gorm:"not null;index"`
	DisabledByBridge bool       `json:"disabledByBridge" gorm:"not null;default:false"`
	LastStatus       string     `json:"lastStatus" gorm:"size:16;not null;default:''"`
	LastGrant        string     `json:"lastGrant" gorm:"size:16;not null;default:''"`
	LastEpoch        string     `json:"lastEpoch" gorm:"size:32;not null;default:''"`
	CheckedAt        *time.Time `json:"checkedAt"`
}

func (HajimiBridgeUserState) TableName() string { return "hajimi_bridge_user_state" }

type HajimiBridgeState struct {
	Key       string    `json:"key" gorm:"primaryKey;size:64"`
	ValueJSON string    `json:"valueJson" gorm:"type:text"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (HajimiBridgeState) TableName() string { return "hajimi_bridge_state" }
