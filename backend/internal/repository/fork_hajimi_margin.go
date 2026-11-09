// hajimi 互通毛利报表（fork 专有）的只读查询。

package repository

import (
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

// HajimiMarginOrderStatuses 是毛利报表关心的订单状态：已结算计入收入，其余三种是尚未结束的订单。
var HajimiMarginOrderStatuses = []model.BillingStatus{
	model.BillingStatusSettled,
	model.BillingStatusReserved,
	model.BillingStatusRunning,
	model.BillingStatusUncertain,
}

// VisitHajimiMarginOrders 按批读取 created_at 落在 [from, to) 附近的订单；SQLite 按文本比较时间，
// 调用方需要再按 CreatedAt 精确过滤，所以这里查询窗口两端各放宽一天。
func (r *Repository) VisitHajimiMarginOrders(from time.Time, to time.Time, visit func([]model.BillingOrder) error) error {
	var batch []model.BillingOrder
	return r.db.Model(&model.BillingOrder{}).
		Where("created_at >= ? AND created_at < ? AND status IN ?", from.Add(-24*time.Hour).UTC(), to.Add(24*time.Hour).UTC(), HajimiMarginOrderStatuses).
		FindInBatches(&batch, 500, func(_ *gorm.DB, _ int) error { return visit(batch) }).Error
}

// HajimiMarginChannels 返回全部渠道（含已删除）的识别所需字段，不读取任何密钥列。
func (r *Repository) HajimiMarginChannels() ([]model.ModelChannel, error) {
	var channels []model.ModelChannel
	err := r.db.Unscoped().Model(&model.ModelChannel{}).
		Select("id", "name", "base_url", "scope", "deleted_at").
		Order("created_at asc").Find(&channels).Error
	return channels, err
}
