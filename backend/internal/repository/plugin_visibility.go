package repository

import (
	"yingce/backend/internal/model"

	"gorm.io/gorm/clause"
)

// HiddenPluginIDs 返回对普通用户隐藏的插件 ID 集合（二改）；没有记录的插件对用户开放。
func (r *Repository) HiddenPluginIDs() (map[string]bool, error) {
	var ids []string
	if err := r.db.Model(&model.PluginUserVisibility{}).Where("hidden_from_users = ?", true).Pluck("plugin_id", &ids).Error; err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result, nil
}

func (r *Repository) SavePluginUserVisibility(state *model.PluginUserVisibility) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "plugin_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"hidden_from_users", "updated_by", "updated_at"}),
	}).Create(state).Error
}

func (r *Repository) DeletePluginUserVisibility(pluginID string) error {
	return r.db.Delete(&model.PluginUserVisibility{}, "plugin_id = ?", pluginID).Error
}
