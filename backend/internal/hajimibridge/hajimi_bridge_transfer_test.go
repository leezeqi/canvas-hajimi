package hajimibridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func appReason(err error) kernel.ErrorReason {
	var appErr *kernel.AppError
	if errors.As(err, &appErr) {
		return appErr.Reason
	}
	return ""
}

func (e *testEnv) create(t *testing.T, direction string, amount int64, key string) *Transfer {
	t.Helper()
	transfer, err := e.svc.CreateTransfer(context.Background(), e.user, CreateTransferRequest{Direction: direction, AmountMicro: amount, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("create %s %d: %v", direction, amount, err)
	}
	return transfer
}

func TestTransferInCommitsCreditsOnceAndReplaysIdempotencyKey(t *testing.T) {
	env := newTestEnv(t)
	transfer := env.create(t, "in", 5_000_000, idemKey("in-1"))
	if transfer.Status != model.HajimiTransferCredited || transfer.CompletedAt == nil || transfer.Source != "manual" || len(transfer.ID) != 36 {
		t.Fatalf("transfer = %#v", transfer)
	}
	if got := env.available(t, env.user.ID); got != 5_000_000 {
		t.Fatalf("available = %d", got)
	}
	if env.ledgerCount(t, repository.HajimiInReferenceKey(transfer.ID)) != 1 {
		t.Fatal("credit ledger entry missing")
	}
	if env.fake.user(testHajimiUserID).balance != 95_000_000 || env.fake.user(testHajimiUserID).canvasUserID != env.user.ID {
		t.Fatalf("hajimi user = %#v", env.fake.user(testHajimiUserID))
	}
	var entry model.CreditLedgerEntry
	if err := env.db.First(&entry, "reference_key = ?", repository.HajimiInReferenceKey(transfer.ID)).Error; err != nil || entry.Type != model.CreditLedgerHajimiTransferIn || entry.AmountMicrocredits != 5_000_000 || entry.AvailableAfterMicrocredits != 5_000_000 {
		t.Fatalf("ledger = %#v err=%v", entry, err)
	}
	posts := env.fake.transferPosts()
	replay := env.create(t, "in", 5_000_000, idemKey("in-1"))
	if replay.ID != transfer.ID || replay.Status != model.HajimiTransferCredited || env.fake.transferPosts() != posts {
		t.Fatalf("replay = %#v posts=%d", replay, env.fake.transferPosts())
	}
	if got := env.available(t, env.user.ID); got != 5_000_000 {
		t.Fatalf("replay moved money: available = %d", got)
	}
	_, err := env.svc.CreateTransfer(context.Background(), env.user, CreateTransferRequest{Direction: "in", AmountMicro: 6_000_000, IdempotencyKey: idemKey("in-1")})
	if appReason(err) != kernel.ReasonConflict {
		t.Fatalf("conflicting replay err = %v", err)
	}
	page, err := env.svc.Transfers(env.user, 1, 20)
	if err != nil || page.Total != 1 || page.Items[0].ID != transfer.ID {
		t.Fatalf("transfers page = %#v err=%v", page, err)
	}
	other, err := env.svc.Transfers(env.admin, 1, 20)
	if err != nil || other.Total != 0 {
		t.Fatalf("other user's transfers leaked: %#v", other)
	}
}

func TestTransferValidationAndAccessErrors(t *testing.T) {
	env := newTestEnv(t)
	cases := []CreateTransferRequest{
		{Direction: "sideways", AmountMicro: 1_000_000, IdempotencyKey: idemKey("v1")},
		{Direction: "in", AmountMicro: 0, IdempotencyKey: idemKey("v2")},
		{Direction: "in", AmountMicro: -5, IdempotencyKey: idemKey("v3")},
		{Direction: "in", AmountMicro: 1_000_000, IdempotencyKey: "short"},
		{Direction: "in", AmountMicro: 1_000_000, IdempotencyKey: "bad key with spaces!!"},
		{Direction: "in", AmountMicro: 1_000_000, IdempotencyKey: idemKey("v4"), Source: "auto"},
		{Direction: "out", AmountMicro: 9_999, IdempotencyKey: idemKey("v5")},
	}
	for _, req := range cases {
		if _, err := env.svc.CreateTransfer(context.Background(), env.user, req); appReason(err) != kernel.ReasonInvalidArgument {
			t.Errorf("%#v err = %v", req, err)
		}
	}
	if _, err := env.svc.CreateTransfer(context.Background(), env.admin, CreateTransferRequest{Direction: "in", AmountMicro: 1_000_000, IdempotencyKey: idemKey("v6")}); appReason(err) != ReasonNotLinked {
		t.Fatalf("unlinked err = %v", err)
	}
	env.svc.config.Enabled = false
	if _, err := env.svc.CreateTransfer(context.Background(), env.user, CreateTransferRequest{Direction: "in", AmountMicro: 1_000_000, IdempotencyKey: idemKey("v7")}); appReason(err) != ReasonBridgeDisabled {
		t.Fatalf("disabled err = %v", err)
	}
}

func TestTransferInRejectedIsTerminalWithoutCredit(t *testing.T) {
	env := newTestEnv(t)
	small := env.create(t, "in", 500_000, idemKey("small"))
	if small.Status != model.HajimiTransferRejected || small.Reason != "amount_out_of_range" {
		t.Fatalf("small = %#v", small)
	}
	big := env.create(t, "in", 150_000_000, idemKey("big"))
	if big.Status != model.HajimiTransferRejected || big.Reason != "insufficient_balance" {
		t.Fatalf("big = %#v", big)
	}
	if env.available(t, env.user.ID) != 0 || env.ledgerCount(t, repository.HajimiInReferenceKey(big.ID)) != 0 {
		t.Fatal("rejected transfer must not credit")
	}
	env.clock.Advance(time.Hour)
	env.svc.RunRetryPass(context.Background())
	if env.transfer(t, big.ID).Status != model.HajimiTransferRejected {
		t.Fatal("rejected is terminal")
	}
}

func TestTransferPendingThenResolvedByRetryWithSameID(t *testing.T) {
	env := newTestEnv(t)
	env.fake.set(func(f *fakeHajimi) { f.commitThenFail = 1 })
	transfer := env.create(t, "in", 5_000_000, idemKey("lost-response"))
	if transfer.Status != model.HajimiTransferPending {
		t.Fatalf("lost response should leave pending: %#v", transfer)
	}
	if env.available(t, env.user.ID) != 0 {
		t.Fatal("pending transfer must not credit yet")
	}
	row := env.transfer(t, transfer.ID)
	if row.Attempts != 1 || row.LastAttemptAt == nil || row.LastError == "" || row.NextAttemptAt == nil {
		t.Fatalf("row after failed attempt = %#v", row)
	}
	// 未到退避时间不重发。
	env.svc.RunRetryPass(context.Background())
	if env.fake.transferPosts() != 1 {
		t.Fatalf("retried before backoff: posts=%d", env.fake.transferPosts())
	}
	env.clock.Advance(16 * time.Second)
	env.svc.RunRetryPass(context.Background())
	row = env.transfer(t, transfer.ID)
	if row.Status != model.HajimiTransferCredited || row.HajimiSeq != 1 || row.Attempts != 2 {
		t.Fatalf("row after retry = %#v", row)
	}
	if env.available(t, env.user.ID) != 5_000_000 || env.fake.committedCount() != 1 || env.fake.user(testHajimiUserID).balance != 95_000_000 {
		t.Fatalf("money moved more than once: available=%d committed=%d", env.available(t, env.user.ID), env.fake.committedCount())
	}
}

func TestTransferPendingResolvedByCursorPull(t *testing.T) {
	env := newTestEnv(t)
	env.fake.set(func(f *fakeHajimi) { f.commitThenFail = 1 })
	transfer := env.create(t, "in", 7_000_000, idemKey("cursor"))
	if transfer.Status != model.HajimiTransferPending {
		t.Fatalf("transfer = %#v", transfer)
	}
	if err := env.svc.RunCursorPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if row := env.transfer(t, transfer.ID); row.Status != model.HajimiTransferCredited || row.HajimiSeq != 1 {
		t.Fatalf("row = %#v", row)
	}
	// 稍后 retry 拿到同一结果，也不会再次入账。
	env.clock.Advance(time.Minute)
	env.svc.RunRetryPass(context.Background())
	if env.available(t, env.user.ID) != 7_000_000 || env.ledgerCount(t, repository.HajimiInReferenceKey(transfer.ID)) != 1 {
		t.Fatalf("available = %d", env.available(t, env.user.ID))
	}
	raw, _, _ := env.repo.HajimiBridgeStateValue(stateKeyCursor)
	if raw != `{"after_seq":1}` {
		t.Fatalf("cursor = %s", raw)
	}
}

// 10 分钟窗口内反复发送均失败；窗口后等最后一次发送满 3 分钟才查询，404 → in 失败、out 退款。
func TestTransferNotFoundAfterWindowFailsInAndRefundsOut(t *testing.T) {
	env := newTestEnv(t)
	seedIn := env.create(t, "in", 20_000_000, idemKey("seed"))
	if seedIn.Status != model.HajimiTransferCredited {
		t.Fatalf("seed = %#v", seedIn)
	}
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = true })
	in := env.create(t, "in", 5_000_000, idemKey("never-in"))
	out := env.create(t, "out", 4_000_000, idemKey("never-out"))
	if in.Status != model.HajimiTransferPending || out.Status != model.HajimiTransferPending {
		t.Fatalf("in=%#v out=%#v", in, out)
	}
	if env.available(t, env.user.ID) != 16_000_000 {
		t.Fatalf("withdraw must debit locally first: available=%d", env.available(t, env.user.ID))
	}
	for elapsed := time.Duration(0); elapsed < 9*time.Minute+30*time.Second; elapsed += 30 * time.Second {
		env.clock.Advance(30 * time.Second)
		env.svc.RunRetryPass(context.Background())
	}
	lastSend := *env.transfer(t, in.ID).LastAttemptAt
	if env.transfer(t, in.ID).Attempts < 4 {
		t.Fatalf("expected several attempts inside the window, got %d", env.transfer(t, in.ID).Attempts)
	}
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = false })
	// 跨过 10 分钟窗口，但距最后一次发送不足 3 分钟：不发送也不定论。
	env.clock.Advance(time.Minute)
	posts := env.fake.transferPosts()
	env.svc.RunRetryPass(context.Background())
	if env.fake.transferPosts() != posts || env.transfer(t, in.ID).Status != model.HajimiTransferPending {
		t.Fatal("must not resend or resolve before the signature grace period")
	}
	for env.clock.Now().Before(lastSend.Add(3*time.Minute + time.Second)) {
		env.clock.Advance(20 * time.Second)
		env.svc.RunRetryPass(context.Background())
	}
	env.clock.Advance(2 * time.Minute)
	env.svc.RunRetryPass(context.Background())
	if env.fake.transferPosts() != posts {
		t.Fatalf("POST sent after the window: %d -> %d", posts, env.fake.transferPosts())
	}
	inRow, outRow := env.transfer(t, in.ID), env.transfer(t, out.ID)
	if inRow.Status != model.HajimiTransferFailed || inRow.Reason != localReasonNotFoundAfterWindow {
		t.Fatalf("in row = %#v", inRow)
	}
	if outRow.Status != model.HajimiTransferRefunded || outRow.Reason != localReasonNotFoundAfterWindow {
		t.Fatalf("out row = %#v", outRow)
	}
	if env.available(t, env.user.ID) != 20_000_000 || env.ledgerCount(t, repository.HajimiOutRefundReferenceKey(out.ID)) != 1 || env.ledgerCount(t, repository.HajimiInReferenceKey(in.ID)) != 0 {
		t.Fatalf("available after refund = %d", env.available(t, env.user.ID))
	}
	// 再次运行不会重复退款。
	env.clock.Advance(time.Hour)
	env.svc.RunRetryPass(context.Background())
	if env.available(t, env.user.ID) != 20_000_000 {
		t.Fatal("refund applied twice")
	}
}

// 桥接被关闭时 hajimi 也返回 404；必须先确认 hajimi 能正常应答才把 404 当作“不存在”。
func TestTransferNotFoundIgnoredWhileBridgeDisabled(t *testing.T) {
	env := newTestEnv(t)
	env.fake.set(func(f *fakeHajimi) { f.commitThenFail = 1 })
	transfer := env.create(t, "in", 5_000_000, idemKey("disabled"))
	env.fake.set(func(f *fakeHajimi) { f.bridgeDisabled = true })
	env.clock.Advance(14 * time.Minute)
	env.svc.RunRetryPass(context.Background())
	if row := env.transfer(t, transfer.ID); row.Status != model.HajimiTransferPending {
		t.Fatalf("disabled bridge 404 must not fail the transfer: %#v", row)
	}
	env.fake.set(func(f *fakeHajimi) { f.bridgeDisabled = false })
	env.clock.Advance(2 * time.Minute)
	env.svc.RunRetryPass(context.Background())
	if row := env.transfer(t, transfer.ID); row.Status != model.HajimiTransferCredited {
		t.Fatalf("row = %#v", row)
	}
}

func TestWithdrawCapRefundAndCompletion(t *testing.T) {
	env := newTestEnv(t)
	env.create(t, "in", 10_000_000, idemKey("seed"))
	// 影策自有积分（充值/兑换码/签到）不可提回：只有 net_in 可提。
	env.setAvailable(t, env.user.ID, 50_000_000)
	if _, err := env.svc.CreateTransfer(context.Background(), env.user, CreateTransferRequest{Direction: "out", AmountMicro: 10_000_001, IdempotencyKey: idemKey("too-much")}); appReason(err) != ReasonNetInExceeded {
		t.Fatalf("net_in cap err = %v", err)
	}
	out := env.create(t, "out", 4_000_000, idemKey("out-1"))
	if out.Status != model.HajimiTransferCompleted {
		t.Fatalf("out = %#v", out)
	}
	if env.available(t, env.user.ID) != 46_000_000 || env.ledgerCount(t, repository.HajimiOutReferenceKey(out.ID)) != 1 {
		t.Fatalf("available = %d", env.available(t, env.user.ID))
	}
	netIn, _ := env.repo.HajimiBridgeLocalNetIn(env.user.ID)
	if netIn != 6_000_000 || env.fake.user(testHajimiUserID).balance != 94_000_000 {
		t.Fatalf("netIn=%d hajimi balance=%d", netIn, env.fake.user(testHajimiUserID).balance)
	}
	replay := env.create(t, "out", 4_000_000, idemKey("out-1"))
	if replay.ID != out.ID || env.available(t, env.user.ID) != 46_000_000 {
		t.Fatal("withdraw replay must not debit twice")
	}
	env.setAvailable(t, env.user.ID, 1_000_000)
	if _, err := env.svc.CreateTransfer(context.Background(), env.user, CreateTransferRequest{Direction: "out", AmountMicro: 2_000_000, IdempotencyKey: idemKey("no-credits")}); appReason(err) != kernel.ReasonInsufficientCredits {
		t.Fatalf("insufficient credits err = %v", err)
	}
	env.setAvailable(t, env.user.ID, 6_000_000)
	env.fake.setUser(testHajimiUserID, func(u *fakeUser) { u.grant = "revoked" })
	rejected := env.create(t, "out", 3_000_000, idemKey("revoked"))
	if rejected.Status != model.HajimiTransferRefunded || rejected.Reason != "grant_inactive" {
		t.Fatalf("rejected withdraw = %#v", rejected)
	}
	if env.available(t, env.user.ID) != 6_000_000 || env.ledgerCount(t, repository.HajimiOutRefundReferenceKey(rejected.ID)) != 1 {
		t.Fatalf("refund missing: available = %d", env.available(t, env.user.ID))
	}
}

func TestTransferConflictMarksAttention(t *testing.T) {
	env := newTestEnv(t)
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = true })
	transfer := env.create(t, "in", 5_000_000, idemKey("conflict"))
	env.fake.set(func(f *fakeHajimi) { f.failTransferPosts = false })
	env.fake.injectCommitted(RemoteTransfer{TransferID: transfer.ID, Direction: remoteDirectionToCanvas, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 9_000_000})
	env.clock.Advance(20 * time.Second)
	env.svc.RunRetryPass(context.Background())
	row := env.transfer(t, transfer.ID)
	if row.Status != model.HajimiTransferAttention || row.Reason != localReasonTransferConflict || env.available(t, env.user.ID) != 0 {
		t.Fatalf("row = %#v", row)
	}
}

func TestWalletReportsRemoteStateAndDegradesWhenUnreachable(t *testing.T) {
	env := newTestEnv(t)
	env.create(t, "in", 5_000_000, idemKey("wallet"))
	wallet, err := env.svc.Wallet(context.Background(), env.user)
	if err != nil {
		t.Fatal(err)
	}
	if !wallet.Enabled || !wallet.Linked || !wallet.Reachable || *wallet.HajimiBalanceMicro != 95_000_000 || *wallet.NetInMicro != 5_000_000 || *wallet.Grant != "active" ||
		wallet.Limits.MinMicro != 1_000_000 || wallet.Limits.UserDailyUsedMicro != 5_000_000 || wallet.LocalNetInMicro != 5_000_000 || wallet.HajimiPublicURL != "https://hajimi.chat" ||
		wallet.AutoTopup.ThresholdMicro != 1_000_000 || wallet.AutoTopup.AmountMicro != 5_000_000 || wallet.AutoTopup.DailyLimitMicro != 20_000_000 || wallet.AutoTopup.Enabled {
		t.Fatalf("wallet = %#v", wallet)
	}
	env.fake.server.Close()
	wallet, err = env.svc.Wallet(context.Background(), env.user)
	if err != nil || wallet.Reachable || wallet.HajimiBalanceMicro != nil || wallet.Limits != nil || !wallet.Linked || wallet.LocalNetInMicro != 5_000_000 {
		t.Fatalf("unreachable wallet = %#v err=%v", wallet, err)
	}
}

func TestAdminRetryResolvesAttentionUsingLedgerKeys(t *testing.T) {
	env := newTestEnv(t)
	now := env.clock.Now()
	// 本地 rejected、hajimi 却已提交：游标标 attention，管理员重试后按 hajimi 结果入账一次。
	inID := "yct_00000000000000000000000000000001"
	if err := env.db.Create(&model.HajimiBridgeTransfer{ID: inID, UserID: env.user.ID, HajimiUserID: testHajimiUserID, Direction: "in", AmountMicrocredits: 3_000_000, Source: "manual", Status: model.HajimiTransferRejected, Reason: "user_daily_limit", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	env.fake.injectCommitted(RemoteTransfer{TransferID: inID, Direction: remoteDirectionToCanvas, HajimiUserID: testHajimiUserID, CanvasUserID: env.user.ID, AmountMicro: 3_000_000})
	if err := env.svc.RunCursorPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if row := env.transfer(t, inID); row.Status != model.HajimiTransferAttention || row.Reason != "remote_committed_local_rejected" || env.available(t, env.user.ID) != 0 {
		t.Fatalf("row = %#v available=%d", row, env.available(t, env.user.ID))
	}
	if _, err := env.svc.AdminRetryTransfer(context.Background(), env.user, inID); err == nil {
		t.Fatal("non-admin retry must be rejected")
	}
	result, err := env.svc.AdminRetryTransfer(context.Background(), env.admin, inID)
	if err != nil || result.Status != model.HajimiTransferCredited || result.Username != "alice" {
		t.Fatalf("retry = %#v err=%v", result, err)
	}
	if env.available(t, env.user.ID) != 3_000_000 || env.ledgerCount(t, repository.HajimiInReferenceKey(inID)) != 1 {
		t.Fatal("attention credit not applied exactly once")
	}
	if _, err := env.svc.AdminRetryTransfer(context.Background(), env.admin, inID); appReason(err) != ReasonTransferFinished {
		t.Fatalf("finished retry err = %v", err)
	}
}
