package database

import (
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

func openForkSchemaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(Config{Driver: "sqlite", DSN: "file:" + t.Name() + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// 模拟二改旧版本：上游迁移执行到 78，二改把作品删除列登记成了迁移 79。
func forkV79Database(t *testing.T) *gorm.DB {
	t.Helper()
	db := openForkSchemaTestDB(t)
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatal(err)
	}
	for _, item := range schemaMigrations {
		if item.version >= forkTaskUserDeletionVersion {
			break
		}
		if err := item.apply(db); err != nil {
			t.Fatalf("apply migration %d: %v", item.version, err)
		}
		if err := db.Create(&schemaMigration{Version: item.version, Name: item.name, Checksum: item.checksum, AppliedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if !db.Migrator().HasColumn(&model.Task{}, "UserDeletedAt") {
		if err := db.Migrator().AddColumn(&model.Task{}, "UserDeletedAt"); err != nil {
			t.Fatal(err)
		}
	}
	record := schemaMigration{Version: forkTaskUserDeletionVersion, Name: forkTaskUserDeletionName, Checksum: forkTaskUserDeletionChecksum, AppliedAt: time.Now().UTC()}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateSchemaFreshDatabaseHasForkTaskDeletionColumn(t *testing.T) {
	db := openForkSchemaTestDB(t)
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(&model.Task{}, "UserDeletedAt") {
		t.Fatal("fresh database is missing tasks.user_deleted_at")
	}
	if err := RequireSchemaVersion(db); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateSchemaReleasesForkV79AndAppliesUpstreamV79(t *testing.T) {
	db := forkV79Database(t)
	if err := RequireSchemaVersion(db); err == nil || !strings.Contains(err.Error(), "过旧") {
		t.Fatalf("fork v79 database must require migrate-schema up before serving, got %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := MigrateSchema(db); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	var v79 schemaMigration
	if err := db.First(&v79, "version = ?", forkTaskUserDeletionVersion).Error; err != nil {
		t.Fatal(err)
	}
	if v79.Name == forkTaskUserDeletionName {
		t.Fatalf("migration 79 still records the fork entry: %+v", v79)
	}
	if !db.Migrator().HasColumn(&model.Task{}, "UserDeletedAt") {
		t.Fatal("upgrade dropped tasks.user_deleted_at")
	}
	if err := RequireSchemaVersion(db); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseForkMigrationRefusesWhenLaterRecordsExist(t *testing.T) {
	db := forkV79Database(t)
	later := schemaMigration{Version: forkTaskUserDeletionVersion + 1, Name: "unexpected", Checksum: "sha256:unexpected", AppliedAt: time.Now().UTC()}
	if err := db.Create(&later).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err == nil || !strings.Contains(err.Error(), "拒绝自动改写迁移账本") {
		t.Fatalf("expected refusal when later records exist, got %v", err)
	}
	var count int64
	if err := db.Model(&schemaMigration{}).Where("version = ? AND name = ?", forkTaskUserDeletionVersion, forkTaskUserDeletionName).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("refused migration must keep the fork record, found %d", count)
	}
}

func hiddenPluginRows(t *testing.T, db *gorm.DB) map[string]bool {
	t.Helper()
	var rows []model.PluginUserVisibility
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	result := make(map[string]bool, len(rows))
	for _, row := range rows {
		result[row.PluginID] = row.HiddenFromUsers
	}
	return result
}

// 新库由基线迁移建表，所有插件默认对用户开放；旧库首次建表时把原先写死的仅管理员可见插件写成隐藏，
// 之后重复迁移不覆盖管理员的修改。
func TestMigrateSchemaSeedsLegacyAdminOnlyPluginsOnlyForExistingDatabases(t *testing.T) {
	db := openForkSchemaTestDB(t)
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if rows := hiddenPluginRows(t, db); len(rows) != 0 {
		t.Fatalf("fresh database must open every plugin to users, got %v", rows)
	}

	// 模拟升级前的旧库：基线迁移早已执行，表还不存在。
	if err := db.Migrator().DropTable(&model.PluginUserVisibility{}); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	rows := hiddenPluginRows(t, db)
	if len(rows) != len(forkLegacyAdminOnlyPlugins) {
		t.Fatalf("legacy database rows = %v, want %v hidden", rows, forkLegacyAdminOnlyPlugins)
	}
	for _, pluginID := range forkLegacyAdminOnlyPlugins {
		if !rows[pluginID] {
			t.Errorf("%s was not hidden after upgrading a legacy database", pluginID)
		}
	}

	if err := db.Model(&model.PluginUserVisibility{}).Where("plugin_id = ?", "aotian-video").Update("hidden_from_users", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if rows := hiddenPluginRows(t, db); rows["aotian-video"] || len(rows) != len(forkLegacyAdminOnlyPlugins) {
		t.Fatalf("repeated migration changed administrator choices: %v", rows)
	}
	if err := RequireSchemaVersion(db); err != nil {
		t.Fatal(err)
	}
}
