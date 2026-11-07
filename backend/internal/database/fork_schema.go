package database

import (
	"errors"
	"fmt"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

// 本仓库（canvas-hajimi）是上游 open-ai-canvas 的二改。迁移版本号只跟随上游，二改的表结构变化不进
// schemaMigrations 账本，统一在这里做幂等补齐；否则二改占用的版本号会和上游后续迁移撞号，
// 部署时报"名称不一致/校验和不一致"拒绝启动。见 docs/upstream-sync-methodology.md。

// 二改曾把作品删除列登记为迁移 79，与上游 79（ecommerce_agent_and_knowledge）冲突。
const (
	forkTaskUserDeletionVersion  int64 = 79
	forkTaskUserDeletionName           = "task_user_deletion"
	forkTaskUserDeletionChecksum       = "sha256:task-user-deletion-v79-20261105"
)

// releaseForkMigrationVersions 删除二改旧版本占用的账本记录，让上游同号迁移正常执行。
// 只删除名称和校验和都精确匹配、且之后没有其他迁移记录的行；对应的列保留，由 applyForkSchema 继续维护。
func releaseForkMigrationVersions(tx *gorm.DB) error {
	var record schemaMigration
	err := tx.First(&record, "version = ?", forkTaskUserDeletionVersion).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取数据库迁移 %d：%w", forkTaskUserDeletionVersion, err)
	}
	if record.Name != forkTaskUserDeletionName || record.Checksum != forkTaskUserDeletionChecksum {
		return nil
	}
	var later schemaMigration
	err = tx.Where("version > ?", forkTaskUserDeletionVersion).Order("version asc").First(&later).Error
	if err == nil {
		return fmt.Errorf("数据库迁移 %d 是二改旧记录 %s，但已存在后续迁移记录 %d；拒绝自动改写迁移账本", forkTaskUserDeletionVersion, forkTaskUserDeletionName, later.Version)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("读取数据库迁移 %d 之后的记录：%w", forkTaskUserDeletionVersion, err)
	}
	result := tx.Where("version = ? AND name = ? AND checksum = ?", forkTaskUserDeletionVersion, forkTaskUserDeletionName, forkTaskUserDeletionChecksum).Delete(&schemaMigration{})
	if result.Error != nil {
		return fmt.Errorf("释放二改迁移记录 %d：%w", forkTaskUserDeletionVersion, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("释放二改迁移记录 %d 影响 %d 行，拒绝继续迁移", forkTaskUserDeletionVersion, result.RowsAffected)
	}
	return nil
}

// applyForkSchema 幂等补齐二改需要的列，在上游迁移全部执行后调用。
func applyForkSchema(tx *gorm.DB) error {
	// tasks.user_deleted_at：用户从作品列表删除终态任务；旧任务为 NULL，仍在列表中可见。
	if tx.Migrator().HasTable(&model.Task{}) && !tx.Migrator().HasColumn(&model.Task{}, "UserDeletedAt") {
		if err := tx.Migrator().AddColumn(&model.Task{}, "UserDeletedAt"); err != nil {
			return fmt.Errorf("补齐二改任务删除列：%w", err)
		}
	}
	return nil
}
