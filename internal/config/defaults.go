package config

import "sync"

func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Host:           "127.0.0.1",
			Port:           886,
			EndpointPrefix: "/api",
			AuthToken:      "",
			MaxQueueSize:   100,
		},
		Endpoints: []EndpointConfig{
			{Path: "confirm", Title: "ClaudeCode 通知", OkText: "确定", CancelText: "取消", Mode: "two-button"},
			{Path: "danger", Title: "⚠️ 危险操作确认", OkText: "允许", CancelText: "拒绝", Mode: "two-button"},
			{Path: "info", Title: "通知", OkText: "知道了", CancelText: "", Mode: "single-button"},
		},
		UI: UIConfig{
			PopupPosition: "center",
			PopupSize:     PopupSize{Width: 480, Height: 220},
			Theme:         "system",
		},
		Behavior: BehaviorConfig{
			DefaultTimeoutSeconds: 180,
			TimeoutAction:         "timeout",
			SoundEnabled:          true,
			Autostart:             false,
			MinimizeToTrayOnClose: true,
		},
		History: HistoryConfig{MaxRecords: 1000, RetentionDays: 30},
		Log:     LogConfig{Level: "info", MaxSizeMB: 5, MaxBackups: 3},
		mu:      &sync.RWMutex{},
	}
}
