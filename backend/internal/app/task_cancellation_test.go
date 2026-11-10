package app

import (
	"context"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCancelTaskCancelsRunningTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now()
	task := model.Task{
		ID:        "running-task",
		UserID:    "user-1",
		Status:    model.TaskStatusRunning,
		Stage:     "后端接管任务",
		StartedAt: &startedAt,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	cancelled, err := svc.CancelTask(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != model.TaskStatusCancelled {
		t.Fatalf("CancelTask() status = %s, want cancelled", cancelled.Status)
	}

	stored, err := svc.repo.TaskForUser(task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusCancelled || stored.CompletedAt == nil {
		t.Fatalf("running task was not cancelled: status=%s completedAt=%v", stored.Status, stored.CompletedAt)
	}
}

func TestCancelTaskRejectsUserCancellationAfterProviderStageStarts(t *testing.T) {
	for _, stage := range []string{"调用生成模型", "正在连接上游", "上游生成中", "后台仍在生成", "等待上游任务同步", "generating", "submitting", "submitted", "submission_unknown", "provider_processing", "作品已生成，正在保存"} {
		t.Run(stage, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.Task{}); err != nil {
				t.Fatal(err)
			}
			task := model.Task{ID: "provider-stage-task", UserID: "user-1", Status: model.TaskStatusRunning, Stage: stage}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}

			svc := &Service{repo: repository.New(db)}
			if _, err := svc.CancelTask(context.Background(), task.UserID, task.ID); err == nil || err.Error() != "第三方请求已提交，任务不可取消" {
				t.Fatalf("CancelTask() error = %v", err)
			}
		})
	}
}

func TestCancelTaskRejectsUserCancellationDuringMediaMaterialization(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "media-stage-task", UserID: "user-1", Status: model.TaskStatusRunning, Stage: "作品已生成，正在保存", MediaStage: "upload"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	if _, err := svc.CancelTask(context.Background(), task.UserID, task.ID); err == nil || err.Error() != "第三方请求已提交，任务不可取消" {
		t.Fatalf("CancelTask() error = %v", err)
	}
}

func TestInternalCancellationStillCoordinatesSubmittedTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "submitted-child", UserID: "user-1", Status: model.TaskStatusRunning, Stage: "上游生成中", ProviderRequestID: "provider-request-1"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	cancelled, err := svc.taskLifecycle().cancelTaskWithIntent(context.Background(), task.UserID, task.ID, model.TaskCancellationIntent{Source: model.TaskCancellationParentFailed})
	if err != nil {
		t.Fatalf("internal cancel error = %v", err)
	}
	if cancelled.Status != model.TaskStatusCancelled || cancelled.CancellationSource != model.TaskCancellationParentFailed {
		t.Fatalf("internal cancel = %+v", cancelled)
	}
}

func TestCancelTaskCancelsQueuedTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "queued-task", UserID: "user-1", Status: model.TaskStatusQueued, Stage: "等待队列调度"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	cancelled, err := svc.CancelTask(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != model.TaskStatusCancelled {
		t.Fatalf("CancelTask() status = %s, want cancelled", cancelled.Status)
	}

	storedTask, err := svc.repo.TaskForUser(task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusCancelled || storedTask.CompletedAt == nil {
		t.Fatalf("queued task was not cancelled: status=%s completedAt=%v", storedTask.Status, storedTask.CompletedAt)
	}
}

func TestCancelTaskRejectsTaskWithProviderRequestID(t *testing.T) {
	for _, status := range []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.Task{}); err != nil {
				t.Fatal(err)
			}
			task := model.Task{
				ID:                "submitted-" + string(status),
				UserID:            "user-1",
				Status:            status,
				ProviderRequestID: "provider-request-1",
			}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}

			svc := &Service{repo: repository.New(db)}
			if _, err := svc.CancelTask(context.Background(), task.UserID, task.ID); err == nil || err.Error() != "第三方请求已提交，任务不可取消" {
				t.Fatalf("CancelTask() error = %v", err)
			}

			stored, err := svc.repo.TaskForUser(task.UserID, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != status || stored.ProviderRequestID != task.ProviderRequestID {
				t.Fatalf("submitted task changed after cancellation attempt: status=%s providerRequestId=%q", stored.Status, stored.ProviderRequestID)
			}
		})
	}
}

func TestAdminCancelUserTaskStopsSubmittedTaskAndAudits(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.User{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	admin := model.User{ID: "admin-1", Username: "admin", Role: "admin", Status: "active"}
	owner := model.User{ID: "user-1", Username: "owner", Role: "user", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	// 没有上游 request ID 但已进入上游阶段：用户自助取消会被拒绝。
	task := model.Task{ID: "stuck-task", UserID: owner.ID, Status: model.TaskStatusRunning, Stage: "上游生成中"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}

	if _, err := svc.CancelTask(context.Background(), owner.ID, task.ID); err == nil {
		t.Fatal("user cancel should be rejected after upstream submission")
	}
	if _, err := svc.AdminCancelUserTask(context.Background(), &owner, owner.ID, task.ID); err == nil {
		t.Fatal("non-admin must not use admin cancel")
	}
	if _, err := svc.AdminCancelUserTask(context.Background(), &admin, admin.ID, task.ID); err == nil {
		t.Fatal("task must be resolved under its owner")
	}

	cancelled, err := svc.AdminCancelUserTask(context.Background(), &admin, owner.ID, task.ID)
	if err != nil {
		t.Fatalf("AdminCancelUserTask() error = %v", err)
	}
	if cancelled.Status != model.TaskStatusCancelled || cancelled.CancellationSource != model.TaskCancellationAdminRequest || cancelled.CancellationActorID != admin.ID {
		t.Fatalf("admin cancel = status %s source %s actor %s", cancelled.Status, cancelled.CancellationSource, cancelled.CancellationActorID)
	}
	stored, err := svc.repo.TaskForUser(owner.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusCancelled || stored.CancellationSource != model.TaskCancellationAdminRequest {
		t.Fatalf("stored task = status %s source %s", stored.Status, stored.CancellationSource)
	}

	// 重复取消幂等，且不重复写审计。
	if _, err := svc.AdminCancelUserTask(context.Background(), &admin, owner.ID, task.ID); err != nil {
		t.Fatalf("repeat admin cancel error = %v", err)
	}
	var audits []model.AdminAuditEvent
	if err := db.Where("action = ?", "user.task.cancel").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].ActorUserID != admin.ID || audits[0].TargetID != owner.ID {
		t.Fatalf("audit events = %+v", audits)
	}

	done := model.Task{ID: "done-task", UserID: owner.ID, Status: model.TaskStatusSucceeded}
	if err := db.Create(&done).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdminCancelUserTask(context.Background(), &admin, owner.ID, done.ID); err == nil {
		t.Fatal("finished task must not be cancellable")
	}
}
