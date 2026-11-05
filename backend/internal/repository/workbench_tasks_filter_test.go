package repository

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

// 工作台失败卡片和任务中心都从列表读取失败原因；列表漏选 error 列时所有失败都会退化成默认文案。
func TestTasksListIncludesFailureReason(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	const reason = "模型服务拒绝了请求，请检查模型和参数"
	if err := db.Create(&model.Task{ID: "failed-image", UserID: "user", Type: "canvas_image", WorkbenchKind: "image", Status: model.TaskStatusFailed, Error: reason, InputJSON: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	for _, kind := range []string{"image", ""} {
		got, err := repo.TasksWithWorkbenchKind("user", 50, "", false, kind)
		if err != nil {
			t.Fatalf("list tasks (kind=%q): %v", kind, err)
		}
		if len(got) != 1 || got[0].Error != reason {
			t.Fatalf("list tasks (kind=%q) = %#v, want failure reason %q", kind, got, reason)
		}
	}
}

func TestTasksWithWorkbenchKindRequiresMatchingTaskType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}

	legacyImage := `{"metadata":{"source":"workbench","workbenchKind":"image"}}`
	legacyVideo := `{"metadata":{"source":"workbench","workbenchKind":"video"}}`
	createdAt := time.Now()
	tasks := []model.Task{
		{ID: "image-column", UserID: "user", Type: "canvas_image", WorkbenchKind: "image", InputJSON: `{}`, CreatedAt: createdAt},
		{ID: "image-legacy", UserID: "user", Type: "canvas_image", InputJSON: legacyImage, CreatedAt: createdAt.Add(time.Second)},
		{ID: "video-column", UserID: "user", Type: "canvas_video", WorkbenchKind: "video", InputJSON: `{}`, CreatedAt: createdAt.Add(2 * time.Second)},
		{ID: "video-legacy", UserID: "user", Type: "canvas_video", InputJSON: legacyVideo, CreatedAt: createdAt.Add(3 * time.Second)},
		{ID: "wrong-column-kind", UserID: "user", Type: "canvas_video", WorkbenchKind: "image", InputJSON: `{}`, CreatedAt: createdAt.Add(4 * time.Second)},
		{ID: "wrong-legacy-type", UserID: "user", Type: "canvas_video", InputJSON: legacyImage, CreatedAt: createdAt.Add(5 * time.Second)},
		{ID: "wrong-source", UserID: "user", Type: "canvas_image", InputJSON: `{"metadata":{"source":"canvas","workbenchKind":"image"}}`, CreatedAt: createdAt.Add(6 * time.Second)},
		{ID: "other-user", UserID: "other", Type: "canvas_image", InputJSON: legacyImage, CreatedAt: createdAt.Add(7 * time.Second)},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}

	repo := New(db)
	for _, tc := range []struct {
		kind string
		want map[string]bool
	}{
		{kind: "image", want: map[string]bool{"image-column": true, "image-legacy": true}},
		{kind: "video", want: map[string]bool{"video-column": true, "video-legacy": true}},
	} {
		got, err := repo.TasksWithWorkbenchKind("user", 50, "", false, tc.kind)
		if err != nil {
			t.Fatalf("list %s tasks: %v", tc.kind, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%s task IDs = %#v, want %d tasks", tc.kind, got, len(tc.want))
		}
		for _, task := range got {
			if !tc.want[task.ID] {
				t.Errorf("%s list included unexpected task %q (type=%q kind=%q)", tc.kind, task.ID, task.Type, task.WorkbenchKind)
			}
			delete(tc.want, task.ID)
		}
		if len(tc.want) != 0 {
			t.Errorf("%s list omitted expected task IDs: %#v", tc.kind, tc.want)
		}
	}
}
