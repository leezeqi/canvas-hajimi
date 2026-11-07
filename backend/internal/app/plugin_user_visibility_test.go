package app

import (
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/outbound/outboundtest"
	"yingce/backend/internal/protocol"

	"gorm.io/gorm"
)

func newPluginUserVisibilityTestService(t *testing.T) (*Service, *gorm.DB, *model.User, *model.User) {
	t.Helper()
	svc, db := newFeatureAvailabilityTestService(t)
	if err := db.AutoMigrate(&model.User{}, &model.PluginPlatformState{}, &model.UserPluginState{}); err != nil {
		t.Fatal(err)
	}
	ordinary := &model.User{ID: "user-1", Username: "user-1", Role: model.UserRoleUser}
	admin := &model.User{ID: "admin-1", Username: "admin-1", Role: model.UserRoleAdmin}
	for _, user := range []*model.User{ordinary, admin} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	return svc, db, ordinary, admin
}

func pluginIDs(items []PluginView) map[string]bool {
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		ids[item.Manifest.ID] = true
	}
	return ids
}

// 默认对用户开放；管理员关闭后只对普通用户隐藏，管理员仍能看到，状态里带回 userVisible。
func TestPluginUserVisibilityHidesProtocolPluginsOnlyFromOrdinaryUsers(t *testing.T) {
	svc, db, ordinary, admin := newPluginUserVisibilityTestService(t)
	if _, err := svc.UpdateFeatureAvailability(admin, FeatureAvailability{
		ShortDramaEnabled: true, TaskCenterEnabled: true, CreditsEnabled: true,
		CustomChannelsEnabled: true, PluginCenterEnabled: true, SystemPluginsVisibleToUsers: true,
	}); err != nil {
		t.Fatal(err)
	}
	svc.pluginRuntime = &pluginRuntime{plugins: map[string]pluginRecord{
		"bundled":  {Source: "bundled", Status: "enabled", Metadata: protocol.Metadata{ID: "bundled", Name: "系统协议", Version: "1"}},
		"uploaded": {Source: "uploaded", Status: "enabled", Metadata: protocol.Metadata{ID: "uploaded", Name: "自定义协议", Version: "1"}},
		"app":      {Source: "bundled", Status: "enabled", Metadata: protocol.Metadata{ID: WorkflowPluginRunningHub, Name: "官方应用", Version: "1"}},
	}}

	visible, err := svc.PluginsForUser(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if ids := pluginIDs(visible); !ids["bundled"] || !ids["uploaded"] || !ids[WorkflowPluginRunningHub] {
		t.Fatalf("plugins are open to users by default, got %v", ids)
	}

	for _, pluginID := range []string{"bundled", "uploaded"} {
		state, err := svc.SetPluginUserVisibility(admin, pluginID, false)
		if err != nil {
			t.Fatalf("hide %s: %v", pluginID, err)
		}
		if state.UserVisible || state.PluginID != pluginID {
			t.Fatalf("hide %s state = %#v", pluginID, state)
		}
	}
	visible, err = svc.PluginsForUser(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if ids := pluginIDs(visible); ids["bundled"] || ids["uploaded"] || !ids[WorkflowPluginRunningHub] {
		t.Fatalf("ordinary user plugins after hiding = %v", ids)
	}
	adminVisible, err := svc.PluginsForUser(admin)
	if err != nil {
		t.Fatal(err)
	}
	if ids := pluginIDs(adminVisible); !ids["bundled"] || !ids["uploaded"] {
		t.Fatalf("administrator plugins after hiding = %v", ids)
	}
	ordinaryStates, err := svc.PluginStatesForUser(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := ordinaryStates["bundled"]; exists {
		t.Fatal("hidden plugin state was returned to an ordinary user")
	}
	adminStates, err := svc.AdminPluginStates(admin)
	if err != nil {
		t.Fatal(err)
	}
	if state := adminStates["bundled"]; state.UserVisible || !state.PlatformAvailable {
		t.Fatalf("administrator state for hidden plugin = %#v", state)
	}
	if !adminStates[WorkflowPluginRunningHub].UserVisible {
		t.Fatal("application plugin must stay open to users")
	}

	if _, err := svc.SetPluginUserVisibility(admin, "bundled", true); err != nil {
		t.Fatal(err)
	}
	visible, err = svc.PluginsForUser(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if !pluginIDs(visible)["bundled"] {
		t.Fatal("reopened plugin is still hidden from ordinary users")
	}

	var auditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ?", "plugin.visibility.update").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("visibility audit count = %d, want 3", auditCount)
	}
}

func TestSetPluginUserVisibilityRejectsNonAdminsAndNonProtocolPlugins(t *testing.T) {
	svc, _, ordinary, admin := newPluginUserVisibilityTestService(t)
	svc.pluginRuntime = &pluginRuntime{plugins: map[string]pluginRecord{
		"bundled": {Source: "bundled", Status: "enabled", Metadata: protocol.Metadata{ID: "bundled", Name: "系统协议", Version: "1"}},
		"app":     {Source: "bundled", Status: "enabled", Metadata: protocol.Metadata{ID: WorkflowPluginRunningHub, Name: "官方应用", Version: "1"}},
	}}
	if _, err := svc.SetPluginUserVisibility(ordinary, "bundled", false); err == nil {
		t.Fatal("ordinary user changed plugin visibility")
	}
	if _, err := svc.SetPluginUserVisibility(admin, WorkflowPluginRunningHub, false); err == nil {
		t.Fatal("application plugin visibility must not be configurable")
	}
	if _, err := svc.SetPluginUserVisibility(admin, "missing", false); err == nil {
		t.Fatal("unknown plugin visibility was saved")
	}
	hidden, err := svc.hiddenPluginIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 0 {
		t.Fatalf("rejected requests saved visibility rows: %v", hidden)
	}
}

// 不开放只拦自定义渠道：系统渠道的协议由渠道模型决定，普通用户可以正常调用；平台停用对所有人生效。
func TestProviderProtocolPluginBlocksHiddenPluginsOnlyThroughCustomChannels(t *testing.T) {
	svc, _, ordinary, admin := newPluginUserVisibilityTestService(t)
	svc.pluginRuntime = &pluginRuntime{plugins: map[string]pluginRecord{
		"video-proto": {Source: "bundled", Status: "enabled", Metadata: protocol.Metadata{ID: "video-proto", Name: "视频协议", Version: "1"}},
	}}
	// resolveProviderConfig 只在系统渠道路径写入 ChannelID，并用渠道模型协议覆盖客户端传入值。
	systemChannel := providerConfig{ChannelID: "system-1", InterfaceType: "video-proto"}
	customChannel := providerConfig{BaseURL: "https://example.com", InterfaceType: "video-proto"}

	if err := svc.requireProviderProtocolPlugin(ordinary.ID, customChannel); err != nil {
		t.Fatalf("open plugin through a custom channel error = %v", err)
	}
	if _, err := svc.SetPluginUserVisibility(admin, "video-proto", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.requireProviderProtocolPlugin(ordinary.ID, systemChannel); err != nil {
		t.Fatalf("ordinary user calling an admin-configured system channel error = %v", err)
	}
	if err := svc.requireProviderProtocolPlugin(ordinary.ID, customChannel); err == nil {
		t.Fatal("ordinary user used the hidden plugin through a custom channel")
	}
	if err := svc.RequirePluginForUser(ordinary.ID, "video-proto"); err == nil {
		t.Fatal("ordinary user was allowed to use the hidden plugin directly")
	}
	if err := svc.requireProviderProtocolPlugin(admin.ID, customChannel); err != nil {
		t.Fatalf("admin custom channel error = %v", err)
	}

	if err := svc.repo.SavePluginPlatformState(&model.PluginPlatformState{PluginID: "video-proto", Available: false, UpdatedBy: admin.ID}); err != nil {
		t.Fatal(err)
	}
	if err := svc.requireProviderProtocolPlugin(ordinary.ID, systemChannel); err == nil {
		t.Fatal("system channel kept using a plugin disabled by the platform")
	}
}

// 用真实内置插件包和系统渠道模型，按生成任务的顺序（解析渠道配置 -> 对用户开放拦截 -> 协议校验）验证：
// 包括原先写死的仅管理员可见插件，以及 Provider ID 与插件 ID 不同的插件（按协议反查所属插件）。
func TestHiddenPluginsStayCallableThroughSystemChannelsWithBundledPackages(t *testing.T) {
	outboundtest.PublicDNS(t, "upstream.example.com")
	svc, db := newChannelModelTestService(t)
	if err := db.AutoMigrate(&model.PluginPlatformState{}, &model.UserPluginState{}, &model.PluginUserVisibility{}, &model.AdminAuditEvent{}); err != nil {
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

	cases := []struct{ pluginID, protocolID string }{
		{"aotian-video", "aotian-video"},
		{"juchuang-video", "juchuang-video"},
		{"nineyapi-video", "nineyapi-video"},
		{"nskeven-video", "nskeven-video"},
		{"sd25pp-video", "sd25pp-video"},
		{"uutoken-video", "uutoken-video"},
		{"minimax-hailuo-video-v2", "minimax-video"},
	}
	for _, item := range cases {
		if _, err := svc.SetPluginUserVisibility(admin, item.pluginID, false); err != nil {
			t.Fatalf("hide %s: %v", item.pluginID, err)
		}
	}
	for _, item := range cases {
		t.Run(item.pluginID, func(t *testing.T) {
			if owner, ok := svc.protocolOwnerPluginID(item.protocolID); !ok || owner != item.pluginID {
				t.Fatalf("protocol %q owner = %q, %v; want %q", item.protocolID, owner, ok, item.pluginID)
			}
			channel := model.ModelChannel{
				ID: "channel-" + item.pluginID, Scope: model.ChannelScopeSystem, Enabled: true, Name: item.pluginID,
				BaseURL: "https://upstream.example.com", APIKey: "system-key", APIFormat: "openai",
			}
			if err := svc.encryptSystemChannelSecrets(&channel); err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&channel).Error; err != nil {
				t.Fatal(err)
			}
			channelModel := model.ChannelModel{
				ID: "model-" + item.pluginID, ChannelID: channel.ID, ModelKey: "video-model", ProviderModelKey: "video-model",
				Capability: "video", Protocol: model.ChannelInterfaceType(item.protocolID), Enabled: true,
			}
			if err := db.Create(&channelModel).Error; err != nil {
				t.Fatal(err)
			}

			// 客户端传入的协议会被渠道模型协议覆盖，普通用户不能借系统渠道指定其他协议。
			config, err := svc.resolveProviderConfig(providerConfig{ChannelID: channel.ID, Model: channelModel.ModelKey, InterfaceType: "openai"})
			if err != nil {
				t.Fatal(err)
			}
			if config.InterfaceType != item.protocolID || config.APIKey != "system-key" {
				t.Fatalf("resolved system channel config = %#v", config)
			}
			if err := svc.requireProviderProtocolPlugin(ordinary.ID, config); err != nil {
				t.Fatalf("ordinary user calling the admin-configured system channel error = %v", err)
			}
			if err := svc.validateGenerationInterface("video", config.InterfaceType); err != nil {
				t.Fatalf("system channel protocol validation error = %v", err)
			}

			custom, err := svc.resolveProviderConfig(providerConfig{BaseURL: "https://upstream.example.com", APIKey: "user-key", Model: "video-model", InterfaceType: item.protocolID})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.requireProviderProtocolPlugin(ordinary.ID, custom); err == nil {
				t.Fatal("ordinary user used the hidden plugin through a custom channel")
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
	adminStates, err := svc.AdminPluginStates(admin)
	if err != nil {
		t.Fatal(err)
	}
	userCatalog, err := svc.PluginProviderCatalogForUser(ordinary, string(protocol.SurfaceUserCustomChannel), string(protocol.CapabilityVideo), true)
	if err != nil {
		t.Fatal(err)
	}
	adminCatalog, err := svc.PluginProviderCatalogForUser(admin, string(protocol.SurfaceUserCustomChannel), string(protocol.CapabilityVideo), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range cases {
		if _, visible := ordinaryStates[item.pluginID]; visible {
			t.Errorf("%s state was visible to an ordinary user", item.pluginID)
		}
		if state, visible := adminStates[item.pluginID]; !visible || !state.EffectiveEnabled || state.UserVisible {
			t.Errorf("%s admin state = %#v, visible=%v; bundled package should be enabled and hidden from users", item.pluginID, state, visible)
		}
		inAdminCatalog := false
		for _, provider := range userCatalog {
			if provider.ID == item.protocolID {
				t.Errorf("%s was in the ordinary user's custom channel catalog", item.protocolID)
			}
		}
		for _, provider := range adminCatalog {
			inAdminCatalog = inAdminCatalog || provider.ID == item.protocolID
		}
		if !inAdminCatalog {
			t.Errorf("%s was missing from the administrator catalog", item.protocolID)
		}
	}
}
