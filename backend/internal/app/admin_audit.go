package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdminUserDetail struct {
	User             model.User                  `json:"user"`
	Account          model.CreditAccount         `json:"account"`
	Counts           repository.AdminUserCounts  `json:"counts"`
	StorageUsage     repository.UserStorageUsage `json:"storageUsage"`
	StoredFileBytes  int64                       `json:"storedFileBytes"`
	DailyUploadBytes int64                       `json:"dailyUploadBytes"`
	Quota            RuntimeResourcePolicy       `json:"quota"`
	// StorageCapacity 是包含已购扩容的有效账号文件容量与超额状态。
	StorageCapacity AccountFileStorageUsage `json:"storageCapacity"`
}

type AdminTaskPage struct {
	Tasks []model.Task `json:"tasks"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Limit int          `json:"pageSize"`
}

type AdminAuditPage struct {
	Events []model.AdminAuditEvent `json:"events"`
	Total  int64                   `json:"total"`
	Page   int                     `json:"page"`
	Limit  int                     `json:"pageSize"`
}

func (s *Service) appendAdminAudit(actor *model.User, action string, targetType string, targetID string, summary string, metadata any) error {
	event, err := newAdminAuditEvent(actor, action, targetType, targetID, summary, metadata)
	if err != nil {
		return err
	}
	return s.repo.AppendAdminAudit(event)
}

func newAdminAuditEvent(actor *model.User, action string, targetType string, targetID string, summary string, metadata any) (*model.AdminAuditEvent, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	encoded := ""
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		encoded = string(data)
	}
	return &model.AdminAuditEvent{
		ID: newID(), ActorUserID: actor.ID, Action: strings.TrimSpace(action), TargetType: strings.TrimSpace(targetType),
		TargetID: strings.TrimSpace(targetID), Summary: truncateRunes(strings.TrimSpace(summary), 500), MetadataJSON: encoded, CreatedAt: time.Now(),
	}, nil
}

func (s *Service) AdminUserDetail(actor *model.User, userID string) (*AdminUserDetail, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	user, err := s.repo.User(strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	account, err := s.repo.CreditAccount(user.ID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.AdminUserCounts(user.ID)
	if err != nil {
		return nil, err
	}
	usage, err := s.repo.UserStorageUsage(user.ID)
	if err != nil {
		return nil, err
	}
	storedFileBytes, err := s.repo.UserStoredFileBytes(user.ID)
	if err != nil {
		return nil, err
	}
	dailyUploadBytes, err := s.repo.DailyUploadBytes(user.ID, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	capacity, err := s.computeAccountStorageStatus(user.ID, time.Now(), false)
	if err != nil {
		return nil, err
	}
	return &AdminUserDetail{
		User: *user, Account: *account, Counts: counts, StorageUsage: usage,
		StoredFileBytes: storedFileBytes, DailyUploadBytes: dailyUploadBytes, Quota: policy.Resource,
		StorageCapacity: *capacity,
	}, nil
}

func (s *Service) AdminUserLedger(actor *model.User, userID string, entryType string, page int, limit int) (*WalletSummary, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.repo.User(userID); err != nil {
		return nil, err
	}
	page, limit = normalizeAdminPage(page, limit)
	account, err := s.repo.CreditAccount(userID)
	if err != nil {
		return nil, err
	}
	entries, total, err := s.repo.CreditLedger(userID, entryType, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	for index := range entries {
		if strings.TrimSpace(entries[index].Note) != "" {
			entries[index].Note = userFacingTaskError(entries[index].Note)
		}
	}
	return &WalletSummary{Account: *account, Entries: entries, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminUserTasks(actor *model.User, userID string, page int, limit int) (*AdminTaskPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.repo.User(userID); err != nil {
		return nil, err
	}
	page, limit = normalizeAdminPage(page, limit)
	tasks, total, err := s.repo.AdminUserTasks(userID, limit, (page-1)*limit)
	return &AdminTaskPage{Tasks: tasks, Total: total, Page: page, Limit: limit}, err
}

// AdminCancelUserTask 让管理员终止指定用户仍在排队或运行的任务。
// 与用户自助取消不同，上游已提交的任务也能取消：任务立即进入 cancelled，
// 随后由上游取消与对账流程决定退款或标记费用待核对。
func (s *Service) AdminCancelUserTask(ctx context.Context, actor *model.User, userID string, taskID string) (*model.Task, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	userID, taskID = strings.TrimSpace(userID), strings.TrimSpace(taskID)
	if _, err := s.repo.User(userID); err != nil {
		return nil, err
	}
	existing, err := s.repo.TaskForUser(userID, taskID)
	if err != nil {
		return nil, err
	}
	if existing.Status == model.TaskStatusCancelled {
		return taskForOutput(*existing), nil
	}
	if existing.Status != model.TaskStatusQueued && existing.Status != model.TaskStatusRunning {
		return nil, BadAuthRequest("只有排队中或运行中的任务可以取消")
	}
	task, err := s.taskLifecycle().cancelTaskWithIntent(ctx, userID, taskID, model.TaskCancellationIntent{Source: model.TaskCancellationAdminRequest, ActorID: actor.ID})
	if err != nil {
		return nil, err
	}
	// 任务已落库为 cancelled；审计写入失败只留任务日志，不能把结果表现成取消失败。
	if err := s.appendAdminAudit(actor, "user.task.cancel", "user", userID, "取消用户任务："+task.ID, map[string]any{
		"taskId": task.ID, "type": task.Type, "model": task.Model, "previousStatus": existing.Status, "previousStage": existing.Stage,
		"upstreamSubmitted": taskUpstreamSubmitted(existing),
	}); err != nil {
		_ = s.log(userID, task.ID, "error", "管理员取消任务后写入审计失败", err.Error())
	}
	return task, nil
}

func (s *Service) AdminUserAuditEvents(actor *model.User, userID string, page int, limit int) (*AdminAuditPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.repo.User(userID); err != nil {
		return nil, err
	}
	page, limit = normalizeAdminPage(page, limit)
	events, total, err := s.repo.AdminAuditEvents("user", userID, limit, (page-1)*limit)
	return &AdminAuditPage{Events: events, Total: total, Page: page, Limit: limit}, err
}
