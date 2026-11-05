package app

import (
	"errors"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func taskDeleteErrorStatus(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Status
	}
	return 0
}

func TestDeleteTaskHidesTerminalTaskFromUserLists(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	now := time.Now()
	tasks := []model.Task{
		{ID: "video-done", UserID: "owner", Type: "canvas_video", WorkbenchKind: "video", Status: model.TaskStatusSucceeded, InputJSON: `{}`, CreatedAt: now},
		{ID: "video-failed", UserID: "owner", Type: "canvas_video", WorkbenchKind: "video", Status: model.TaskStatusFailed, Error: "上游失败", InputJSON: `{}`, CreatedAt: now.Add(time.Second)},
		{ID: "video-running", UserID: "owner", Type: "canvas_video", WorkbenchKind: "video", Status: model.TaskStatusRunning, InputJSON: `{}`, CreatedAt: now.Add(2 * time.Second)},
		{ID: "other-done", UserID: "other", Type: "canvas_video", WorkbenchKind: "video", Status: model.TaskStatusSucceeded, InputJSON: `{}`, CreatedAt: now.Add(3 * time.Second)},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteTask("owner", "video-done"); err != nil {
		t.Fatalf("delete succeeded task: %v", err)
	}
	if err := s.DeleteTask("owner", "video-done"); err != nil {
		t.Fatalf("repeat delete should be idempotent: %v", err)
	}
	if err := s.DeleteTask("owner", "video-failed"); err != nil {
		t.Fatalf("delete failed task: %v", err)
	}
	if status := taskDeleteErrorStatus(s.DeleteTask("owner", "video-running")); status != 400 {
		t.Fatalf("delete running task status = %d, want 400", status)
	}
	if status := taskDeleteErrorStatus(s.DeleteTask("owner", "other-done")); status != 404 {
		t.Fatalf("delete another user's task status = %d, want 404", status)
	}
	if status := taskDeleteErrorStatus(s.DeleteTask("owner", "missing")); status != 404 {
		t.Fatalf("delete missing task status = %d, want 404", status)
	}

	for _, options := range []TaskListOptions{{Limit: 50, WorkbenchKind: "video"}, {Limit: 50}} {
		listed, err := s.TasksWithOptions("owner", options)
		if err != nil {
			t.Fatal(err)
		}
		if len(listed) != 1 || listed[0].ID != "video-running" {
			t.Fatalf("list %+v = %#v, want only the running task", options, listed)
		}
	}

	// 任务行保留给计费与后台统计，只是带上删除标记。
	kept, err := s.repo.TaskForUser("owner", "video-done")
	if err != nil || kept.UserDeletedAt == nil {
		t.Fatalf("deleted task row = %#v, %v; want kept with deletion mark", kept, err)
	}
	other, err := s.repo.TaskForUser("other", "other-done")
	if err != nil || other.UserDeletedAt != nil {
		t.Fatalf("another user's task = %#v, %v; want untouched", other, err)
	}

	if _, err := s.RetryTask("owner", "video-failed"); taskDeleteErrorStatus(err) != 404 {
		t.Fatalf("retry deleted task error = %v, want 404", err)
	}
}

func TestDeleteTaskRejectsTaskRetriedConcurrently(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	task := model.Task{ID: "image-failed", UserID: "owner", Type: "canvas_image", WorkbenchKind: "image", Status: model.TaskStatusFailed, InputJSON: `{}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	// 模拟读取之后、标记之前任务被重试回到排队：条件写入不能把它藏起来。
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusQueued).Error; err != nil {
		t.Fatal(err)
	}
	marked, err := s.repo.MarkTaskUserDeleted("owner", task.ID, time.Now())
	if err != nil || marked {
		t.Fatalf("mark queued task = %v, %v; want not marked", marked, err)
	}
}
