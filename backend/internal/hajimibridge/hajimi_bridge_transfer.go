package hajimibridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

const (
	localReasonNotFoundAfterWindow = "not_found_after_window"
	localReasonRemoteMismatch      = "remote_mismatch"
	localReasonTransferConflict    = "transfer_conflict"
	localReasonMissingLocalRecord  = "missing_local_record"
	localReasonCommittedButLocal   = "remote_committed_local_"
)

func newTransferID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "yct_" + hex.EncodeToString(value[:]), nil
}

func remoteDirection(direction string) string {
	if direction == model.HajimiTransferDirectionOut {
		return remoteDirectionToHajimi
	}
	return remoteDirectionToCanvas
}

func localDirection(remote string) (string, bool) {
	switch remote {
	case remoteDirectionToCanvas:
		return model.HajimiTransferDirectionIn, true
	case remoteDirectionToHajimi:
		return model.HajimiTransferDirectionOut, true
	}
	return "", false
}

func transferNote(direction string, id string) string {
	if direction == model.HajimiTransferDirectionOut {
		return "转回 hajimi #" + id
	}
	return "从 hajimi 转入 #" + id
}

func refundNote(id string) string { return "转回 hajimi 未成功，已退回 #" + id }

// retryDelay：15s、30s、60s 后固定 2 分钟，10 分钟窗口内约发送 7 次。
func retryDelay(attempts int) time.Duration {
	delay := 15 * time.Second
	for index := 1; index < attempts && delay < 2*time.Minute; index++ {
		delay *= 2
	}
	if delay > 2*time.Minute {
		delay = 2 * time.Minute
	}
	return delay
}

// CreateTransfer 登记一笔划转并在请求内同步尝试一次（≤ 8 秒）；结果可能仍是 pending。
func (s *Service) CreateTransfer(ctx context.Context, user *model.User, req CreateTransferRequest) (*Transfer, error) {
	if user == nil {
		return nil, kernel.Unauthorized("请先登录")
	}
	if err := s.host.RequireCreditsFeature(); err != nil {
		return nil, err
	}
	if !s.config.Active() {
		return nil, errDisabled()
	}
	if req.Direction != model.HajimiTransferDirectionIn && req.Direction != model.HajimiTransferDirectionOut {
		return nil, kernel.BadAuthRequest("划转方向无效")
	}
	source := req.Source
	if source == "" {
		source = model.HajimiTransferSourceManual
	}
	if source != model.HajimiTransferSourceManual && source != model.HajimiTransferSourcePrompt {
		return nil, kernel.BadAuthRequest("划转来源无效")
	}
	if !idempotencyKeyPattern.MatchString(req.IdempotencyKey) {
		return nil, kernel.BadAuthRequest("幂等键需为 16-80 位字母、数字、下划线或连字符")
	}
	if req.AmountMicro <= 0 || req.AmountMicro > maxSaneMicro {
		return nil, kernel.BadAuthRequest("划转金额无效")
	}
	if req.Direction == model.HajimiTransferDirectionOut && req.AmountMicro < minWithdrawMicro {
		return nil, kernel.BadAuthRequest("单次转回金额至少 0.01")
	}
	hajimiUserID, linked, err := s.linkedHajimiUser(user.ID)
	if err != nil {
		return nil, err
	}
	if !linked {
		return nil, errNotLinked()
	}
	if existing, err := s.repo.HajimiBridgeTransferByClientKey(user.ID, req.IdempotencyKey); err == nil {
		return s.replayTransfer(ctx, existing, req, hajimiUserID)
	} else if !notFound(err) {
		return nil, err
	}
	if !s.client.Available() {
		return nil, errUnavailable()
	}
	id, err := newTransferID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	clientKey := req.IdempotencyKey
	row := &model.HajimiBridgeTransfer{
		ID: id, UserID: user.ID, HajimiUserID: hajimiUserID, Direction: req.Direction, AmountMicrocredits: req.AmountMicro,
		ClientKey: &clientKey, Source: source, Status: model.HajimiTransferPending, NextAttemptAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if req.Direction == model.HajimiTransferDirectionIn {
		err = s.repo.CreateHajimiBridgeInTransfer(row)
	} else {
		err = s.repo.CreateHajimiBridgeOutTransfer(row, transferNote(row.Direction, row.ID))
	}
	switch {
	case errors.Is(err, repository.ErrHajimiBridgeDuplicate):
		existing, readErr := s.repo.HajimiBridgeTransferByClientKey(user.ID, req.IdempotencyKey)
		if readErr != nil {
			return nil, readErr
		}
		return s.replayTransfer(ctx, existing, req, hajimiUserID)
	case errors.Is(err, repository.ErrHajimiBridgeNetInExceeded):
		return nil, &kernel.AppError{Status: http.StatusBadRequest, Code: http.StatusBadRequest, Reason: ReasonNetInExceeded, Message: "转回金额超过可转回额度（只能转回从 hajimi 转入的净额）"}
	case errors.Is(err, repository.ErrInsufficientCredits):
		return nil, kernel.InsufficientCredits("可用积分不足，不能转回")
	case err != nil:
		return nil, err
	}
	s.process(ctx, row, s.syncTimeout)
	return s.reloadTransfer(row.ID)
}

// replayTransfer：同一用户同一幂等键只对应一笔划转；参数不同视为冲突，绝不发起第二次资金移动。
func (s *Service) replayTransfer(ctx context.Context, existing *model.HajimiBridgeTransfer, req CreateTransferRequest, hajimiUserID int64) (*Transfer, error) {
	if existing.Direction != req.Direction || existing.AmountMicrocredits != req.AmountMicro || existing.HajimiUserID != hajimiUserID {
		return nil, errIdempotencyConflict()
	}
	if existing.Status == model.HajimiTransferPending {
		s.process(ctx, existing, s.syncTimeout)
		return s.reloadTransfer(existing.ID)
	}
	transfer := toTransfer(existing)
	return &transfer, nil
}

func (s *Service) reloadTransfer(id string) (*Transfer, error) {
	row, err := s.repo.HajimiBridgeTransfer(id)
	if err != nil {
		return nil, err
	}
	transfer := toTransfer(row)
	return &transfer, nil
}

// process 推进一笔 pending 划转：10 分钟窗口内用同一 transfer_id 重发；
// 窗口之后不再发送，等最后一次发送满 3 分钟（hajimi 拒绝提交签名超过 150 秒的请求）再查询定论。
func (s *Service) process(ctx context.Context, row *model.HajimiBridgeTransfer, timeout time.Duration) {
	if row.Status != model.HajimiTransferPending {
		return
	}
	now := s.now()
	if !s.client.Available() {
		return
	}
	if now.Sub(row.CreatedAt) >= s.resolveAfter {
		s.resolveAfterWindow(ctx, row, timeout)
		return
	}
	// 发送占用同时要求记录仍在 10 分钟窗口内，窗口之后数据库层面也不会再发出新请求。
	claimed, err := s.repo.ClaimHajimiBridgeAttempt(row.ID, now, now.Add(retryDelay(row.Attempts+1)), now.Add(-s.resolveAfter))
	if err != nil {
		slog.Error("hajimi transfer claim failed", "transfer_id", row.ID, "error", err)
		return
	}
	if !claimed {
		return
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	remote, err := s.client.CreateTransfer(callCtx, RemoteTransferRequest{
		TransferID: row.ID, Direction: remoteDirection(row.Direction), HajimiUserID: row.HajimiUserID, CanvasUserID: row.UserID, AmountMicro: row.AmountMicrocredits,
	})
	if err != nil {
		if isRemoteCode(err, http.StatusConflict, remoteErrorConflict) {
			_ = s.markAttention(row, localReasonTransferConflict, []string{model.HajimiTransferPending})
			return
		}
		s.noteError(row, err, nil)
		return
	}
	s.applyRemote(row, remote)
}

func (s *Service) resolveAfterWindow(ctx context.Context, row *model.HajimiBridgeTransfer, timeout time.Duration) {
	now := s.now()
	lastSend := row.CreatedAt
	if row.LastAttemptAt != nil {
		lastSend = *row.LastAttemptAt
	}
	if earliest := lastSend.Add(s.signatureGrace); now.Before(earliest) {
		if _, err := s.repo.ClaimHajimiBridgeCheck(row.ID, now, earliest); err != nil {
			slog.Error("hajimi transfer reschedule failed", "transfer_id", row.ID, "error", err)
		}
		return
	}
	// 占用查询时再次以数据库中的 last_attempt_at 为准：批量读取后若另一实例刚发送过，
	// 该请求仍在签名有效期内，本次不查询，留给下一轮重新计算等待时间。
	claimed, err := s.repo.ClaimHajimiBridgeLookup(row.ID, now, now.Add(-s.signatureGrace), now.Add(time.Minute))
	if err != nil || !claimed {
		return
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	remote, found, err := s.lookupRemote(callCtx, row.ID)
	if err != nil {
		s.noteError(row, err, nil)
		return
	}
	if found {
		s.applyRemote(row, remote)
		return
	}
	// hajimi 拒绝提交签名时间戳超过 150 秒的请求，最后一次发送已满 3 分钟，
	// 明确的 transfer_not_found 即可定论为未发生。
	if row.Direction == model.HajimiTransferDirectionIn {
		if _, err := s.repo.ResolveHajimiBridgeTransfer(row.ID, []string{model.HajimiTransferPending}, model.HajimiTransferFailed, localReasonNotFoundAfterWindow, 0, s.now()); err != nil {
			slog.Error("hajimi transfer fail update failed", "transfer_id", row.ID, "error", err)
		}
		return
	}
	if _, err := s.repo.RefundHajimiBridgeOut(row.ID, []string{model.HajimiTransferPending}, localReasonNotFoundAfterWindow, refundNote(row.ID), s.now()); err != nil {
		slog.Error("hajimi withdraw refund failed", "transfer_id", row.ID, "error", err)
	}
}

// lookupRemote 查询 hajimi 记录：found=false 只在 hajimi 明确答复 transfer_not_found 时返回；
// 桥接关闭、路由不存在或非 JSON 的 404 一律作为错误，稍后重试。
func (s *Service) lookupRemote(ctx context.Context, id string) (*RemoteTransfer, bool, error) {
	remote, err := s.client.GetTransfer(ctx, id)
	if err == nil {
		return remote, true, nil
	}
	if isRemoteTransferAbsent(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func remoteMatches(row *model.HajimiBridgeTransfer, remote *RemoteTransfer) bool {
	return remote.TransferID == row.ID && remote.Direction == remoteDirection(row.Direction) &&
		remote.AmountMicro == row.AmountMicrocredits && remote.HajimiUserID == row.HajimiUserID && remote.CanvasUserID == row.UserID
}

// applyRemote 把 hajimi 的定论结果应用到 pending 记录；所有迁移都是条件更新，重复应用无副作用。
func (s *Service) applyRemote(row *model.HajimiBridgeTransfer, remote *RemoteTransfer) {
	pending := []string{model.HajimiTransferPending}
	if !remoteMatches(row, remote) {
		_ = s.markAttention(row, localReasonRemoteMismatch, pending)
		return
	}
	now := s.now()
	var err error
	switch {
	case remote.Status == remoteStatusCommitted && row.Direction == model.HajimiTransferDirectionIn:
		_, err = s.repo.ApplyHajimiBridgeInCommitted(row.ID, remote.Seq, pending, transferNote(row.Direction, row.ID), now)
	case remote.Status == remoteStatusCommitted:
		_, err = s.repo.ResolveHajimiBridgeTransfer(row.ID, pending, model.HajimiTransferCompleted, "", remote.Seq, now)
	case remote.Status == remoteStatusRejected && row.Direction == model.HajimiTransferDirectionIn:
		_, err = s.repo.ResolveHajimiBridgeTransfer(row.ID, pending, model.HajimiTransferRejected, remote.Reason, 0, now)
	case remote.Status == remoteStatusRejected:
		_, err = s.repo.RefundHajimiBridgeOut(row.ID, pending, remote.Reason, refundNote(row.ID), now)
	default:
		s.noteError(row, errBridgeResponseInvalid, nil)
		return
	}
	if err != nil {
		slog.Error("hajimi transfer apply failed", "transfer_id", row.ID, "error", err)
	}
}

// markAttention 返回写库错误：游标对账依赖它决定能否前移，未成功标记的矛盾不能被跳过。
func (s *Service) markAttention(row *model.HajimiBridgeTransfer, reason string, from []string) error {
	changed, err := s.repo.ResolveHajimiBridgeTransfer(row.ID, from, model.HajimiTransferAttention, reason, 0, s.now())
	if err != nil {
		slog.Error("hajimi transfer attention update failed", "transfer_id", row.ID, "error", err)
		return err
	}
	if changed {
		slog.Warn("hajimi transfer needs admin attention", "transfer_id", row.ID, "user_id", row.UserID, "reason", reason)
	}
	return nil
}

func (s *Service) noteError(row *model.HajimiBridgeTransfer, err error, next *time.Time) {
	if noteErr := s.repo.NoteHajimiBridgeError(row.ID, safeBridgeError(err), next, s.now()); noteErr != nil {
		slog.Error("hajimi transfer error note failed", "transfer_id", row.ID, "error", noteErr)
	}
	slog.Warn("hajimi transfer attempt unresolved", "transfer_id", row.ID, "error", safeBridgeError(err))
}

// AdminRetryTransfer：pending 立即重试；failed/attention 按 hajimi 的定论和本地账本键重新核对。
// 只有在本地账本状态能唯一确定时才动钱，其余情况保持 attention 交给人工处理。
func (s *Service) AdminRetryTransfer(ctx context.Context, actor *model.User, id string) (*AdminTransfer, error) {
	if err := s.host.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if !s.config.Active() {
		return nil, errDisabled()
	}
	row, err := s.repo.HajimiBridgeTransfer(id)
	if notFound(err) {
		return nil, kernel.NotFound("划转记录不存在")
	}
	if err != nil {
		return nil, err
	}
	switch row.Status {
	case model.HajimiTransferPending:
		if _, err := s.repo.RescheduleHajimiBridgeTransfer(row.ID, s.now()); err != nil {
			return nil, err
		}
		if row, err = s.repo.HajimiBridgeTransfer(id); err != nil {
			return nil, err
		}
		s.process(ctx, row, s.syncTimeout)
	case model.HajimiTransferFailed, model.HajimiTransferAttention:
		if err := s.reevaluate(ctx, row); err != nil {
			return nil, err
		}
	default:
		return nil, &kernel.AppError{Status: http.StatusConflict, Code: http.StatusConflict, Reason: ReasonTransferFinished, Message: "该划转已结束，无需重试"}
	}
	row, err = s.repo.HajimiBridgeTransfer(id)
	if err != nil {
		return nil, err
	}
	result := toAdminTransfer(row, "")
	if user, userErr := s.repo.User(row.UserID); userErr == nil {
		result.Username = user.Username
	}
	return &result, nil
}

func (s *Service) reevaluate(ctx context.Context, row *model.HajimiBridgeTransfer) error {
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.syncTimeout)
	defer cancel()
	remote, found, err := s.lookupRemote(callCtx, row.ID)
	if err != nil {
		return errUnavailable()
	}
	if found && !remoteMatches(row, remote) {
		return errManualReview("hajimi 记录与本地记录不一致，需要人工核对")
	}
	if _, err := s.repo.User(row.UserID); err != nil {
		if notFound(err) {
			return errManualReview("对应的影策用户不存在，需要人工处理")
		}
		return err
	}
	from := []string{row.Status}
	now := s.now()
	creditExists, err := s.repo.CreditLedgerReferenceExists(repository.HajimiInReferenceKey(row.ID))
	if err != nil {
		return err
	}
	debitExists, err := s.repo.CreditLedgerReferenceExists(repository.HajimiOutReferenceKey(row.ID))
	if err != nil {
		return err
	}
	refundExists, err := s.repo.CreditLedgerReferenceExists(repository.HajimiOutRefundReferenceKey(row.ID))
	if err != nil {
		return err
	}
	committed := found && remote.Status == remoteStatusCommitted
	var changed bool
	switch {
	case row.Direction == model.HajimiTransferDirectionIn && committed:
		changed, err = s.repo.ApplyHajimiBridgeInCommitted(row.ID, remote.Seq, from, transferNote(row.Direction, row.ID), now)
	case row.Direction == model.HajimiTransferDirectionIn && creditExists:
		return errManualReview("本地已入账但 hajimi 未提交，需要人工调账")
	case row.Direction == model.HajimiTransferDirectionIn && found:
		changed, err = s.repo.ResolveHajimiBridgeTransfer(row.ID, from, model.HajimiTransferRejected, remote.Reason, 0, now)
	case row.Direction == model.HajimiTransferDirectionIn:
		changed, err = s.repo.ResolveHajimiBridgeTransfer(row.ID, from, model.HajimiTransferFailed, localReasonNotFoundAfterWindow, 0, now)
	case !debitExists:
		return errManualReview("本地没有对应扣款记录，需要人工调账")
	case committed && !refundExists:
		changed, err = s.repo.ResolveHajimiBridgeTransfer(row.ID, from, model.HajimiTransferCompleted, "", remote.Seq, now)
	case committed:
		return errManualReview("hajimi 已入账且本地已退款，需要人工调账")
	case refundExists:
		changed, err = s.repo.ResolveHajimiBridgeTransfer(row.ID, from, model.HajimiTransferRefunded, "", 0, now)
	default:
		reason := localReasonNotFoundAfterWindow
		if found {
			reason = remote.Reason
		}
		changed, err = s.repo.RefundHajimiBridgeOut(row.ID, from, reason, refundNote(row.ID), now)
	}
	if err != nil {
		return err
	}
	if !changed {
		return &kernel.AppError{Status: http.StatusConflict, Code: http.StatusConflict, Reason: kernel.ReasonConflict, Message: "划转状态已变化，请刷新后重试"}
	}
	return nil
}
