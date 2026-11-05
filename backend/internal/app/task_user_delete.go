package app

import (
	"errors"
	"strings"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

// DeleteTask 把终态任务从用户的作品列表和创作历史中移除。
// 任务行、计费订单和已生成文件都保留：账务核对、后台统计和画布/素材里的产物引用仍依赖它们。
func (s *Service) DeleteTask(userID string, id string) error {
	id = strings.TrimSpace(id)
	if userID == "" || id == "" {
		return NotFound("任务不存在")
	}
	task, err := s.repo.TaskForUser(userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NotFound("任务不存在")
	}
	if err != nil {
		return err
	}
	if task.UserDeletedAt != nil {
		return nil
	}
	if !taskUserDeletable(task.Status) {
		return BadAuthRequest("任务仍在生成，请先取消再删除")
	}
	marked, err := s.repo.MarkTaskUserDeleted(userID, id, time.Now().UTC())
	if err != nil || marked {
		return err
	}
	// 条件写入没有命中：可能已被并发删除，也可能刚被重试回到排队。
	latest, err := s.repo.TaskForUser(userID, id)
	if err != nil {
		return err
	}
	if latest.UserDeletedAt != nil {
		return nil
	}
	return BadAuthRequest("任务状态已变化，请刷新后重试")
}

func taskUserDeletable(status model.TaskStatus) bool {
	return status == model.TaskStatusSucceeded || status == model.TaskStatusFailed || status == model.TaskStatusCancelled
}
