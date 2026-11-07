package model

import "time"

// PluginUserVisibility 记录协议插件是否对普通用户开放（二改）。没有记录即对用户开放；
// 隐藏只影响普通用户能否看到插件、能否在自定义渠道里使用，不影响管理员配置好的系统渠道。
type PluginUserVisibility struct {
	PluginID        string    `json:"pluginId" gorm:"primaryKey;size:120"`
	HiddenFromUsers bool      `json:"hiddenFromUsers" gorm:"index"`
	UpdatedBy       string    `json:"updatedBy" gorm:"index;size:36"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
