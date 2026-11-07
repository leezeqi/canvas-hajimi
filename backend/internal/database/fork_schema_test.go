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
