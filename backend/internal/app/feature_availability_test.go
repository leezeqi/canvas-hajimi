package app

import (
	"errors"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/platform"
	"yingce/backend/internal/protocol"
	"yingce/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFeatureAvailabilityDefaultsToDisableFrontendModels(t *testing.T) {
	svc, _ := newFeatureAvailabilityTestService(t)

	setting, err := svc.FeatureAvailability()
	if err != nil {
		t.Fatal(err)
	}
	if !setting.WelcomeEnabled {
		t.Fatal("welcome should be enabled by default")
	}
	if setting.Configured || !setting.ShortDramaEnabled || !setting.TaskCenterEnabled || !setting.CreditsEnabled || !setting.CustomChannelsEnabled || setting.FrontendModelsEnabled || !setting.PluginCenterEnabled || !setting.SystemPluginsVisibleToUsers || !setting.InspirationSourcesVisible {
		t.Fatalf("FeatureAvailability() = %#v", setting)
	}
}

func TestFeatureAvailabilityLegacySettingKeepsCustomChannelsEnabled(t *testing.T) {
	svc, db := newFeatureAvailabilityTestService(t)
	legacy := &model.SystemSetting{Key: featureAvailabilitySettingKey, ValueJSON: `{"shortDramaEnabled":false,"taskCenterEnabled":true,"creditsEnabled":true}`}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}

	setting, err := svc.FeatureAvailability()
	if err != nil {
		t.Fatal(err)
	}
	if setting.ShortDramaEnabled || !setting.CustomChannelsEnabled || setting.FrontendModelsEnabled || !setting.PluginCenterEnabled || !setting.SystemPluginsVisibleToUsers {
		t.Fatalf("FeatureAvailability() = %#v", setting)
	}
	if !setting.WelcomeEnabled {
		t.Fatal("existing settings without welcomeEnabled should keep welcome enabled")
	}
}

func TestWelcomeAvailabilityCanBeDisabledAndReenabled(t *testing.T) {
	svc, _ := newFeatureAvailabilityTestService(t)
	actor := &model.User{ID: "admin-welcome", Role: model.UserRoleAdmin}
	for _, enabled := range []bool{false, true} {
		value := platform.DefaultFeatureAvailability()
		value.WelcomeEnabled = enabled
		if _, err := svc.UpdateFeatureAvailability(actor, value); err != nil {
			t.Fatal(err)
		}
		setting, err := svc.FeatureAvailability()
		if err != nil {
			t.Fatal(err)
		}
		if setting.WelcomeEnabled != enabled {
			t.Fatalf("welcomeEnabled = %v, want %v", setting.WelcomeEnabled, enabled)
		}
	}
}

func TestUpdateFeatureAvailabilityPersistsAndAudits(t *testing.T) {
	svc, db := newFeatureAvailabilityTestService(t)
	actor := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	want := FeatureAvailability{ShortDramaEnabled: false, TaskCenterEnabled: true, CreditsEnabled: false, CustomChannelsEnabled: true, PluginCenterEnabled: false, SystemPluginsVisibleToUsers: false}

	setting, err := svc.UpdateFeatureAvailability(actor, want)
	if err != nil {
		t.Fatal(err)
	}
	if !setting.Configured || setting.ShortDramaEnabled || !setting.TaskCenterEnabled || setting.CreditsEnabled || !setting.CustomChannelsEnabled || setting.PluginCenterEnabled || setting.SystemPluginsVisibleToUsers || setting.InspirationSourcesVisible {
		t.Fatalf("UpdateFeatureAvailability() = %#v", setting)
	}
	if enabled, err := svc.FeatureEnabled(FeaturePluginCenter); err != nil || enabled {
		t.Fatalf("FeatureEnabled(pluginCenter) = %v, %v", enabled, err)
	}
	if err := svc.RequireFeature(FeatureShortDrama); err == nil {
		t.Fatal("RequireFeature(shortDrama) error = nil")
	} else {
		var authErr *AuthError
		if !errors.As(err, &authErr) || authErr.Status != 403 {
			t.Fatalf("RequireFeature(shortDrama) error = %#v", err)
		}
	}
	var auditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ?", "feature_availability.update").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count = %d, want 1", auditCount)
	}
}

func TestPluginsForUserKeepsOfficialApplicationsAndHidesManagedPlugins(t *testing.T) {
	svc, _ := newFeatureAvailabilityTestService(t)
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	if _, err := svc.UpdateFeatureAvailability(admin, FeatureAvailability{
		ShortDramaEnabled: true, TaskCenterEnabled: true, CreditsEnabled: true,
		CustomChannelsEnabled: true, PluginCenterEnabled: true, SystemPluginsVisibleToUsers: false,
	}); err != nil {
		t.Fatal(err)
	}
	svc.pluginRuntime = &pluginRuntime{plugins: map[string]pluginRecord{
		"bundled":         {Source: "bundled", Metadata: protocol.Metadata{ID: "bundled", Name: "系统协议", Version: "1"}},
		"uploaded":        {Source: "uploaded", Metadata: protocol.Metadata{ID: "uploaded", Name: "自定义协议", Version: "1"}},
		"app":             {Source: "bundled", Metadata: protocol.Metadata{ID: WorkflowPluginRunningHub, Name: "官方应用", Version: "1"}},
		PluginAotianVideo: {Source: "bundled", Metadata: protocol.Metadata{ID: PluginAotianVideo, Name: "傲天 Seedance 视频", Version: "1"}},
		PluginSD25PPVideo: {Source: "bundled", Metadata: protocol.Metadata{ID: PluginSD25PPVideo, Name: "SD2.5PP 全能参考视频", Version: "1"}},
	}}

	visibleToUser, err := svc.PluginsForUser(&model.User{ID: "user-1", Role: model.UserRoleUser})
	if err != nil {
		t.Fatal(err)
	}
	if len(visibleToUser) != 1 || visibleToUser[0].Manifest.ID != WorkflowPluginRunningHub {
		t.Fatalf("PluginsForUser(user) = %#v", visibleToUser)
	}
	if _, err := svc.UpdateFeatureAvailability(admin, FeatureAvailability{
		ShortDramaEnabled: true, TaskCenterEnabled: true, CreditsEnabled: true,
		CustomChannelsEnabled: true, PluginCenterEnabled: true, SystemPluginsVisibleToUsers: true,
	}); err != nil {
		t.Fatal(err)
	}
	visibleToUser, err = svc.PluginsForUser(&model.User{ID: "user-1", Role: model.UserRoleUser})
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range visibleToUser {
		if isAdminOnlyPlugin(plugin.Manifest.ID) {
			t.Fatalf("admin-only plugin %q was visible to an ordinary user", plugin.Manifest.ID)
		}
	}
	visibleToAdmin, err := svc.PluginsForUser(admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(visibleToAdmin) != 5 {
		t.Fatalf("PluginsForUser(admin) = %#v", visibleToAdmin)
	}
}

func TestProviderProtocolPluginAllowsAdminOnlyPluginsForUsersOnlyThroughSystemChannels(t *testing.T) {
	for _, pluginID := range []string{PluginAotianVideo, PluginSD25PPVideo} {
		t.Run(pluginID, func(t *testing.T) {
			svc, db := newFeatureAvailabilityTestService(t)
			if err := db.AutoMigrate(&model.User{}, &model.PluginPlatformState{}); err != nil {
				t.Fatal(err)
			}
			ordinary := &model.User{ID: "user-1", Username: "user-1", Role: model.UserRoleUser}
			admin := &model.User{ID: "admin-1", Username: "admin-1", Role: model.UserRoleAdmin}
			for _, user := range []*model.User{ordinary, admin} {
				if err := db.Create(user).Error; err != nil {
					t.Fatal(err)
				}
			}
			svc.pluginRuntime = &pluginRuntime{plugins: map[string]pluginRecord{
				pluginID: {Source: "bundled", Status: "enabled", Metadata: protocol.Metadata{ID: pluginID, Name: pluginID, Version: "1"}},
			}}
			// resolveProviderConfig 只在系统渠道路径写入 ChannelID，并用渠道模型协议覆盖客户端传入值。
			systemChannel := providerConfig{ChannelID: "system-1", InterfaceType: pluginID}
			customChannel := providerConfig{BaseURL: "https://example.com", InterfaceType: pluginID}

			if err := svc.requireProviderProtocolPlugin(ordinary.ID, systemChannel); err != nil {
				t.Fatalf("ordinary user calling an admin-configured system channel error = %v", err)
			}
			if err := svc.requireProviderProtocolPlugin(ordinary.ID, customChannel); err == nil {
				t.Fatal("ordinary user used the admin-only plugin through a custom channel")
			}
			if err := svc.RequirePluginForUser(ordinary.ID, pluginID); err == nil {
				t.Fatal("ordinary user was allowed to use the admin-only plugin directly")
			}
			if err := svc.requireProviderProtocolPlugin(admin.ID, customChannel); err != nil {
				t.Fatalf("admin custom channel error = %v", err)
			}

			if err := svc.repo.SavePluginPlatformState(&model.PluginPlatformState{PluginID: pluginID, Available: false, UpdatedBy: admin.ID}); err != nil {
				t.Fatal(err)
			}
			if err := svc.requireProviderProtocolPlugin(ordinary.ID, systemChannel); err == nil {
				t.Fatal("system channel kept using a plugin disabled by the platform")
			}
		})
	}
}

func TestCustomChannelTaskInputRequiresFeature(t *testing.T) {
	svc, _ := newFeatureAvailabilityTestService(t)
	actor := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	if _, err := svc.UpdateFeatureAvailability(actor, FeatureAvailability{ShortDramaEnabled: true, TaskCenterEnabled: true, CreditsEnabled: true, CustomChannelsEnabled: false}); err != nil {
		t.Fatal(err)
	}

	customInput, err := normalizeTaskInput(map[string]any{"config": providerConfig{BaseURL: "https://example.com", APIKey: "private-key", Model: "text-model"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.requireCustomChannelsForTaskInput(customInput); err == nil {
		t.Fatal("custom channel task must be rejected when the feature is disabled")
	}
	systemInput, err := normalizeTaskInput(map[string]any{"config": providerConfig{ChannelID: "system-1", BaseURL: "/api/ai/system/system-1", APIKey: "system", Model: "text-model"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.requireCustomChannelsForTaskInput(systemInput); err != nil {
		t.Fatalf("system channel task error = %v", err)
	}
	legacySystemInput, err := normalizeTaskInput(map[string]any{"config": providerConfig{BaseURL: "/api/ai/system/system-1", APIKey: "system", Model: "text-model"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.requireCustomChannelsForTaskInput(legacySystemInput); err != nil {
		t.Fatalf("legacy system proxy task error = %v", err)
	}
}

func TestCreateTaskDoesNotClassifyCustomChannelAsMissingSystemModel(t *testing.T) {
	svc, _ := newFeatureAvailabilityTestService(t)
	actor := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	if _, err := svc.UpdateFeatureAvailability(actor, FeatureAvailability{ShortDramaEnabled: true, TaskCenterEnabled: true, CreditsEnabled: true, CustomChannelsEnabled: false, FrontendModelsEnabled: true}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.CreateTask("user-1", CreateTaskRequest{
		Type:   "canvas_text",
		Prompt: "test custom channel",
		Input: map[string]any{
			"mode":   "text",
			"config": providerConfig{BaseURL: "https://example.com/v1", APIKey: "private-key", Model: "text-model"},
		},
	})
	if err == nil {
		t.Fatal("CreateTask() error = nil")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Message != "自定义渠道暂未开放" {
		t.Fatalf("CreateTask() error = %#v, want custom channel feature error", err)
	}
}

func TestTaskBillingOrderSkipsPricingWhenCreditsDisabled(t *testing.T) {
	svc, _ := newFeatureAvailabilityTestService(t)
	actor := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	if _, err := svc.UpdateFeatureAvailability(actor, FeatureAvailability{ShortDramaEnabled: true, TaskCenterEnabled: true, CreditsEnabled: false, CustomChannelsEnabled: true}); err != nil {
		t.Fatal(err)
	}

	order, err := svc.taskBillingOrder("user-1", &model.Task{ID: "task-1"}, map[string]any{"config": map[string]any{"channelId": "missing", "model": "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	if order != nil {
		t.Fatalf("taskBillingOrder() = %#v, want nil", order)
	}
}

func newFeatureAvailabilityTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir()), db
}
