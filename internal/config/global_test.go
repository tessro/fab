package config

import "testing"

func TestGetLogLevel(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   string
	}{
		{"nil config", nil, DefaultLogLevel},
		{"empty log level", &GlobalConfig{}, DefaultLogLevel},
		{"custom log level", &GlobalConfig{LogLevel: "debug"}, "debug"},
		{"warn level", &GlobalConfig{LogLevel: "warn"}, "warn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetLogLevel(); got != tt.want {
				t.Errorf("GetLogLevel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetDefaultAgentBackend(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   string
	}{
		{"nil config", nil, "claude"},
		{"empty config", &GlobalConfig{}, "claude"},
		{"empty defaults", &GlobalConfig{Defaults: DefaultsConfig{}}, "claude"},
		{"claude explicit", &GlobalConfig{Defaults: DefaultsConfig{AgentBackend: "claude"}}, "claude"},
		{"codex explicit", &GlobalConfig{Defaults: DefaultsConfig{AgentBackend: "codex"}}, "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultAgentBackend(); got != tt.want {
				t.Errorf("GetDefaultAgentBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}
