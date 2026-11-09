package hajimibridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/gorm"
)

func (e *testEnv) lookupRequests(id string) int {
	e.fake.mu.Lock()
	defer e.fake.mu.Unlock()
	count := 0
	for _, request := range e.fake.requests {
		if request == "GET "+bridgeAPIPrefix+"/transfers/"+id {
			count++
		}
	}
	return count
}

// 修复 1：只有精确的 404 transfer_not_found 才能把窗口后的划转定论为“未发生”。
func TestLookupOnlyTrustsExactTransferNotFound(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.create(t, "in", 10_000_000, idemKey("seed"))
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = true })
	in := env.create(t, "in", 2_000_000, idemKey("in"))
	out := env.create(t, "out", 3_000_000, idemKey("out"))
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = false })
	env.clock.Advance(14 * time.Minute)
	for _, body := range []string{`{"error":"not_found"}`, `<html>404 page not found</html>`, ` `, `{"error":" transfer_not_found"}`, `{"error":"TRANSFER_NOT_FOUND"}`} {
		env.fake.set(func(f *fakeHajimi) { f.lookupOverride = body })
		env.clock.Advance(2 * time.Minute)
		env.svc.RunRetryPass(ctx)
		if env.transfer(t, in.ID).Status != model.HajimiTransferPending || env.transfer(t, out.ID).Status != model.HajimiTransferPending {
			t.Fatalf("404 body %q must not resolve the transfer", body)
		}
		if env.lookupRequests(in.ID) == 0 {
			t.Fatalf("expected a lookup for body %q", body)
		}
	}
	if env.available(t, env.user.ID) != 7_000_000 {
		t.Fatalf("withdraw refunded on an ambiguous 404: available=%d", env.available(t, env.user.ID))
	}
	env.fake.set(func(f *fakeHajimi) { f.lookupOverride = "" })
	env.clock.Advance(2 * time.Minute)
	env.svc.RunRetryPass(ctx)
	if row := env.transfer(t, in.ID); row.Status != model.HajimiTransferFailed || row.Reason != localReasonNotFoundAfterWindow {
		t.Fatalf("in row = %#v", row)
	}
	if row := env.transfer(t, out.ID); row.Status != model.HajimiTransferRefunded || env.available(t, env.user.ID) != 10_000_000 {
		t.Fatalf("out row = %#v available=%d", row, env.available(t, env.user.ID))
	}

	// 管理员重试同样只认 transfer_not_found。
	now := env.clock.Now()
	attentionID := "yct_000000000000000000000000000000c1"
	if err := env.db.Create(&model.HajimiBridgeTransfer{ID: attentionID, UserID: env.user.ID, HajimiUserID: testHajimiUserID, Direction: "in", AmountMicrocredits: 1_000_000, Source: "manual", Status: model.HajimiTransferAttention, Reason: localReasonRemoteMismatch, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	env.fake.set(func(f *fakeHajimi) { f.lookupOverride = `{"error":"not_found"}` })
	if _, err := env.svc.AdminRetryTransfer(ctx, env.admin, attentionID); appReason(err) != ReasonUnavailable {
		t.Fatalf("admin retry on ambiguous 404 err = %v", err)
	}
	if env.transfer(t, attentionID).Status != model.HajimiTransferAttention {
		t.Fatal("ambiguous 404 must not resolve an attention transfer")
	}
	env.fake.set(func(f *fakeHajimi) { f.lookupOverride = "" })
	if result, err := env.svc.AdminRetryTransfer(ctx, env.admin, attentionID); err != nil || result.Status != model.HajimiTransferFailed {
		t.Fatalf("admin retry on transfer_not_found = %#v err=%v", result, err)
	}
}

// 修复 2：批量读取后另一实例刚发送过，本实例不能在其签名有效期内查询定论；窗口之后数据库也拒绝再发送。
func TestLookupClaimHonorsFreshAttemptFromAnotherInstance(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = true })
	transfer := env.create(t, "in", 2_000_000, idemKey("race"))
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = false })
	env.clock.Advance(11 * time.Minute)
	stale := env.transfer(t, transfer.ID)
	// 另一实例在本实例读出批量之后刚刚发送过一次（仍在签名有效期内）。
	fresh := env.clock.Now().Add(-30 * time.Second)
	if err := env.db.Model(&model.HajimiBridgeTransfer{}).Where("id = ?", transfer.ID).Update("last_attempt_at", fresh).Error; err != nil {
		t.Fatal(err)
	}
	env.svc.process(ctx, stale, time.Second)
	if env.lookupRequests(transfer.ID) != 0 || env.transfer(t, transfer.ID).Status != model.HajimiTransferPending {
		t.Fatal("lookup must wait for the other instance's signature window")
	}
	now := env.clock.Now()
	if claimed, err := env.repo.ClaimHajimiBridgeAttempt(transfer.ID, now.Add(time.Hour), now.Add(time.Hour), now.Add(-10*time.Minute)); err != nil || claimed {
		t.Fatalf("send claim after the window = %v %v", claimed, err)
	}
	env.clock.Advance(3 * time.Minute)
	env.svc.RunRetryPass(ctx)
	if env.transfer(t, transfer.ID).Status != model.HajimiTransferFailed || env.lookupRequests(transfer.ID) != 1 {
		t.Fatalf("row = %#v lookups=%d", env.transfer(t, transfer.ID), env.lookupRequests(transfer.ID))
	}
}

// 修复 3：每日全量复核必须读到末页；触及安全上限时记为非完整并告警。
func TestFullReconcilePagesToEndOrReportsTruncation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.svc.transferPageLimit = 2
	for index := range 45 {
		env.fake.injectCommitted(RemoteTransfer{TransferID: fmt.Sprintf("yct_%032x", 0xd000+index), Direction: remoteDirectionToCanvas, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 1_000_000})
	}
	result, err := env.svc.RunReconcilePass(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	attention, _ := env.repo.HajimiBridgeStatusCount(model.HajimiTransferAttention)
	if !result.Full || attention != 45 {
		t.Fatalf("full scan beyond 20 pages: full=%v attention=%d", result.Full, attention)
	}
	for _, mismatch := range result.Mismatches {
		if strings.Contains(mismatch, "incomplete") {
			t.Fatalf("complete scan reported truncation: %v", result.Mismatches)
		}
	}
	env.svc.fullScanMaxPages = 3
	result, err = env.svc.RunReconcilePass(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	truncated := false
	for _, mismatch := range result.Mismatches {
		truncated = truncated || strings.Contains(mismatch, "full scan incomplete: stopped at seq 6 after 3 pages")
	}
	if result.Full || result.OK || !truncated {
		t.Fatalf("truncated scan = %#v", result)
	}
}

// 修复 4：标记 attention 写库失败时游标不得越过该笔。
func TestCursorDoesNotAdvancePastUnflaggedInconsistency(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.create(t, "in", 10_000_000, idemKey("seed"))
	now := env.clock.Now()
	refundedID := "yct_000000000000000000000000000000e1"
	if err := env.db.Create(&model.HajimiBridgeTransfer{ID: refundedID, UserID: env.user.ID, HajimiUserID: testHajimiUserID, Direction: "out", AmountMicrocredits: 2_000_000, Source: "manual", Status: model.HajimiTransferRefunded, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	env.fake.injectCommitted(RemoteTransfer{TransferID: refundedID, Direction: remoteDirectionToHajimi, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 2_000_000})
	var failAttention atomic.Bool
	failAttention.Store(true)
	if err := env.db.Callback().Update().Before("gorm:update").Register("test:fail_attention", func(tx *gorm.DB) {
		if failAttention.Load() && tx.Statement.Table == "hajimi_bridge_transfers" {
			if updates, ok := tx.Statement.Dest.(map[string]any); ok && updates["status"] == model.HajimiTransferAttention {
				_ = tx.AddError(errors.New("injected attention write failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.RunCursorPass(ctx); err == nil {
		t.Fatal("attention write failure must surface")
	}
	if raw, _, _ := env.repo.HajimiBridgeStateValue(stateKeyCursor); raw != `{"after_seq":1}` {
		t.Fatalf("cursor advanced past the unflagged transfer: %s", raw)
	}
	failAttention.Store(false)
	if err := env.svc.RunCursorPass(ctx); err != nil {
		t.Fatal(err)
	}
	if env.transfer(t, refundedID).Status != model.HajimiTransferAttention {
		t.Fatal("retry must flag the transfer")
	}
	if raw, _, _ := env.repo.HajimiBridgeStateValue(stateKeyCursor); raw != `{"after_seq":2}` {
		t.Fatalf("cursor = %s", raw)
	}
}

func (e *testEnv) enableAutoTopup(t *testing.T) {
	t.Helper()
	if _, err := e.svc.UpdateAutoTopup(context.Background(), e.user, AutoTopupRequest{Enabled: true, ThresholdMicro: 1_000_000, AmountMicro: 5_000_000, DailyLimitMicro: 20_000_000}); err != nil {
		t.Fatal(err)
	}
	e.syncStatus(t)
}

// 修复 5：并发的自动转入只会产生一笔；幂等键按用户 + 分钟确定，锁内复查 pending/日额度。
func TestAutoTopupConcurrentPassesCreateOneTransfer(t *testing.T) {
	env := newTestEnv(t)
	env.enableAutoTopup(t)
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = true })
	settings, err := env.repo.HajimiBridgeUserSettings(env.user.ID)
	if err != nil || settings == nil {
		t.Fatalf("settings = %#v err=%v", settings, err)
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = env.svc.maybeAutoTopup(context.Background(), settings)
		}()
	}
	wg.Wait()
	var rows []model.HajimiBridgeTransfer
	env.db.Where("source = ?", model.HajimiTransferSourceAuto).Find(&rows)
	wantKey := fmt.Sprintf("auto-%d", env.clock.Now().Unix()/60)
	if len(rows) != 1 || rows[0].ClientKey == nil || *rows[0].ClientKey != wantKey {
		t.Fatalf("auto rows = %d key=%v", len(rows), rows)
	}
	// 跨分钟（不同幂等键）时，锁内复查发现已有 pending 转入，拒绝插入。
	now := env.clock.Now().Add(time.Minute)
	key := fmt.Sprintf("auto-%d", now.Unix()/60)
	second := &model.HajimiBridgeTransfer{ID: "yct_000000000000000000000000000000f1", UserID: env.user.ID, HajimiUserID: testHajimiUserID, Direction: "in", AmountMicrocredits: 5_000_000, ClientKey: &key, Source: "auto", Status: "pending", CreatedAt: now, UpdatedAt: now}
	if err := env.repo.CreateHajimiBridgeAutoInTransfer(second, 1_000_000, 20_000_000, now.Add(-24*time.Hour)); !errors.Is(err, repository.ErrHajimiBridgeAutoSkipped) {
		t.Fatalf("second auto insert err = %v", err)
	}
	// 日额度在锁内复查：pending 结束后，已用 5 + 本次 16 > 20 也会被拒。
	env.db.Model(&model.HajimiBridgeTransfer{}).Where("source = ?", "auto").Update("status", model.HajimiTransferCredited)
	second.AmountMicrocredits = 16_000_000
	if err := env.repo.CreateHajimiBridgeAutoInTransfer(second, 1_000_000, 20_000_000, now.Add(-24*time.Hour)); !errors.Is(err, repository.ErrHajimiBridgeAutoSkipped) {
		t.Fatalf("daily limit recheck err = %v", err)
	}
}

// 修复 6：积分功能关闭时自动转入不动钱。
func TestAutoTopupRespectsCreditsFeature(t *testing.T) {
	env := newTestEnv(t)
	env.enableAutoTopup(t)
	env.host.creditsOff = true
	if err := env.svc.RunAutoTopupPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	env.db.Model(&model.HajimiBridgeTransfer{}).Count(&count)
	if count != 0 || env.fake.transferPosts() != 0 {
		t.Fatalf("auto top-up ran with credits disabled: rows=%d posts=%d", count, env.fake.transferPosts())
	}
	env.host.creditsOff = false
	if err := env.svc.RunAutoTopupPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if env.available(t, env.user.ID) != 5_000_000 {
		t.Fatalf("auto top-up after re-enabling credits: available=%d", env.available(t, env.user.ID))
	}
}

// 修复 7：OAuth 槽位的 subjectField 不是 "sub" 时，不承认任何 hajimi 关联。
func TestLinkedRequiresSubjectFieldSub(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	for _, field := range []string{"id", "username", ""} {
		env.setOAuthSlot(t, "https://hajimi.chat/yingce/authorize", field)
		if _, linked, err := env.svc.linkedHajimiUser(env.user.ID); err != nil || linked {
			t.Fatalf("subjectField %q must not link: %v %v", field, linked, err)
		}
	}
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.status = "disabled" })
	env.syncStatus(t)
	if env.userStatus(t, env.user.ID) != model.UserStatusActive {
		t.Fatal("status sync must not act on an untrusted slot")
	}
	overview, err := env.svc.AdminOverview(ctx, env.admin)
	if err != nil || !overview.Config.IdPHostMatches || overview.Config.SubjectFieldOK {
		t.Fatalf("overview config = %#v err=%v", overview.Config, err)
	}
	env.setOAuthSlot(t, "https://hajimi.chat/yingce/authorize", "sub")
	if _, linked, _ := env.svc.linkedHajimiUser(env.user.ID); !linked {
		t.Fatal("subjectField sub must link")
	}
}
