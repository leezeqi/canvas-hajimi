package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func assertForkHajimiTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if ready, err := ForkHajimiSchemaReady(db); err != nil || !ready {
		t.Fatalf("fork schema ready = %v %v", ready, err)
	}
	if !db.Migrator().HasIndex(&model.HajimiBridgeTransfer{}, "idx_hajimi_bridge_transfers_user_client_key") {
		t.Fatal("missing unique (user_id, client_key) index")
	}
	// 上游版本校验不受二改表影响。
	if err := RequireSchemaVersion(db); err != nil {
		t.Fatalf("upstream version check: %v", err)
	}
	status, err := ReadSchemaStatus(db)
	if err != nil || !status.Ready || status.Current != CurrentSchemaVersion {
		t.Fatalf("schema status = %#v err=%v", status, err)
	}
}

func assertForkHajimiSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	// 新库：基线迁移按 Models() 建表。
	if err := MigrateSchema(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertForkHajimiTables(t, db)

	key := "client-key"
	now := time.Now()
	first := model.HajimiBridgeTransfer{ID: "yct_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: "u1", HajimiUserID: 1, Direction: "in", AmountMicrocredits: 1, ClientKey: &key, Source: "manual", Status: "pending", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := first
	duplicate.ID = "yct_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate (user_id, client_key) must be rejected")
	}
	// client_key 为空的记录（游标补登）不受唯一约束限制。
	for _, id := range []string{"yct_cccccccccccccccccccccccccccccccc", "yct_dddddddddddddddddddddddddddddddd"} {
		row := model.HajimiBridgeTransfer{ID: id, UserID: "u1", HajimiUserID: 1, Direction: "in", AmountMicrocredits: 1, Source: "reconcile", Status: "attention", CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("null client_key rows: %v", err)
		}
	}

	// 已有数据时重复迁移不动表、不丢数据。
	if err := MigrateSchema(db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	var count int64
	if err := db.Model(&model.HajimiBridgeTransfer{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("rows after re-migrate = %d err=%v", count, err)
	}

	// 旧库（上游迁移已执行、还没有这四张表）：下一次迁移由 applyForkSchema 补建。
	for _, item := range forkHajimiBridgeModels() {
		if err := db.Migrator().DropTable(item); err != nil {
			t.Fatal(err)
		}
	}
	if ready, err := ForkHajimiSchemaReady(db); err != nil || ready {
		t.Fatalf("ready without tables = %v %v", ready, err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatalf("migrate existing database: %v", err)
	}
	assertForkHajimiTables(t, db)
}

func TestForkHajimiSchemaSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fork.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	assertForkHajimiSchema(t, db)
}

func TestForkHajimiSchemaPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("CANVAS_TEST_POSTGRES_DSN is not configured")
	}
	base, err := Open(Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	baseSQL, _ := base.DB()
	defer baseSQL.Close()
	schemaName := fmt.Sprintf("fork_hajimi_schema_%d", time.Now().UnixNano())
	if err := base.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatal(err)
	}
	defer base.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`)
	testDSN, err := postgresDSNWithSearchPath(dsn, schemaName)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(Config{Driver: "postgres", DSN: testDSN})
	if err != nil {
		t.Fatal(err)
	}
	dbSQL, _ := db.DB()
	defer dbSQL.Close()
	assertForkHajimiSchema(t, db)
}

func TestModelsIncludeForkHajimiBridgeTables(t *testing.T) {
	want := map[string]bool{}
	for _, item := range forkHajimiBridgeModels() {
		want[fmt.Sprintf("%T", item)] = false
	}
	for _, item := range Models() {
		if _, ok := want[fmt.Sprintf("%T", item)]; ok {
			want[fmt.Sprintf("%T", item)] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("Models() must include %s", name)
		}
	}
}
