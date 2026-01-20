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

func TestGetDefaultIssueBackend(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   string
	}{
		{"nil config", nil, DefaultIssueBackend},
		{"empty config", &GlobalConfig{}, DefaultIssueBackend},
		{"empty defaults", &GlobalConfig{Defaults: DefaultsConfig{}}, DefaultIssueBackend},
		{"github explicit", &GlobalConfig{Defaults: DefaultsConfig{IssueBackend: "github"}}, "github"},
		{"linear explicit", &GlobalConfig{Defaults: DefaultsConfig{IssueBackend: "linear"}}, "linear"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultIssueBackend(); got != tt.want {
				t.Errorf("GetDefaultIssueBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetDefaultPermissionsChecker(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   string
	}{
		{"nil config", nil, DefaultPermissionsChecker},
		{"empty config", &GlobalConfig{}, DefaultPermissionsChecker},
		{"empty defaults", &GlobalConfig{Defaults: DefaultsConfig{}}, DefaultPermissionsChecker},
		{"llm explicit", &GlobalConfig{Defaults: DefaultsConfig{PermissionsChecker: "llm"}}, "llm"},
		{"manual explicit", &GlobalConfig{Defaults: DefaultsConfig{PermissionsChecker: "manual"}}, "manual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultPermissionsChecker(); got != tt.want {
				t.Errorf("GetDefaultPermissionsChecker() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetDefaultAutostart(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name   string
		config *GlobalConfig
		want   bool
	}{
		{"nil config", nil, false},
		{"empty config", &GlobalConfig{}, false},
		{"empty defaults", &GlobalConfig{Defaults: DefaultsConfig{}}, false},
		{"true explicit", &GlobalConfig{Defaults: DefaultsConfig{Autostart: &trueVal}}, true},
		{"false explicit", &GlobalConfig{Defaults: DefaultsConfig{Autostart: &falseVal}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultAutostart(); got != tt.want {
				t.Errorf("GetDefaultAutostart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetDefaultMaxAgents(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   int
	}{
		{"nil config", nil, DefaultMaxAgents},
		{"empty config", &GlobalConfig{}, DefaultMaxAgents},
		{"empty defaults", &GlobalConfig{Defaults: DefaultsConfig{}}, DefaultMaxAgents},
		{"custom value", &GlobalConfig{Defaults: DefaultsConfig{MaxAgents: 5}}, 5},
		{"zero value uses default", &GlobalConfig{Defaults: DefaultsConfig{MaxAgents: 0}}, DefaultMaxAgents},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultMaxAgents(); got != tt.want {
				t.Errorf("GetDefaultMaxAgents() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetDefaultPlannerBackend(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   string
	}{
		{"nil config", nil, "claude"},
		{"empty config", &GlobalConfig{}, "claude"},
		{"planner explicit", &GlobalConfig{Defaults: DefaultsConfig{PlannerBackend: "codex"}}, "codex"},
		{"falls back to agent", &GlobalConfig{Defaults: DefaultsConfig{AgentBackend: "codex"}}, "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultPlannerBackend(); got != tt.want {
				t.Errorf("GetDefaultPlannerBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetDefaultCodingBackend(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   string
	}{
		{"nil config", nil, "claude"},
		{"empty config", &GlobalConfig{}, "claude"},
		{"coding explicit", &GlobalConfig{Defaults: DefaultsConfig{CodingBackend: "codex"}}, "codex"},
		{"falls back to agent", &GlobalConfig{Defaults: DefaultsConfig{AgentBackend: "codex"}}, "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultCodingBackend(); got != tt.want {
				t.Errorf("GetDefaultCodingBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetDefaultMergeStrategy(t *testing.T) {
	tests := []struct {
		name   string
		config *GlobalConfig
		want   string
	}{
		{"nil config", nil, DefaultMergeStrategy},
		{"empty config", &GlobalConfig{}, DefaultMergeStrategy},
		{"pull-request explicit", &GlobalConfig{Defaults: DefaultsConfig{MergeStrategy: "pull-request"}}, "pull-request"},
		{"direct explicit", &GlobalConfig{Defaults: DefaultsConfig{MergeStrategy: "direct"}}, "direct"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDefaultMergeStrategy(); got != tt.want {
				t.Errorf("GetDefaultMergeStrategy() = %q, want %q", got, tt.want)
			}
		})
	}
}
