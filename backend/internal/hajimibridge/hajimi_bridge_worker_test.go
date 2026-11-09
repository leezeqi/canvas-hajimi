package hajimibridge

import (
	"context"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func (e *testEnv) userStatus(t *testing.T, userID string) model.UserStatus {
	t.Helper()
	user, err := e.repo.User(userID)
	if err != nil {
		t.Fatal(err)
	}
	return user.Status
}

func (e *testEnv) addSession(t *testing.T, userID string, id string) {
	t.Helper()
	if err := e.db.Create(&model.AuthSession{ID: id, UserID: userID, TokenHash: "hash", ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) syncStatus(t *testing.T) {
	t.Helper()
	if err := e.svc.RunStatusSyncPass(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStatusSyncDisablesReenablesAndRevokesSessions(t *testing.T) {
	env := newTestEnv(t)
	// 第一次观察只记录 epoch，不清会话。
	env.syncStatus(t)
	state, _ := env.repo.HajimiBridgeUserState(env.user.ID)
	if state == nil || state.LastEpoch != "aaaaaaaaaaaaaaaa" || state.LastGrant != "active" || state.LastStatus != "active" || state.HajimiUserID != testHajimiUserID || env.sessionCount(t, env.user.ID) != 1 {
		t.Fatalf("first observation state=%#v sessions=%d", state, env.sessionCount(t, env.user.ID))
	}
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.status = "disabled" })
	env.syncStatus(t)
	state, _ = env.repo.HajimiBridgeUserState(env.user.ID)
	if env.userStatus(t, env.user.ID) != model.UserStatusDisabled || !state.DisabledByBridge || env.sessionCount(t, env.user.ID) != 0 {
		t.Fatalf("disable: status=%s state=%#v", env.userStatus(t, env.user.ID), state)
	}
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.status = "active" })
	env.syncStatus(t)
	state, _ = env.repo.HajimiBridgeUserState(env.user.ID)
	if env.userStatus(t, env.user.ID) != model.UserStatusActive || state.DisabledByBridge {
		t.Fatalf("re-enable: status=%s state=%#v", env.userStatus(t, env.user.ID), state)
	}
	// auth_epoch 变化（改密码/邮箱）只清会话，不停用。
	env.addSession(t, env.user.ID, "session-after-enable")
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.epoch = "bbbbbbbbbbbbbbbb" })
	env.syncStatus(t)
	if env.sessionCount(t, env.user.ID) != 0 || env.userStatus(t, env.user.ID) != model.UserStatusActive {
		t.Fatal("epoch change must only revoke sessions")
	}
	env.addSession(t, env.user.ID, "session-after-epoch")
	env.syncStatus(t)
	if env.sessionCount(t, env.user.ID) != 1 {
		t.Fatal("unchanged epoch must keep sessions")
	}
	// 撤销授权：清一次会话；之后保持撤销状态不会反复踢人。
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.grant = "revoked" })
	env.syncStatus(t)
	if env.sessionCount(t, env.user.ID) != 0 || env.userStatus(t, env.user.ID) != model.UserStatusActive {
		t.Fatal("grant revoke must only revoke sessions")
	}
	env.addSession(t, env.user.ID, "session-after-revoke")
	env.syncStatus(t)
	if env.sessionCount(t, env.user.ID) != 1 {
		t.Fatal("repeated revoked observation must not revoke again")
	}
}

func TestStatusSyncExemptsAdminsAndHandlesMissingUsers(t *testing.T) {
	env := newTestEnv(t)
	linkedAdmin := env.createUser(t, "canvas-admin-linked", "root", model.UserRoleAdmin, "hajimi:77")
	env.fake.addUser(77, fakeUser{status: "disabled", grant: "active", epoch: "cccccccccccccccc"})
	ghost := env.createUser(t, "canvas-user-ghost", "ghost", model.UserRoleUser, "hajimi:88")
	env.syncStatus(t)
	if env.userStatus(t, linkedAdmin.ID) != model.UserStatusActive || env.sessionCount(t, linkedAdmin.ID) != 1 {
		t.Fatal("canvas admins must never be auto-disabled")
	}
	if env.userStatus(t, ghost.ID) != model.UserStatusDisabled || env.sessionCount(t, ghost.ID) != 0 {
		t.Fatal("missing hajimi user must disable the canvas account")
	}
	// 管理员手动停用的账号不被互通认领，hajimi 恢复后也不自动启用。
	manual := env.createUser(t, "canvas-user-manual", "manual", model.UserRoleUser, "hajimi:99")
	env.fake.addUser(99, fakeUser{status: "active", grant: "active", epoch: "dddddddddddddddd"})
	if err := env.db.Model(&model.User{}).Where("id = ?", manual.ID).Update("status", model.UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	env.syncStatus(t)
	if env.userStatus(t, manual.ID) != model.UserStatusDisabled {
		t.Fatal("bridge must not re-enable a user it did not disable")
	}
	// IdP 槽位被改指后，状态同步不做任何事。
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.status = "disabled" })
	env.setIdPAuthorizationURL(t, "https://other.example/oauth")
	env.syncStatus(t)
	if env.userStatus(t, env.user.ID) != model.UserStatusActive {
		t.Fatal("status sync must stop when the IdP host guard fails")
	}
}

func TestAutoTopupConditions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.svc.UpdateAutoTopup(ctx, env.user, AutoTopupRequest{Enabled: true, ThresholdMicro: 1_000_000, AmountMicro: 500_000, DailyLimitMicro: 20_000_000}); err == nil {
		t.Fatal("amount below hajimi min must be rejected")
	}
	if _, err := env.svc.UpdateAutoTopup(ctx, env.user, AutoTopupRequest{Enabled: true, ThresholdMicro: 1_000_000, AmountMicro: 5_000_000, DailyLimitMicro: 4_000_000}); err == nil {
		t.Fatal("daily below amount must be rejected")
	}
	if _, err := env.svc.UpdateAutoTopup(ctx, env.user, AutoTopupRequest{Enabled: true, ThresholdMicro: -1, AmountMicro: 5_000_000, DailyLimitMicro: 20_000_000}); err == nil {
		t.Fatal("negative threshold must be rejected")
	}
	setting, err := env.svc.UpdateAutoTopup(ctx, env.user, AutoTopupRequest{Enabled: true, ThresholdMicro: 1_000_000, AmountMicro: 5_000_000, DailyLimitMicro: 12_000_000})
	if err != nil || !setting.Enabled {
		t.Fatalf("setting = %#v err=%v", setting, err)
	}
	// 尚未经过状态同步（不知道授权是否有效）时不自动转入。
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	if env.available(t, env.user.ID) != 0 {
		t.Fatal("auto top-up must wait for a status observation")
	}
	env.syncStatus(t)
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	if env.available(t, env.user.ID) != 5_000_000 {
		t.Fatalf("first auto top-up: available = %d", env.available(t, env.user.ID))
	}
	// 高于阈值不转。
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	if env.available(t, env.user.ID) != 5_000_000 {
		t.Fatal("above threshold must not top up")
	}
	// 同一分钟内（同一幂等键桶）不会发起第二笔。
	env.setAvailable(t, env.user.ID, 0)
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	if env.available(t, env.user.ID) != 0 {
		t.Fatalf("second auto top-up in the same minute: available = %d", env.available(t, env.user.ID))
	}
	env.clock.Advance(time.Minute)
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	if env.available(t, env.user.ID) != 5_000_000 {
		t.Fatalf("second auto top-up: available = %d", env.available(t, env.user.ID))
	}
	// 24 小时内已自动转入 10，再转 5 会超过每日 12 的上限。
	env.setAvailable(t, env.user.ID, 0)
	env.clock.Advance(time.Minute)
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	if env.available(t, env.user.ID) != 0 {
		t.Fatal("daily limit exceeded")
	}
	env.clock.Advance(25 * time.Hour)
	// 有 pending 转入时不再发起新的自动转入。
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = true })
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	var autoRows []model.HajimiBridgeTransfer
	env.db.Where("source = ?", "auto").Order("created_at asc").Find(&autoRows)
	if len(autoRows) != 3 || autoRows[2].Status != model.HajimiTransferPending {
		t.Fatalf("auto rows = %d", len(autoRows))
	}
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = false })
	env.clock.Advance(20 * time.Second)
	env.svc.RunRetryPass(ctx)
	if env.available(t, env.user.ID) != 5_000_000 {
		t.Fatalf("pending auto transfer should credit after retry: %d", env.available(t, env.user.ID))
	}
	// hajimi 余额不足被拒后进入冷却，不会每分钟刷出新的拒绝记录。
	env.setAvailable(t, env.user.ID, 0)
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.balance = 1_000_000 })
	env.clock.Advance(25 * time.Hour)
	for range 3 {
		if err := env.svc.RunAutoTopupPass(ctx); err != nil {
			t.Fatal(err)
		}
		env.clock.Advance(time.Minute)
	}
	env.db.Where("source = ?", "auto").Order("created_at asc").Find(&autoRows)
	if len(autoRows) != 4 || autoRows[3].Status != model.HajimiTransferRejected || autoRows[3].Reason != "insufficient_balance" {
		t.Fatalf("auto rows after rejection = %#v", autoRows)
	}
	// 授权撤销后不再自动转入。
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.balance = 100_000_000; u.grant = "revoked" })
	env.syncStatus(t)
	env.clock.Advance(time.Hour)
	if err := env.svc.RunAutoTopupPass(ctx); err != nil {
		t.Fatal(err)
	}
	var count int64
	env.db.Model(&model.HajimiBridgeTransfer{}).Where("source = ?", "auto").Count(&count)
	if count != 4 {
		t.Fatalf("auto top-up ran with revoked grant: %d rows", count)
	}
	// 关闭开关不需要 hajimi 可达。
	env.fake.server.Close()
	if _, err := env.svc.UpdateAutoTopup(ctx, env.user, AutoTopupRequest{Enabled: false, ThresholdMicro: 1_000_000, AmountMicro: 5_000_000, DailyLimitMicro: 12_000_000}); err != nil {
		t.Fatalf("disabling auto top-up while hajimi is down: %v", err)
	}
}

func TestCursorReconcileMarksAttentionWithoutMovingMoney(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.create(t, "in", 10_000_000, idemKey("seed"))
	now := env.clock.Now()
	// 本地已退款的转回，hajimi 却已提交 → attention，不再扣款。
	refundedID := "yct_000000000000000000000000000000a1"
	if err := env.db.Create(&model.HajimiBridgeTransfer{ID: refundedID, UserID: env.user.ID, HajimiUserID: testHajimiUserID, Direction: "out", AmountMicrocredits: 2_000_000, Source: "manual", Status: model.HajimiTransferRefunded, Reason: localReasonNotFoundAfterWindow, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	env.fake.injectCommitted(RemoteTransfer{TransferID: refundedID, Direction: remoteDirectionToHajimi, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 2_000_000})
	// 本地 failed 的转入，hajimi 已提交 → 补入账。
	failedID := "yct_000000000000000000000000000000a2"
	if err := env.db.Create(&model.HajimiBridgeTransfer{ID: failedID, UserID: env.user.ID, HajimiUserID: testHajimiUserID, Direction: "in", AmountMicrocredits: 1_500_000, Source: "manual", Status: model.HajimiTransferFailed, Reason: localReasonNotFoundAfterWindow, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	env.fake.injectCommitted(RemoteTransfer{TransferID: failedID, Direction: remoteDirectionToCanvas, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 1_500_000})
	// 本地没有记录 → 补一条 attention。
	missingID := "yct_000000000000000000000000000000a3"
	env.fake.injectCommitted(RemoteTransfer{TransferID: missingID, Direction: remoteDirectionToCanvas, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 4_000_000})
	before := env.available(t, env.user.ID)
	if err := env.svc.RunCursorPass(ctx); err != nil {
		t.Fatal(err)
	}
	if row := env.transfer(t, refundedID); row.Status != model.HajimiTransferAttention || row.Reason != "remote_committed_local_refunded" {
		t.Fatalf("refunded row = %#v", row)
	}
	if row := env.transfer(t, failedID); row.Status != model.HajimiTransferCredited || row.HajimiSeq != 3 {
		t.Fatalf("failed row = %#v", row)
	}
	missing := env.transfer(t, missingID)
	if missing.Status != model.HajimiTransferAttention || missing.Reason != localReasonMissingLocalRecord || missing.Source != model.HajimiTransferSourceReconcile || missing.UserID != env.user.ID {
		t.Fatalf("missing row = %#v", missing)
	}
	if got := env.available(t, env.user.ID); got != before+1_500_000 {
		t.Fatalf("only the failed→credited transfer may move money: before=%d after=%d", before, got)
	}
	if env.ledgerCount(t, repository.HajimiInReferenceKey(missingID)) != 0 || env.ledgerCount(t, repository.HajimiOutReferenceKey(refundedID)) != 0 {
		t.Fatal("attention must not move money")
	}
	raw, _, _ := env.repo.HajimiBridgeStateValue(stateKeyCursor)
	if raw != `{"after_seq":4}` {
		t.Fatalf("cursor = %s", raw)
	}
	env.fake.set(func(f *fakeHajimi) { f.requests = nil })
	if err := env.svc.RunCursorPass(ctx); err != nil {
		t.Fatal(err)
	}
	if len(env.fake.requests) != 1 || !strings.Contains(env.fake.requests[0], "after_seq=4") {
		t.Fatalf("cursor not persisted: %v", env.fake.requests)
	}
	// 管理员重试：已退款又被 hajimi 提交的转回必须人工调账；缺本地记录的转入按 hajimi 结果补入账。
	if _, err := env.svc.AdminRetryTransfer(ctx, env.admin, refundedID); appReason(err) != ReasonNeedsManualReview {
		t.Fatalf("refunded+committed retry err = %v", err)
	}
	if _, err := env.svc.AdminRetryTransfer(ctx, env.admin, missingID); err != nil {
		t.Fatal(err)
	}
	if env.transfer(t, missingID).Status != model.HajimiTransferCredited || env.available(t, env.user.ID) != before+5_500_000 {
		t.Fatalf("missing record retry: available=%d", env.available(t, env.user.ID))
	}
	overview, err := env.svc.AdminOverview(ctx, env.admin)
	if err != nil {
		t.Fatal(err)
	}
	if !overview.Config.Enabled || !overview.Config.IdPHostMatches || overview.Config.KeyID != env.fake.keyID || !overview.Reachable || overview.RemoteSummary == nil ||
		len(overview.Attention) != 1 || overview.Attention[0].ID != refundedID || overview.Attention[0].Username != "alice" || overview.LinkedUsers != 1 {
		t.Fatalf("overview = %#v", overview)
	}
	if _, err := env.svc.AdminOverview(ctx, env.user); err == nil {
		t.Fatal("overview requires admin")
	}
}

func TestReconcileSummaryAndFullScan(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.create(t, "in", 10_000_000, idemKey("r1"))
	env.create(t, "out", 3_000_000, idemKey("r2"))
	result, err := env.svc.RunReconcilePass(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.OK || !result.Full || result.LocalInMicro != 10_000_000 || result.LocalOutMicro != 3_000_000 || result.RemoteToCanvasMicro != 10_000_000 || result.RemoteToHajimiMicro != 3_000_000 {
		t.Fatalf("result = %#v", result)
	}
	// 一小时内不重复。
	if again, err := env.svc.RunReconcilePass(ctx, false); err != nil || again != nil {
		t.Fatalf("hourly gate: %#v %v", again, err)
	}
	// hajimi 多出一笔本地没有的提交：下一次（小时级）对账发现总额不一致。
	env.fake.injectCommitted(RemoteTransfer{TransferID: "yct_000000000000000000000000000000b1", Direction: remoteDirectionToCanvas, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 1_000_000})
	env.clock.Advance(61 * time.Minute)
	result, err = env.svc.RunReconcilePass(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Full || len(result.Mismatches) == 0 {
		t.Fatalf("mismatch not reported: %#v", result)
	}
	// 每 24 小时的全量复核会把缺失记录登记为 attention（不移动游标）。
	env.clock.Advance(24 * time.Hour)
	result, err = env.svc.RunReconcilePass(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Full || env.transfer(t, "yct_000000000000000000000000000000b1").Status != model.HajimiTransferAttention {
		t.Fatalf("full run = %#v", result)
	}
	stored, err := env.svc.loadReconcileResult()
	if err != nil || stored == nil || stored.OK {
		t.Fatalf("stored = %#v err=%v", stored, err)
	}
}
