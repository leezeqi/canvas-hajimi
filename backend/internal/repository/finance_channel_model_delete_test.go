package repository

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeleteChannelModelsArchivedReferences(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { testDeleteChannelModelsArchivedReferences(t, driver) })
	}
}

func testDeleteChannelModelsArchivedReferences(t *testing.T, driver string) {
	for _, tc := range []struct {
		name       string
		archived   bool
		enabled    bool
		taskStatus model.TaskStatus
		blocked    bool
	}{
		{name: "archived history is retained", archived: true, enabled: true, taskStatus: model.TaskStatusSucceeded},
		{name: "unarchived model blocks deletion", enabled: true, blocked: true},
		{name: "disabled unarchived model still blocks deletion", blocked: true},
		{name: "queued task still blocks deletion", archived: true, taskStatus: model.TaskStatusQueued, blocked: true},
		{name: "running task still blocks deletion", archived: true, taskStatus: model.TaskStatusRunning, blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newChannelModelDeleteTestDB(t, driver)
			if err := db.AutoMigrate(&model.ModelChannel{}, &model.ChannelModel{}, &model.LogicalModel{}, &model.LogicalModelRevision{}, &model.LogicalModelRoute{}, &model.Task{}); err != nil {
				t.Fatal(err)
			}
			repo := New(db)
			now := time.Now()
			channel := model.ModelChannel{ID: "channel", Scope: model.ChannelScopeSystem, ModelsJSON: `["used","unused"]`}
			items := []model.ChannelModel{
				{ID: "used", ChannelID: channel.ID, ModelKey: "used", Enabled: true, PriceVersion: 1},
				{ID: "unused", ChannelID: channel.ID, ModelKey: "unused", Enabled: true, PriceVersion: 1},
			}
			logical := model.LogicalModel{ID: "logical", Code: "logical", Enabled: tc.enabled, ActiveRevisionID: "revision", CreatedAt: now, UpdatedAt: now}
			revision := model.LogicalModelRevision{ID: "revision", LogicalModelID: logical.ID, Version: 1, CapabilitySpecJSON: `{"version":1,"capability":"text"}`, DefaultOptionsJSON: `{"temperature":0.7}`, CreatedAt: now}
			route := model.LogicalModelRoute{ID: "route", LogicalModelRevisionID: revision.ID, ChannelModelID: "used", Enabled: true, Weight: 100, CreatedAt: now, UpdatedAt: now}
			for _, value := range []any{&channel, &items, &logical, &revision, &route} {
				if err := db.Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tc.archived {
				if err := repo.ArchiveLogicalModel(logical.ID, nil, now); err != nil {
					t.Fatal(err)
				}
			}
			if tc.taskStatus != "" {
				task := model.Task{ID: "task", ChannelModelID: "used", Status: tc.taskStatus, LogicalModelID: logical.ID, LogicalModelRevisionID: revision.ID, RouteID: route.ID, CreatedAt: now, UpdatedAt: now}
				if err := db.Create(&task).Error; err != nil {
					t.Fatal(err)
				}
			}

			deleted, err := repo.DeleteChannelModels(channel.ID, []string{"used", "unused"}, now)
			if tc.blocked {
				if !errors.Is(err, ErrChannelModelInUse) || deleted != 0 {
					t.Fatalf("delete = %d, %v; want no deletion and reference error", deleted, err)
				}
			} else if err != nil || deleted != 2 {
				t.Fatalf("delete = %d, %v; want both models deleted", deleted, err)
			}
			var saved []model.ChannelModel
			if err := db.Unscoped().Order("id").Find(&saved).Error; err != nil {
				t.Fatal(err)
			}
			if len(saved) != 2 {
				t.Fatalf("channel model history count = %d, want 2", len(saved))
			}
			for _, item := range saved {
				if item.DeletedAt.Valid == tc.blocked || item.Enabled != tc.blocked {
					t.Fatalf("unexpected channel model state: %#v", item)
				}
			}
			var savedLogical model.LogicalModel
			var savedRevision model.LogicalModelRevision
			var savedRoute model.LogicalModelRoute
			for _, value := range []any{&savedLogical, &savedRevision, &savedRoute} {
				if err := db.First(value).Error; err != nil {
					t.Fatalf("history lookup: %v", err)
				}
			}
			if (savedLogical.ArchivedAt != nil) != tc.archived || savedLogical.ActiveRevisionID != revision.ID ||
				savedRevision.ID != revision.ID || savedRevision.LogicalModelID != logical.ID || savedRevision.Version != revision.Version ||
				savedRevision.CapabilitySpecJSON != revision.CapabilitySpecJSON || savedRevision.DefaultOptionsJSON != revision.DefaultOptionsJSON ||
				savedRoute.ID != route.ID || savedRoute.LogicalModelRevisionID != revision.ID || savedRoute.ChannelModelID != "used" ||
				savedRoute.Enabled != route.Enabled || savedRoute.Weight != route.Weight {
				t.Fatal("logical model, revision or route history was changed")
			}
			if tc.taskStatus != "" {
				var savedTask model.Task
				if err := db.First(&savedTask, "id = ?", "task").Error; err != nil {
					t.Fatal(err)
				}
				if savedTask.Status != tc.taskStatus || savedTask.RouteID != route.ID || savedTask.ChannelModelID != "used" {
					t.Fatal("task history was changed")
				}
			}
		})
	}
}

func newChannelModelDeleteTestDB(t *testing.T, driver string) *gorm.DB {
	t.Helper()
	config := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var dialector gorm.Dialector = sqlite.Open(filepath.Join(t.TempDir(), "delete.db"))
	if driver == "postgres" {
		dsn := os.Getenv("CANVAS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set CANVAS_TEST_POSTGRES_DSN to an isolated test database")
		}
		admin, err := gorm.Open(postgres.Open(dsn), config)
		if err != nil {
			t.Fatal(err)
		}
		schema := fmt.Sprintf("channel_delete_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
				t.Error(err)
			}
			connection, _ := admin.DB()
			_ = connection.Close()
		})
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dialector = postgres.Open(parsed.String())
	}
	db, err := gorm.Open(dialector, config)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return db
}
