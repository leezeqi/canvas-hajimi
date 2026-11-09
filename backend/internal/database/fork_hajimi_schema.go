package database

import (
	"fmt"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

// hajimi 余额互通（二改）的四张表。按 docs/upstream-sync-methodology.md 的约定不登记迁移版本：
// 新库由基线迁移按 Models() 建表，旧库由 applyForkSchema 在上游迁移之后调用这里幂等补建。

func forkHajimiBridgeModels() []any {
	return []any{&model.HajimiBridgeTransfer{}, &model.HajimiBridgeUserSettings{}, &model.HajimiBridgeUserState{}, &model.HajimiBridgeState{}}
}

// applyForkHajimiBridgeSchema 只建缺失的表（连同模型声明的索引），已有的表不改动。
func applyForkHajimiBridgeSchema(tx *gorm.DB) error {
	for _, item := range forkHajimiBridgeModels() {
		if tx.Migrator().HasTable(item) {
			continue
		}
		if err := tx.Migrator().CreateTable(item); err != nil {
			return fmt.Errorf("创建 hajimi 互通表 %T：%w", item, err)
		}
	}
	return nil
}

// EnsureForkHajimiBridgeTables 供只建部分表的测试库使用；生产路径由 MigrateSchema 负责。
func EnsureForkHajimiBridgeTables(db *gorm.DB) error {
	return db.Transaction(applyForkHajimiBridgeSchema)
}

// ForkHajimiSchemaReady 只读检查 hajimi 互通表是否都已建好；未迁移时互通功能保持关闭。
func ForkHajimiSchemaReady(db *gorm.DB) (bool, error) {
	for _, item := range forkHajimiBridgeModels() {
		if !db.Migrator().HasTable(item) {
			return false, nil
		}
	}
	return true, nil
}
