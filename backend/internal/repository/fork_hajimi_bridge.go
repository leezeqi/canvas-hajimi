// hajimi 互通（fork 专有）的持久化：划转状态机、积分账本与同步状态。
//
// 资金规则：所有本地加减积分都写带唯一 reference_key 的账本条目（重复执行只入账一次）；
// 状态迁移一律是带 WHERE status IN (...) 的条件更新，不做无条件的读后写。
// SQLite 事务先写后读，避免读快照升级写锁时的 BUSY_SNAPSHOT。

package repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
)

var (
	ErrHajimiBridgeDuplicate     = errors.New("hajimi 划转记录已存在")
	ErrHajimiBridgeNetInExceeded = errors.New("转回金额超过可转回额度")
	ErrHajimiBridgeDebitMissing  = errors.New("hajimi 转回缺少本地扣款记录")
	ErrHajimiBridgeAutoSkipped   = errors.New("自动转入条件已不满足")
)

func HajimiInReferenceKey(id string) string        { return "hajimi:in:" + id }
func HajimiOutReferenceKey(id string) string       { return "hajimi:out:" + id }
func HajimiOutRefundReferenceKey(id string) string { return "hajimi:out-refund:" + id }

func (r *Repository) ForkHajimiSchemaReady() (bool, error) {
	return database.ForkHajimiSchemaReady(r.db)
}

// HajimiLinkedSubjects 返回用户名下全部 hajimi: 前缀的 linuxdo 身份 subject。
func (r *Repository) HajimiLinkedSubjects(userID string) ([]string, error) {
	var subjects []string
	err := r.db.Model(&model.UserIdentity{}).
		Where("user_id = ? AND provider = ? AND subject LIKE ?", userID, "linuxdo", "hajimi:%").
		Order("subject asc").Pluck("subject", &subjects).Error
	return subjects, err
}

type HajimiLinkedUser struct {
	UserID   string
	Subjects []string
	Role     model.UserRole
	Status   model.UserStatus
}

// HajimiLinkedUsers 按 user_id 游标分页列出有 hajimi 身份的影策用户。
func (r *Repository) HajimiLinkedUsers(afterUserID string, limit int) ([]HajimiLinkedUser, error) {
	var userIDs []string
	if err := r.db.Model(&model.UserIdentity{}).
		Where("provider = ? AND subject LIKE ? AND user_id > ?", "linuxdo", "hajimi:%", afterUserID).
		Distinct("user_id").Order("user_id asc").Limit(limit).Pluck("user_id", &userIDs).Error; err != nil {
		return nil, err
	}
	if len(userIDs) == 0 {
		return []HajimiLinkedUser{}, nil
	}
	var rows []struct {
		UserID  string
		Subject string
		Role    model.UserRole
		Status  model.UserStatus
	}
	if err := r.db.Table("user_identities").
		Select("user_identities.user_id, user_identities.subject, users.role, users.status").
		Joins("JOIN users ON users.id = user_identities.user_id").
		Where("user_identities.provider = ? AND user_identities.subject LIKE ? AND user_identities.user_id IN ?", "linuxdo", "hajimi:%", userIDs).
		Order("user_identities.user_id asc, user_identities.subject asc").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]HajimiLinkedUser, 0, len(userIDs))
	for _, row := range rows {
		if len(result) == 0 || result[len(result)-1].UserID != row.UserID {
			result = append(result, HajimiLinkedUser{UserID: row.UserID, Role: row.Role, Status: row.Status})
		}
		last := &result[len(result)-1]
		last.Subjects = append(last.Subjects, row.Subject)
	}
	return result, nil
}

func (r *Repository) HajimiLinkedUserCount() (int64, error) {
	var count int64
	err := r.db.Model(&model.UserIdentity{}).
		Where("provider = ? AND subject LIKE ?", "linuxdo", "hajimi:%").
		Distinct("user_id").Count(&count).Error
	return count, err
}

func (r *Repository) HajimiBridgeTransfer(id string) (*model.HajimiBridgeTransfer, error) {
	var row model.HajimiBridgeTransfer
	if err := r.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) HajimiBridgeTransferByClientKey(userID string, clientKey string) (*model.HajimiBridgeTransfer, error) {
	var row model.HajimiBridgeTransfer
	if err := r.db.First(&row, "user_id = ? AND client_key = ?", userID, clientKey).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// CreateHajimiBridgeInTransfer 只登记待处理的转入；唯一键冲突返回 ErrHajimiBridgeDuplicate，由调用方重读。
func (r *Repository) CreateHajimiBridgeInTransfer(row *model.HajimiBridgeTransfer) error {
	created := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected == 0 {
		return ErrHajimiBridgeDuplicate
	}
	return nil
}

// CreateHajimiBridgeOutTransfer 在同一事务里锁定积分账户、校验本地 net_in 与可用积分、
// 登记 pending 转回并先扣本地积分（hajimi:out:<id>）。
func (r *Repository) CreateHajimiBridgeOutTransfer(row *model.HajimiBridgeTransfer, note string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		account, err := lockHajimiCreditAccount(tx, row.UserID)
		if err != nil {
			return err
		}
		if row.ClientKey != nil {
			var existing int64
			if err := tx.Model(&model.HajimiBridgeTransfer{}).Where("user_id = ? AND client_key = ?", row.UserID, *row.ClientKey).Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				return ErrHajimiBridgeDuplicate
			}
		}
		netIn, err := hajimiBridgeNetIn(tx, row.UserID)
		if err != nil {
			return err
		}
		if row.AmountMicrocredits > netIn {
			return ErrHajimiBridgeNetInExceeded
		}
		if account.AvailableMicrocredits < row.AmountMicrocredits {
			return ErrInsufficientCredits
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			return ErrHajimiBridgeDuplicate
		}
		debited, err := debitHajimiCreditsOnceTx(tx, row.UserID, model.CreditLedgerHajimiTransferOut, row.AmountMicrocredits, HajimiOutReferenceKey(row.ID), note)
		if err != nil {
			return err
		}
		if !debited {
			return ErrHajimiBridgeDuplicate
		}
		return nil
	})
}

// ClaimHajimiBridgeAttempt 原子地占用一次发送机会；多实例并发时只有一方会真正发请求。
// createdAfter 之前创建的记录（已过重发窗口）不再允许发送。
func (r *Repository) ClaimHajimiBridgeAttempt(id string, now time.Time, nextAttemptAt time.Time, createdAfter time.Time) (bool, error) {
	updated := r.db.Model(&model.HajimiBridgeTransfer{}).
		Where("id = ? AND status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?) AND created_at > ?", id, model.HajimiTransferPending, now, createdAfter).
		Updates(map[string]any{"attempts": gorm.Expr("attempts + 1"), "last_attempt_at": now, "next_attempt_at": nextAttemptAt, "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

// ClaimHajimiBridgeLookup 占用一次窗口后的定论查询：以库中最新的 last_attempt_at 为准，
// 只有最后一次发送早于 sentBefore（已超出签名有效期）时才成功。
func (r *Repository) ClaimHajimiBridgeLookup(id string, now time.Time, sentBefore time.Time, nextAttemptAt time.Time) (bool, error) {
	updated := r.db.Model(&model.HajimiBridgeTransfer{}).
		Where("id = ? AND status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?) AND (last_attempt_at IS NULL OR last_attempt_at <= ?)", id, model.HajimiTransferPending, now, sentBefore).
		Updates(map[string]any{"next_attempt_at": nextAttemptAt, "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

// ClaimHajimiBridgeCheck 占用一次窗口外的等待（不计入发送次数，不更新 last_attempt_at）。
func (r *Repository) ClaimHajimiBridgeCheck(id string, now time.Time, nextAttemptAt time.Time) (bool, error) {
	updated := r.db.Model(&model.HajimiBridgeTransfer{}).
		Where("id = ? AND status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", id, model.HajimiTransferPending, now).
		Updates(map[string]any{"next_attempt_at": nextAttemptAt, "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

func (r *Repository) NoteHajimiBridgeError(id string, lastError string, nextAttemptAt *time.Time, now time.Time) error {
	updates := map[string]any{"last_error": truncateHajimiText(lastError, 500), "updated_at": now}
	if nextAttemptAt != nil {
		updates["next_attempt_at"] = *nextAttemptAt
	}
	return r.db.Model(&model.HajimiBridgeTransfer{}).
		Where("id = ? AND status = ?", id, model.HajimiTransferPending).Updates(updates).Error
}

// RescheduleHajimiBridgeTransfer 由管理员重试使用：让 pending 记录立即到期。
func (r *Repository) RescheduleHajimiBridgeTransfer(id string, at time.Time) (bool, error) {
	updated := r.db.Model(&model.HajimiBridgeTransfer{}).
		Where("id = ? AND status = ?", id, model.HajimiTransferPending).
		Updates(map[string]any{"next_attempt_at": at, "updated_at": at})
	return updated.RowsAffected == 1, updated.Error
}

// ApplyHajimiBridgeInCommitted 把 hajimi 已提交的转入落地：条件迁移到 credited 并以 hajimi:in:<id> 入账一次。
func (r *Repository) ApplyHajimiBridgeInCommitted(id string, seq int64, from []string, note string, now time.Time) (bool, error) {
	applied := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.HajimiBridgeTransfer{}).
			Where("id = ? AND direction = ? AND status IN ?", id, model.HajimiTransferDirectionIn, from).
			Updates(map[string]any{"status": model.HajimiTransferCredited, "reason": "", "hajimi_seq": seq, "next_attempt_at": nil, "last_error": "", "completed_at": now, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return nil
		}
		var row model.HajimiBridgeTransfer
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if _, err := grantHajimiCreditsOnceTx(tx, row.UserID, model.CreditLedgerHajimiTransferIn, row.AmountMicrocredits, HajimiInReferenceKey(id), note); err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

// ResolveHajimiBridgeTransfer 是不涉及本地资金的条件状态迁移（rejected/failed/completed/attention）。
func (r *Repository) ResolveHajimiBridgeTransfer(id string, from []string, to string, reason string, seq int64, now time.Time) (bool, error) {
	updates := map[string]any{"status": to, "reason": truncateHajimiText(reason, 64), "next_attempt_at": nil, "updated_at": now}
	if seq > 0 {
		updates["hajimi_seq"] = seq
	}
	if to != model.HajimiTransferAttention {
		updates["completed_at"] = now
		updates["last_error"] = ""
	}
	updated := r.db.Model(&model.HajimiBridgeTransfer{}).Where("id = ? AND status IN ?", id, from).Updates(updates)
	return updated.RowsAffected == 1, updated.Error
}

// RefundHajimiBridgeOut 条件迁移到 refunded 并以 hajimi:out-refund:<id> 退回本地扣款一次。
func (r *Repository) RefundHajimiBridgeOut(id string, from []string, reason string, note string, now time.Time) (bool, error) {
	applied := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.HajimiBridgeTransfer{}).
			Where("id = ? AND direction = ? AND status IN ?", id, model.HajimiTransferDirectionOut, from).
			Updates(map[string]any{"status": model.HajimiTransferRefunded, "reason": truncateHajimiText(reason, 64), "next_attempt_at": nil, "last_error": "", "completed_at": now, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return nil
		}
		var row model.HajimiBridgeTransfer
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		var debits int64
		if err := tx.Model(&model.CreditLedgerEntry{}).Where("reference_key = ?", HajimiOutReferenceKey(id)).Count(&debits).Error; err != nil {
			return err
		}
		if debits == 0 {
			return ErrHajimiBridgeDebitMissing
		}
		if _, err := grantHajimiCreditsOnceTx(tx, row.UserID, model.CreditLedgerHajimiTransferOutRefund, row.AmountMicrocredits, HajimiOutRefundReferenceKey(id), note); err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

// InsertHajimiBridgeAttention 为 hajimi 已提交但本地没有记录的划转补一条待人工处理记录。
func (r *Repository) InsertHajimiBridgeAttention(row *model.HajimiBridgeTransfer) (bool, error) {
	created := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	return created.RowsAffected == 1, created.Error
}

func (r *Repository) DueHajimiBridgeTransfers(now time.Time, limit int) ([]model.HajimiBridgeTransfer, error) {
	var rows []model.HajimiBridgeTransfer
	err := r.db.Where("status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", model.HajimiTransferPending, now).
		Order("next_attempt_at asc, created_at asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *Repository) HajimiBridgeTransfersForUser(userID string, limit int, offset int) ([]model.HajimiBridgeTransfer, int64, error) {
	var rows []model.HajimiBridgeTransfer
	var total int64
	query := r.db.Model(&model.HajimiBridgeTransfer{}).Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at desc, id desc").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

// HajimiBridgePendingCount 统计 pending 划转；userID 为空时统计全部用户。
func (r *Repository) HajimiBridgePendingCount(userID string) (int64, error) {
	var count int64
	query := r.db.Model(&model.HajimiBridgeTransfer{}).Where("status = ?", model.HajimiTransferPending)
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	err := query.Count(&count).Error
	return count, err
}

func (r *Repository) HajimiBridgeLocalNetIn(userID string) (int64, error) {
	return hajimiBridgeNetIn(r.db, userID)
}

// HajimiBridgeTotals 返回本地 Σ credited 转入与 Σ completed 转回，用于和 hajimi 汇总对账。
func (r *Repository) HajimiBridgeTotals() (int64, int64, error) {
	var credited, completed int64
	if err := r.db.Model(&model.HajimiBridgeTransfer{}).Where("direction = ? AND status = ?", model.HajimiTransferDirectionIn, model.HajimiTransferCredited).
		Select("COALESCE(SUM(amount_microcredits), 0)").Scan(&credited).Error; err != nil {
		return 0, 0, err
	}
	if err := r.db.Model(&model.HajimiBridgeTransfer{}).Where("direction = ? AND status = ?", model.HajimiTransferDirectionOut, model.HajimiTransferCompleted).
		Select("COALESCE(SUM(amount_microcredits), 0)").Scan(&completed).Error; err != nil {
		return 0, 0, err
	}
	return credited, completed, nil
}

func (r *Repository) HajimiBridgeStatusCount(status string) (int64, error) {
	var count int64
	err := r.db.Model(&model.HajimiBridgeTransfer{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

type HajimiBridgeAttentionRow struct {
	model.HajimiBridgeTransfer
	Username string
}

func (r *Repository) HajimiBridgeAttentionTransfers(limit int) ([]HajimiBridgeAttentionRow, error) {
	var rows []HajimiBridgeAttentionRow
	err := r.db.Table("hajimi_bridge_transfers").
		Select("hajimi_bridge_transfers.*, COALESCE(users.username, '') AS username").
		Joins("LEFT JOIN users ON users.id = hajimi_bridge_transfers.user_id").
		Where("hajimi_bridge_transfers.status = ?", model.HajimiTransferAttention).
		Order("hajimi_bridge_transfers.updated_at desc").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *Repository) HajimiBridgeHasPendingIn(userID string) (bool, error) {
	var count int64
	err := r.db.Model(&model.HajimiBridgeTransfer{}).
		Where("user_id = ? AND direction = ? AND status = ?", userID, model.HajimiTransferDirectionIn, model.HajimiTransferPending).Count(&count).Error
	return count > 0, err
}

// HajimiBridgeAutoInSince 汇总自动转入在窗口内占用的额度；rejected/failed 没有动钱，不占额度。
func (r *Repository) HajimiBridgeAutoInSince(userID string, since time.Time) (int64, error) {
	return hajimiBridgeAutoInSince(r.db, userID, since)
}

func hajimiBridgeAutoInSince(tx *gorm.DB, userID string, since time.Time) (int64, error) {
	var total int64
	err := tx.Model(&model.HajimiBridgeTransfer{}).
		Where("user_id = ? AND direction = ? AND source = ? AND status IN ? AND created_at >= ?", userID, model.HajimiTransferDirectionIn, model.HajimiTransferSourceAuto,
			[]string{model.HajimiTransferPending, model.HajimiTransferCredited, model.HajimiTransferAttention}, since).
		Select("COALESCE(SUM(amount_microcredits), 0)").Scan(&total).Error
	return total, err
}

// CreateHajimiBridgeAutoInTransfer 在锁定该用户积分账户后复查阈值、“无 pending 转入”和 24 小时自动额度，
// 条件仍满足才登记自动转入；多实例在同一用户上串行，不会各自插入一笔。
func (r *Repository) CreateHajimiBridgeAutoInTransfer(row *model.HajimiBridgeTransfer, threshold int64, dailyLimit int64, since time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		account, err := lockHajimiCreditAccount(tx, row.UserID)
		if err != nil {
			return err
		}
		if account.AvailableMicrocredits >= threshold {
			return ErrHajimiBridgeAutoSkipped
		}
		var pending int64
		if err := tx.Model(&model.HajimiBridgeTransfer{}).
			Where("user_id = ? AND direction = ? AND status = ?", row.UserID, model.HajimiTransferDirectionIn, model.HajimiTransferPending).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return ErrHajimiBridgeAutoSkipped
		}
		used, err := hajimiBridgeAutoInSince(tx, row.UserID, since)
		if err != nil {
			return err
		}
		if used+row.AmountMicrocredits > dailyLimit {
			return ErrHajimiBridgeAutoSkipped
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			return ErrHajimiBridgeDuplicate
		}
		return nil
	})
}

// HajimiBridgeLatestIn 返回用户最近一笔转入；source 为空表示不限来源。
func (r *Repository) HajimiBridgeLatestIn(userID string, source string) (*model.HajimiBridgeTransfer, error) {
	var rows []model.HajimiBridgeTransfer
	query := r.db.Where("user_id = ? AND direction = ?", userID, model.HajimiTransferDirectionIn)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Order("created_at desc, id desc").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Repository) HajimiBridgeUserSettings(userID string) (*model.HajimiBridgeUserSettings, error) {
	var rows []model.HajimiBridgeUserSettings
	if err := r.db.Where("user_id = ?", userID).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Repository) SaveHajimiBridgeUserSettings(settings *model.HajimiBridgeUserSettings) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"auto_topup_enabled", "threshold_microcredits", "amount_microcredits", "daily_limit_microcredits", "updated_at"}),
	}).Create(settings).Error
}

func (r *Repository) HajimiBridgeAutoTopupSettings(afterUserID string, limit int) ([]model.HajimiBridgeUserSettings, error) {
	var rows []model.HajimiBridgeUserSettings
	err := r.db.Where("auto_topup_enabled = ? AND user_id > ?", true, afterUserID).Order("user_id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *Repository) HajimiBridgeUserState(userID string) (*model.HajimiBridgeUserState, error) {
	var rows []model.HajimiBridgeUserState
	if err := r.db.Where("user_id = ?", userID).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Repository) SaveHajimiBridgeUserState(state *model.HajimiBridgeUserState) error {
	return saveHajimiBridgeUserState(r.db, state)
}

func saveHajimiBridgeUserState(tx *gorm.DB, state *model.HajimiBridgeUserState) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"hajimi_user_id", "disabled_by_bridge", "last_status", "last_grant", "last_epoch", "checked_at"}),
	}).Create(state).Error
}

// DisableUserByHajimiBridge 只停用仍为 active 的非管理员用户，并在同一事务清除其登录会话。
func (r *Repository) DisableUserByHajimiBridge(state *model.HajimiBridgeUserState, now time.Time) (bool, error) {
	changed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.User{}).
			Where("id = ? AND status = ? AND role <> ?", state.UserID, model.UserStatusActive, model.UserRoleAdmin).
			Updates(map[string]any{"status": model.UserStatusDisabled, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 1 {
			changed = true
			state.DisabledByBridge = true
			if err := tx.Delete(&model.AuthSession{}, "user_id = ?", state.UserID).Error; err != nil {
				return err
			}
		}
		return saveHajimiBridgeUserState(tx, state)
	})
	return changed, err
}

// EnableUserByHajimiBridge 只恢复由互通停用的用户。
func (r *Repository) EnableUserByHajimiBridge(state *model.HajimiBridgeUserState, now time.Time) (bool, error) {
	changed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.User{}).
			Where("id = ? AND status = ?", state.UserID, model.UserStatusDisabled).
			Updates(map[string]any{"status": model.UserStatusActive, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		changed = updated.RowsAffected == 1
		state.DisabledByBridge = false
		return saveHajimiBridgeUserState(tx, state)
	})
	return changed, err
}

func (r *Repository) HajimiBridgeStateValue(key string) (string, bool, error) {
	var rows []model.HajimiBridgeState
	if err := r.db.Where("key = ?", key).Limit(1).Find(&rows).Error; err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return rows[0].ValueJSON, true, nil
}

func (r *Repository) SaveHajimiBridgeStateValue(key string, value string, now time.Time) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value_json", "updated_at"}),
	}).Create(&model.HajimiBridgeState{Key: key, ValueJSON: value, UpdatedAt: now}).Error
}

// hajimiBridgeNetIn = Σ credited 转入 − Σ (pending+completed+attention) 转回。
// attention 转回已在本地扣款且结果未定，按占用额度计算，避免人工处理前被重复提回。
func hajimiBridgeNetIn(tx *gorm.DB, userID string) (int64, error) {
	var credited, withdrawn int64
	if err := tx.Model(&model.HajimiBridgeTransfer{}).
		Where("user_id = ? AND direction = ? AND status = ?", userID, model.HajimiTransferDirectionIn, model.HajimiTransferCredited).
		Select("COALESCE(SUM(amount_microcredits), 0)").Scan(&credited).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&model.HajimiBridgeTransfer{}).
		Where("user_id = ? AND direction = ? AND status IN ?", userID, model.HajimiTransferDirectionOut,
			[]string{model.HajimiTransferPending, model.HajimiTransferCompleted, model.HajimiTransferAttention}).
		Select("COALESCE(SUM(amount_microcredits), 0)").Scan(&withdrawn).Error; err != nil {
		return 0, err
	}
	return credited - withdrawn, nil
}

// lockHajimiCreditAccount 先写（确保账户存在，SQLite 借此拿到写锁）再按行锁读取。
func lockHajimiCreditAccount(tx *gorm.DB, userID string) (*model.CreditAccount, error) {
	account := model.CreditAccount{UserID: userID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return nil, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func grantHajimiCreditsOnceTx(tx *gorm.DB, userID string, entryType model.CreditLedgerType, amount int64, referenceKey string, note string) (bool, error) {
	account := model.CreditAccount{UserID: userID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return false, err
	}
	entry := model.CreditLedgerEntry{ID: newRepositoryID(), UserID: userID, Type: entryType, AmountMicrocredits: amount, ReferenceKey: &referenceKey, Note: note}
	created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "reference_key"}}, DoNothing: true}).Create(&entry)
	if created.Error != nil {
		return false, created.Error
	}
	if created.RowsAffected == 0 {
		return false, nil
	}
	if err := tx.Model(&model.CreditAccount{}).Where("user_id = ?", userID).Updates(map[string]any{
		"available_microcredits": gorm.Expr("available_microcredits + ?", amount),
		"version":                gorm.Expr("version + 1"),
		"updated_at":             time.Now(),
	}).Error; err != nil {
		return false, err
	}
	if err := tx.First(&account, "user_id = ?", userID).Error; err != nil {
		return false, err
	}
	return true, tx.Model(&entry).Updates(map[string]any{
		"available_delta_microcredits": amount,
		"available_after_microcredits": account.AvailableMicrocredits,
		"reserved_after_microcredits":  account.ReservedMicrocredits,
	}).Error
}

func debitHajimiCreditsOnceTx(tx *gorm.DB, userID string, entryType model.CreditLedgerType, amount int64, referenceKey string, note string) (bool, error) {
	entry := model.CreditLedgerEntry{ID: newRepositoryID(), UserID: userID, Type: entryType, AmountMicrocredits: -amount, ReferenceKey: &referenceKey, Note: note}
	created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "reference_key"}}, DoNothing: true}).Create(&entry)
	if created.Error != nil {
		return false, created.Error
	}
	if created.RowsAffected == 0 {
		return false, nil
	}
	updated := tx.Model(&model.CreditAccount{}).Where("user_id = ? AND available_microcredits >= ?", userID, amount).Updates(map[string]any{
		"available_microcredits": gorm.Expr("available_microcredits - ?", amount),
		"version":                gorm.Expr("version + 1"),
		"updated_at":             time.Now(),
	})
	if updated.Error != nil {
		return false, updated.Error
	}
	if updated.RowsAffected != 1 {
		return false, ErrInsufficientCredits
	}
	var account model.CreditAccount
	if err := tx.First(&account, "user_id = ?", userID).Error; err != nil {
		return false, err
	}
	return true, tx.Model(&entry).Updates(map[string]any{
		"available_delta_microcredits": -amount,
		"available_after_microcredits": account.AvailableMicrocredits,
		"reserved_after_microcredits":  account.ReservedMicrocredits,
	}).Error
}

func truncateHajimiText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	for len(string(runes)) > limit {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
