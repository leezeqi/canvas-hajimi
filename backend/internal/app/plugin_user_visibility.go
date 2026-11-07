package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"yingce/backend/internal/model"
)

// 插件“对用户开放”开关（二改）。管理员在插件管理里把协议插件设为不开放后，普通用户的插件中心、
// 插件状态、自定义渠道协议目录和插件包下载都不返回它，自定义渠道也不能使用它的协议；管理员照常查看、
// 配置和调用，管理员配置好的系统渠道模型对所有人可用。平台停用仍然对所有人生效。

// pluginUserVisibilityConfigurable 只放开协议插件：应用插件没有系统渠道这条路，隐藏就等于停用；
// 支付和短信插件本来就不出现在普通用户面前。
func pluginUserVisibilityConfigurable(plugin PluginView) bool {
	return pluginManagementFromView(plugin).Kind == PluginKindProtocol
}

func (s *Service) hiddenPluginIDs() (map[string]bool, error) {
	if s.repo == nil {
		return map[string]bool{}, nil
	}
	hidden, err := s.repo.HiddenPluginIDs()
	if err != nil {
		return nil, fmt.Errorf("读取插件用户可见性：%w", err)
	}
	return hidden, nil
}

// pluginsHiddenFrom 返回需要对该用户隐藏的插件；管理员不隐藏任何插件。
func (s *Service) pluginsHiddenFrom(actor *model.User) (map[string]bool, error) {
	if actor != nil && actor.Role == model.UserRoleAdmin {
		return map[string]bool{}, nil
	}
	return s.hiddenPluginIDs()
}

// PluginProviderCatalogForUser 是自定义渠道使用的协议目录，不返回对该用户隐藏的插件。
func (s *Service) PluginProviderCatalogForUser(actor *model.User, scope, capability string, includeUnavailable bool) ([]PluginProviderCatalogItem, error) {
	hidden, err := s.pluginsHiddenFrom(actor)
	if err != nil {
		return nil, err
	}
	return s.pluginProviderCatalog(scope, capability, includeUnavailable, hidden), nil
}

func (s *Service) SetPluginUserVisibility(actor *model.User, pluginID string, visible bool) (AdminPluginStateView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AdminPluginStateView{}, err
	}
	pluginID = strings.TrimSpace(pluginID)
	plugin, ok := runtimePluginByID(s.Plugins(), pluginID)
	if !ok {
		return AdminPluginStateView{}, NotFound(fmt.Sprintf("插件 %q 不存在", pluginID))
	}
	if !pluginUserVisibilityConfigurable(plugin) {
		return AdminPluginStateView{}, NewAppError(http.StatusBadRequest, "只有协议插件可以设置是否对用户开放")
	}
	if s.repo == nil {
		return AdminPluginStateView{}, fmt.Errorf("插件状态存储未初始化")
	}
	now := time.Now()
	state := &model.PluginUserVisibility{PluginID: pluginID, HiddenFromUsers: !visible, UpdatedBy: actor.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.SavePluginUserVisibility(state); err != nil {
		return AdminPluginStateView{}, fmt.Errorf("保存插件用户可见性：%w", err)
	}
	if err := s.appendAdminAudit(actor, "plugin.visibility.update", "plugin", pluginID, "更新插件是否对用户开放", map[string]any{"visibleToUsers": visible}); err != nil {
		return AdminPluginStateView{}, err
	}
	states, err := s.AdminPluginStates(actor)
	if err != nil {
		return AdminPluginStateView{}, err
	}
	return states[pluginID], nil
}

// protocolOwnerPluginID 找到贡献该协议的插件。协议 ID 可以和插件 ID 不同（上传插件、一个插件声明多个
// Provider 时都会出现），所以先按 Provider 贡献查找，再退回同名插件。
func (s *Service) protocolOwnerPluginID(protocolID string) (string, bool) {
	protocolID = strings.TrimSpace(protocolID)
	if protocolID == "" {
		return "", false
	}
	candidates := []string{protocolID}
	if canonical, ok := s.canonicalProtocolID(protocolID); ok && canonical != protocolID {
		candidates = append(candidates, canonical)
	}
	plugins := s.Plugins()
	for _, plugin := range plugins {
		for _, provider := range plugin.Manifest.Contributes.Providers {
			for _, candidate := range candidates {
				if provider.ID == candidate {
					return plugin.Manifest.ID, true
				}
			}
		}
	}
	for _, plugin := range plugins {
		for _, candidate := range candidates {
			if plugin.Manifest.ID == candidate {
				return plugin.Manifest.ID, true
			}
		}
	}
	return "", false
}

// requirePluginVisibleToUser 拒绝普通用户直接使用未对用户开放的插件。
func (s *Service) requirePluginVisibleToUser(userID string, pluginID string) error {
	// 应用、支付和短信插件不能设置对用户开放，不必查询（Eagle 等应用插件每个请求都会走这里）。
	if pluginManagement(pluginID, "").Kind != PluginKindProtocol {
		return nil
	}
	hidden, err := s.hiddenPluginIDs()
	if err != nil {
		return err
	}
	if !hidden[strings.TrimSpace(pluginID)] {
		return nil
	}
	user, err := s.repo.User(strings.TrimSpace(userID))
	if err != nil {
		return fmt.Errorf("读取用户权限：%w", err)
	}
	if user.Role != model.UserRoleAdmin {
		return Forbidden("该插件未对普通用户开放")
	}
	return nil
}

// requireProviderProtocolPlugin 校验生成任务所用协议的所属插件，config 必须已经过 resolveProviderConfig。
// 插件对用户开放时不额外拦截。不开放时：系统渠道的协议由 resolveProviderConfig 按渠道模型覆盖，客户端无法指定，
// 因此只要求插件仍由平台启用；自定义渠道的协议来自客户端，按用户角色拦截，避免绕过界面直接使用隐藏插件。
func (s *Service) requireProviderProtocolPlugin(userID string, config providerConfig) error {
	pluginID, ok := s.protocolOwnerPluginID(config.InterfaceType)
	if !ok {
		return nil
	}
	hidden, err := s.hiddenPluginIDs()
	if err != nil {
		return err
	}
	if !hidden[pluginID] {
		return nil
	}
	if strings.TrimSpace(config.ChannelID) != "" {
		return s.requirePluginEffectiveForUser(userID, pluginID)
	}
	return s.RequirePluginForUser(userID, pluginID)
}
