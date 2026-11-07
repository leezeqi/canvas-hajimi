package app

import (
	"sort"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/outbound/outboundtest"
	"yingce/backend/internal/protocol"
)

// 仅管理员可见只隐藏插件，不禁止普通用户使用管理员配好的系统渠道。用真实内置插件包和系统渠道模型，
// 按生成任务的顺序（解析渠道配置 -> 仅管理员可见拦截 -> 协议校验）逐个验证名单内的插件。
func TestAdminOnlyPluginsStayCallableThroughSystemChannelsWithBundledPackages(t *testing.T) {
	outboundtest.PublicDNS(t, "upstream.example.com")
	svc, db := newChannelModelTestService(t)
	if err := db.AutoMigrate(&model.PluginPlatformState{}, &model.UserPluginState{}); err != nil {
		t.Fatal(err)
	}
	svc.dataDir = t.TempDir()
	runtime, err := newPluginRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.pluginRuntime = runtime
	ordinary := &model.User{ID: "user-1", Username: "user-1", Role: model.UserRoleUser}
	admin := &model.User{ID: "admin-1", Username: "admin-1", Role: model.UserRoleAdmin}
	for _, user := range []*model.User{ordinary, admin} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}

	pluginIDs := make([]string, 0, len(adminOnlyPlugins))
	for pluginID := range adminOnlyPlugins {
		pluginIDs = append(pluginIDs, pluginID)
	}
	sort.Strings(pluginIDs)
	for _, pluginID := range pluginIDs {
		t.Run(pluginID, func(t *testing.T) {
			if _, ok := svc.protocolRegistry().Resolve(pluginID); !ok {
				t.Fatalf("bundled package is not registered under %q; its Provider ID must equal the plugin ID", pluginID)
			}
			channel := model.ModelChannel{
				ID: "channel-" + pluginID, Scope: model.ChannelScopeSystem, Enabled: true, Name: pluginID,
				BaseURL: "https://upstream.example.com", APIKey: "system-key", APIFormat: "openai",
			}
			if err := svc.encryptSystemChannelSecrets(&channel); err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&channel).Error; err != nil {
				t.Fatal(err)
			}
			item := model.ChannelModel{
				ID: "model-" + pluginID, ChannelID: channel.ID, ModelKey: "video-model", ProviderModelKey: "video-model",
				Capability: "video", Protocol: model.ChannelInterfaceType(pluginID), Enabled: true,
			}
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}

			// 客户端传入的协议会被渠道模型协议覆盖，普通用户不能借系统渠道指定其他协议。
			config, err := svc.resolveProviderConfig(providerConfig{ChannelID: channel.ID, Model: item.ModelKey, InterfaceType: "openai"})
			if err != nil {
				t.Fatal(err)
			}
			if config.InterfaceType != pluginID || config.APIKey != "system-key" {
				t.Fatalf("resolved system channel config = %#v", config)
			}
			if err := svc.requireProviderProtocolPlugin(ordinary.ID, config); err != nil {
				t.Fatalf("ordinary user calling the admin-configured system channel error = %v", err)
			}
			if err := svc.validateGenerationInterface("video", config.InterfaceType); err != nil {
				t.Fatalf("system channel protocol validation error = %v", err)
			}

			custom, err := svc.resolveProviderConfig(providerConfig{BaseURL: "https://upstream.example.com", APIKey: "user-key", Model: "video-model", InterfaceType: pluginID})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.requireProviderProtocolPlugin(ordinary.ID, custom); err == nil {
				t.Fatal("ordinary user used the admin-only plugin through a custom channel")
			}
			if err := svc.requireProviderProtocolPlugin(admin.ID, custom); err != nil {
				t.Fatalf("admin custom channel error = %v", err)
			}
		})
	}

	ordinaryStates, err := svc.PluginStatesForUser(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	adminStates, err := svc.PluginStatesForUser(admin)
	if err != nil {
		t.Fatal(err)
	}
	userCatalog := svc.PluginProviderCatalogForUser(ordinary, string(protocol.SurfaceUserCustomChannel), string(protocol.CapabilityVideo), true)
	for _, pluginID := range pluginIDs {
		if _, visible := ordinaryStates[pluginID]; visible {
			t.Errorf("%s state was visible to an ordinary user", pluginID)
		}
		if state, visible := adminStates[pluginID]; !visible || !state.EffectiveEnabled {
			t.Errorf("%s admin state = %#v, visible=%v; bundled package should be enabled by default", pluginID, state, visible)
		}
		for _, item := range userCatalog {
			if item.ID == pluginID {
				t.Errorf("%s was in the ordinary user's custom channel catalog", pluginID)
			}
		}
	}
}
