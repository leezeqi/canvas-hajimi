package hajimibridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

const (
	stateKeyCursor        = "cursor"
	stateKeyReconcile     = "reconcile"
	stateKeyReconcileFull = "reconcile_full"

	retryBatchSize     = 50
	cursorPagesPerTick = 20
	// 每日全量复核的安全上限：默认 200 条/页 × 10000 页 = 200 万笔。
	defaultFullScanMaxPages = 10_000
	statusBatchSize         = 500
	statusBatchesPerTick    = 100
	autoTopupBatchSize      = 200
)

// StartWorkers 注册互通后台循环；仅在 HAJIMI_BRIDGE_ENABLED 且配置完整、fork 表已迁移时启动。
// 多实例同时运行是安全的：发送靠条件占用，入账靠账本唯一键，状态迁移都是条件更新。
func (s *Service) StartWorkers(runLoop func(func(context.Context)) bool) {
	if !s.config.Enabled {
		return
	}
	if !s.config.Active() {
		slog.Warn("hajimi bridge enabled but misconfigured; bridge stays off", "invalid", s.config.Problems)
		return
	}
	ready, err := s.repo.ForkHajimiSchemaReady()
	if err != nil || !ready {
		slog.Error("hajimi bridge tables are not migrated; run migrate-schema up", "error", err)
		return
	}
	runLoop(func(ctx context.Context) {
		s.runTicker(ctx, 15*time.Second, func(ctx context.Context) { s.RunRetryPass(ctx) })
	})
	runLoop(func(ctx context.Context) {
		s.runTicker(ctx, time.Minute, func(ctx context.Context) {
			if err := s.RunCursorPass(ctx); err != nil {
				slog.Warn("hajimi cursor pull failed", "error", safeBridgeError(err))
			}
			if err := s.RunStatusSyncPass(ctx); err != nil {
				slog.Warn("hajimi status sync failed", "error", safeBridgeError(err))
			}
			if err := s.RunAutoTopupPass(ctx); err != nil {
				slog.Warn("hajimi auto top-up failed", "error", err)
			}
		})
	})
	runLoop(func(ctx context.Context) {
		s.runTicker(ctx, 5*time.Minute, func(ctx context.Context) {
			if _, err := s.RunReconcilePass(ctx, false); err != nil {
				slog.Warn("hajimi reconcile failed", "error", safeBridgeError(err))
			}
		})
	})
}

func (s *Service) runTicker(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn(ctx)
		}
	}
}

// RunRetryPass 推进到期的 pending 划转（同一 transfer_id 重发或窗口后查询定论）。
func (s *Service) RunRetryPass(ctx context.Context) {
	rows, err := s.repo.DueHajimiBridgeTransfers(s.now(), retryBatchSize)
	if err != nil {
		slog.Error("hajimi pending transfer query failed", "error", err)
		return
	}
	for index := range rows {
		if ctx.Err() != nil {
			return
		}
		if !s.client.Available() {
			return
		}
		s.process(ctx, &rows[index], s.workerTimeout)
	}
}

type cursorState struct {
	AfterSeq int64 `json:"after_seq"`
}

// RunCursorPass 按 seq 拉取 hajimi 已提交划转并补齐本地状态；本地矛盾只标 attention，不自动动钱。
func (s *Service) RunCursorPass(ctx context.Context) error {
	raw, _, err := s.repo.HajimiBridgeStateValue(stateKeyCursor)
	if err != nil {
		return err
	}
	var cursor cursorState
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cursor)
	}
	_, _, err = s.scanCommitted(ctx, cursor.AfterSeq, cursorPagesPerTick, func(seq int64) error {
		encoded, _ := json.Marshal(cursorState{AfterSeq: seq})
		return s.repo.SaveHajimiBridgeStateValue(stateKeyCursor, string(encoded), s.now())
	})
	return err
}

// scanCommitted 从 after 之后逐页处理 hajimi 已提交划转，最多 maxPages 页；
// complete 表示已读到末页。任何一笔处理失败都立即返回，游标只推进到最后一笔成功处理的 seq。
func (s *Service) scanCommitted(ctx context.Context, after int64, maxPages int, persist func(int64) error) (int64, bool, error) {
	for page := 0; page < maxPages; page++ {
		callCtx, cancel := context.WithTimeout(ctx, s.workerTimeout)
		result, err := s.client.ListTransfers(callCtx, after, s.transferPageLimit)
		cancel()
		if err != nil {
			return after, false, err
		}
		processed := after
		for index := range result.Items {
			item := &result.Items[index]
			if item.Seq <= processed || item.Status != remoteStatusCommitted {
				continue
			}
			if err := s.reconcileCommitted(item); err != nil {
				if persist != nil && processed > after {
					_ = persist(processed)
				}
				return processed, false, err
			}
			processed = item.Seq
		}
		if persist != nil && processed > after {
			if err := persist(processed); err != nil {
				return processed, false, err
			}
		}
		if len(result.Items) < s.transferPageLimit || processed == after {
			return processed, true, nil
		}
		after = processed
	}
	return after, false, nil
}

func (s *Service) reconcileCommitted(item *RemoteTransfer) error {
	direction, ok := localDirection(item.Direction)
	if !ok {
		slog.Warn("hajimi committed transfer has unknown direction", "transfer_id", item.TransferID)
		return nil
	}
	row, err := s.repo.HajimiBridgeTransfer(item.TransferID)
	if notFound(err) {
		now := s.now()
		inserted, insertErr := s.repo.InsertHajimiBridgeAttention(&model.HajimiBridgeTransfer{
			ID: item.TransferID, UserID: item.CanvasUserID, HajimiUserID: item.HajimiUserID, Direction: direction, AmountMicrocredits: item.AmountMicro,
			Source: model.HajimiTransferSourceReconcile, Status: model.HajimiTransferAttention, Reason: localReasonMissingLocalRecord, HajimiSeq: item.Seq,
			CreatedAt: now, UpdatedAt: now,
		})
		if insertErr != nil {
			return insertErr
		}
		if inserted {
			slog.Warn("hajimi committed transfer has no local record", "transfer_id", item.TransferID, "canvas_user_id", item.CanvasUserID)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !remoteMatches(row, item) {
		return s.markAttention(row, localReasonRemoteMismatch, []string{model.HajimiTransferPending, model.HajimiTransferFailed, model.HajimiTransferRejected, model.HajimiTransferRefunded})
	}
	now := s.now()
	switch row.Direction + "/" + row.Status {
	case model.HajimiTransferDirectionIn + "/" + model.HajimiTransferPending, model.HajimiTransferDirectionIn + "/" + model.HajimiTransferFailed:
		_, err = s.repo.ApplyHajimiBridgeInCommitted(row.ID, item.Seq, []string{model.HajimiTransferPending, model.HajimiTransferFailed}, transferNote(row.Direction, row.ID), now)
	case model.HajimiTransferDirectionOut + "/" + model.HajimiTransferPending:
		_, err = s.repo.ResolveHajimiBridgeTransfer(row.ID, []string{model.HajimiTransferPending}, model.HajimiTransferCompleted, "", item.Seq, now)
	case model.HajimiTransferDirectionIn + "/" + model.HajimiTransferRejected, model.HajimiTransferDirectionOut + "/" + model.HajimiTransferRefunded:
		err = s.markAttention(row, localReasonCommittedButLocal+row.Status, []string{row.Status})
	}
	return err
}

// RunStatusSyncPass 同步 hajimi 账号状态：停用/删除/不存在 → 停用影策账号并清会话；
// 恢复 active → 恢复由互通停用的账号；授权撤销或 auth_epoch 变化 → 只清会话。管理员不自动停用。
func (s *Service) RunStatusSyncPass(ctx context.Context) error {
	trusted, err := s.slotTrusted()
	if err != nil || !trusted {
		return err
	}
	after := ""
	for batch := 0; batch < statusBatchesPerTick; batch++ {
		users, err := s.repo.HajimiLinkedUsers(after, statusBatchSize)
		if err != nil {
			return err
		}
		if len(users) == 0 {
			return nil
		}
		after = users[len(users)-1].UserID
		byHajimiID := make(map[int64][]int, len(users))
		ids := make([]int64, 0, len(users))
		for index := range users {
			id, linked, _ := singleHajimiID(users[index].Subjects)
			if !linked {
				continue
			}
			if _, seen := byHajimiID[id]; !seen {
				ids = append(ids, id)
			}
			byHajimiID[id] = append(byHajimiID[id], index)
		}
		if len(ids) > 0 {
			callCtx, cancel := context.WithTimeout(ctx, s.workerTimeout)
			statuses, err := s.client.UsersStatus(callCtx, ids)
			cancel()
			if err != nil {
				return err
			}
			for _, remote := range statuses {
				for _, index := range byHajimiID[remote.HajimiUserID] {
					if err := s.applyUserStatus(users[index].UserID, users[index].Role, users[index].Status, remote); err != nil {
						slog.Error("hajimi status apply failed", "user_id", users[index].UserID, "error", err)
					}
				}
			}
		}
		if len(users) < statusBatchSize {
			return nil
		}
	}
	return nil
}

func (s *Service) applyUserStatus(userID string, role model.UserRole, status model.UserStatus, remote RemoteUserStatus) error {
	previous, err := s.repo.HajimiBridgeUserState(userID)
	if err != nil {
		return err
	}
	now := s.now()
	state := &model.HajimiBridgeUserState{UserID: userID}
	if previous != nil {
		*state = *previous
	}
	state.HajimiUserID = remote.HajimiUserID
	state.CheckedAt = &now
	killSessions := false
	if remote.Grant != "active" && (previous == nil || previous.LastGrant != remote.Grant) {
		killSessions = true
	}
	if previous != nil && previous.LastEpoch != "" && remote.AuthEpoch != "" && previous.LastEpoch != remote.AuthEpoch {
		killSessions = true
	}
	state.LastStatus, state.LastGrant, state.LastEpoch = remote.Status, remote.Grant, remote.AuthEpoch
	switch remote.Status {
	case "disabled", "deleted", "missing":
		if role == model.UserRoleAdmin {
			slog.Warn("hajimi account inactive but canvas admin is never auto-disabled", "user_id", userID, "hajimi_status", remote.Status)
		} else if status == model.UserStatusActive {
			changed, err := s.repo.DisableUserByHajimiBridge(state, now)
			if err != nil {
				return err
			}
			if changed {
				// 停用事务已清除会话。
				slog.Info("canvas user disabled by hajimi bridge", "user_id", userID, "hajimi_status", remote.Status)
				killSessions = false
			}
		}
	case "active":
		if state.DisabledByBridge {
			if status == model.UserStatusDisabled {
				changed, err := s.repo.EnableUserByHajimiBridge(state, now)
				if err != nil {
					return err
				}
				if changed {
					slog.Info("canvas user re-enabled by hajimi bridge", "user_id", userID)
				}
			} else {
				// 管理员已手动恢复：互通不再认领这个停用。
				state.DisabledByBridge = false
			}
		}
	}
	if killSessions {
		if err := s.repo.DeleteUserAuthSessions(userID); err != nil {
			return err
		}
		slog.Info("canvas sessions revoked by hajimi bridge", "user_id", userID, "grant", remote.Grant)
	}
	return s.repo.SaveHajimiBridgeUserState(state)
}

// RunAutoTopupPass 为开启自动转入的关联用户补足积分；每次至多一笔 pending，按 24 小时额度控制。
func (s *Service) RunAutoTopupPass(ctx context.Context) error {
	// 积分功能关闭时与手动划转一致：不发起任何自动资金移动。
	if s.host.RequireCreditsFeature() != nil {
		return nil
	}
	trusted, err := s.slotTrusted()
	if err != nil || !trusted {
		return err
	}
	after := ""
	for {
		settings, err := s.repo.HajimiBridgeAutoTopupSettings(after, autoTopupBatchSize)
		if err != nil {
			return err
		}
		for index := range settings {
			if ctx.Err() != nil || !s.client.Available() {
				return ctx.Err()
			}
			if err := s.maybeAutoTopup(ctx, &settings[index]); err != nil {
				slog.Error("hajimi auto top-up check failed", "user_id", settings[index].UserID, "error", err)
			}
		}
		if len(settings) < autoTopupBatchSize {
			return nil
		}
		after = settings[len(settings)-1].UserID
	}
}

func (s *Service) maybeAutoTopup(ctx context.Context, settings *model.HajimiBridgeUserSettings) error {
	if !settings.AutoTopupEnabled || settings.AmountMicrocredits <= 0 {
		return nil
	}
	user, err := s.repo.User(settings.UserID)
	if err != nil || user.Status != model.UserStatusActive {
		if notFound(err) {
			return nil
		}
		return err
	}
	hajimiUserID, linked, err := s.linkedHajimiUser(user.ID)
	if err != nil || !linked {
		return err
	}
	state, err := s.repo.HajimiBridgeUserState(user.ID)
	if err != nil {
		return err
	}
	if state == nil || state.HajimiUserID != hajimiUserID || state.LastGrant != "active" || state.LastStatus != "active" {
		return nil
	}
	account, err := s.repo.CreditAccount(user.ID)
	if err != nil {
		return err
	}
	if account.AvailableMicrocredits >= settings.ThresholdMicrocredits {
		return nil
	}
	pending, err := s.repo.HajimiBridgeHasPendingIn(user.ID)
	if err != nil || pending {
		return err
	}
	now := s.now()
	lastAuto, err := s.repo.HajimiBridgeLatestIn(user.ID, model.HajimiTransferSourceAuto)
	if err != nil {
		return err
	}
	if lastAuto != nil && (lastAuto.Status == model.HajimiTransferRejected || lastAuto.Status == model.HajimiTransferFailed) && now.Sub(lastAuto.UpdatedAt) < s.autoTopupCooldown {
		return nil
	}
	since := now.Add(-24 * time.Hour)
	used, err := s.repo.HajimiBridgeAutoInSince(user.ID, since)
	if err != nil {
		return err
	}
	if used+settings.AmountMicrocredits > settings.DailyLimitMicrocredits {
		return nil
	}
	// 幂等键按“用户 + 分钟”确定：同一分钟内多个实例算出同一个键，唯一索引只放行一笔；
	// 跨分钟的竞争由下面在账户行锁内复查阈值、pending 和日额度兜住。用户幂等键至少 16 位，不会与之重名。
	clientKey := "auto-" + strconv.FormatInt(now.Unix()/60, 10)
	id, err := newTransferID()
	if err != nil {
		return err
	}
	row := &model.HajimiBridgeTransfer{
		ID: id, UserID: user.ID, HajimiUserID: hajimiUserID, Direction: model.HajimiTransferDirectionIn, AmountMicrocredits: settings.AmountMicrocredits,
		ClientKey: &clientKey, Source: model.HajimiTransferSourceAuto, Status: model.HajimiTransferPending, NextAttemptAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateHajimiBridgeAutoInTransfer(row, settings.ThresholdMicrocredits, settings.DailyLimitMicrocredits, since); err != nil {
		if errors.Is(err, repository.ErrHajimiBridgeDuplicate) || errors.Is(err, repository.ErrHajimiBridgeAutoSkipped) {
			return nil
		}
		return err
	}
	s.process(ctx, row, s.workerTimeout)
	return nil
}

// RunReconcilePass 每小时把本地 Σ credited 转入 / Σ completed 转回与 hajimi 汇总对账，
// 每 24 小时额外从 seq 0 全量复核一次（不移动游标）。force 用于测试和手动触发。
func (s *Service) RunReconcilePass(ctx context.Context, force bool) (*ReconcileResult, error) {
	now := s.now()
	last, err := s.loadReconcileResult()
	if err != nil {
		return nil, err
	}
	if !force && last != nil && now.Sub(last.At) < s.reconcileInterval {
		return nil, nil
	}
	full := force
	if !full {
		raw, found, err := s.repo.HajimiBridgeStateValue(stateKeyReconcileFull)
		if err != nil {
			return nil, err
		}
		lastFull, parseErr := time.Parse(time.RFC3339Nano, raw)
		full = !found || parseErr != nil || now.Sub(lastFull) >= s.fullReconcileGap
	}
	truncatedAt := int64(-1)
	if full {
		// 全量复核读到末页才算完整；触及安全上限时如实记录为非完整并给出告警。
		lastSeq, complete, err := s.scanCommitted(ctx, 0, s.fullScanMaxPages, nil)
		if err != nil {
			return nil, err
		}
		if !complete {
			full, truncatedAt = false, lastSeq
		}
		if err := s.repo.SaveHajimiBridgeStateValue(stateKeyReconcileFull, now.UTC().Format(time.RFC3339Nano), now); err != nil {
			return nil, err
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, s.workerTimeout)
	summary, err := s.client.ReconcileSummary(callCtx)
	cancel()
	if err != nil {
		return nil, err
	}
	localIn, localOut, err := s.repo.HajimiBridgeTotals()
	if err != nil {
		return nil, err
	}
	result := &ReconcileResult{At: now, Full: full, LocalInMicro: localIn, LocalOutMicro: localOut, RemoteToCanvasMicro: summary.ToCanvasCommittedMicro, RemoteToHajimiMicro: summary.ToHajimiCommittedMicro, Mismatches: []string{}}
	if truncatedAt >= 0 {
		result.Mismatches = append(result.Mismatches, fmt.Sprintf("full scan incomplete: stopped at seq %d after %d pages (hajimi max_seq %d)", truncatedAt, s.fullScanMaxPages, summary.MaxSeq))
	}
	if localIn != summary.ToCanvasCommittedMicro {
		result.Mismatches = append(result.Mismatches, fmt.Sprintf("to_canvas: local credited %s != hajimi committed %s", strconv.FormatInt(localIn, 10), strconv.FormatInt(summary.ToCanvasCommittedMicro, 10)))
	}
	if localOut != summary.ToHajimiCommittedMicro {
		result.Mismatches = append(result.Mismatches, fmt.Sprintf("to_hajimi: local completed %s != hajimi committed %s", strconv.FormatInt(localOut, 10), strconv.FormatInt(summary.ToHajimiCommittedMicro, 10)))
	}
	attention, err := s.repo.HajimiBridgeStatusCount(model.HajimiTransferAttention)
	if err != nil {
		return nil, err
	}
	if attention > 0 {
		result.Mismatches = append(result.Mismatches, fmt.Sprintf("attention transfers: %d", attention))
	}
	if len(result.Mismatches) > 0 {
		if pending, err := s.repo.HajimiBridgePendingCount(""); err == nil && pending > 0 {
			result.Mismatches = append(result.Mismatches, fmt.Sprintf("pending transfers (may explain in-flight differences): %d", pending))
		}
	}
	result.OK = len(result.Mismatches) == 0
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveHajimiBridgeStateValue(stateKeyReconcile, string(encoded), now); err != nil {
		return nil, err
	}
	if !result.OK {
		slog.Warn("hajimi reconcile mismatch", "mismatches", result.Mismatches)
	}
	return result, nil
}
