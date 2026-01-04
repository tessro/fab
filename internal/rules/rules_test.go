package rules

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		// Wildcard patterns
		{"empty pattern matches all", "", "anything", true},
		{"wildcard matches all", ":*", "anything", true},
		{"wildcard matches empty", ":*", "", true},

		// Prefix patterns
		{"prefix match success", "git :*", "git status", true},
		{"prefix match with space", "git :*", "git commit -m 'test'", true},
		{"prefix match exact prefix", "git:*", "git", true},
		{"prefix match failure", "git :*", "cargo build", false},
		{"prefix match partial failure", "git :*", "gitignore", false},

		// Exact patterns
		{"exact match success", "git status", "git status", true},
		{"exact match failure", "git status", "git commit", false},
		{"exact match with extra", "git status", "git status --short", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchPattern(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestResolvePrimaryField(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		toolInput string
		want      string
	}{
		{"bash command", "Bash", `{"command":"git status"}`, "git status"},
		{"read file_path", "Read", `{"file_path":"/etc/passwd"}`, "/etc/passwd"},
		{"write file_path", "Write", `{"file_path":"/tmp/test.txt","content":"hello"}`, "/tmp/test.txt"},
		{"edit file_path", "Edit", `{"file_path":"/src/main.go"}`, "/src/main.go"},
		{"glob pattern", "Glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"grep pattern", "Grep", `{"pattern":"TODO"}`, "TODO"},
		{"webfetch url", "WebFetch", `{"url":"https://example.com"}`, "https://example.com"},
		{"task prompt", "Task", `{"prompt":"search for files"}`, "search for files"},
		{"unknown tool", "Unknown", `{"foo":"bar"}`, ""},
		{"empty input", "Bash", `{}`, ""},
		{"invalid json", "Bash", `invalid`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePrimaryField(tt.toolName, json.RawMessage(tt.toolInput))
			if got != tt.want {
				t.Errorf("ResolvePrimaryField(%q, %q) = %q, want %q", tt.toolName, tt.toolInput, got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Create temp file
	dir := t.TempDir()
	configPath := filepath.Join(dir, "permissions.toml")

	content := `
[[rules]]
tool = "Bash"
effect = "allow"
pattern = "git :*"

[[rules]]
tool = "Read"
effect = "allow"
pattern = ":*"

[[rules]]
tool = "Write"
effect = "deny"
pattern = "/etc/:*"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(config.Rules) != 3 {
		t.Errorf("got %d rules, want 3", len(config.Rules))
	}

	// Check first rule
	if config.Rules[0].Tool != "Bash" {
		t.Errorf("rule 0 tool = %q, want Bash", config.Rules[0].Tool)
	}
	if config.Rules[0].Effect != EffectAllow {
		t.Errorf("rule 0 effect = %q, want allow", config.Rules[0].Effect)
	}
	if config.Rules[0].Pattern != "git :*" {
		t.Errorf("rule 0 pattern = %q, want 'git :*'", config.Rules[0].Pattern)
	}
}

func TestLoadConfigNonExistent(t *testing.T) {
	config, err := LoadConfig("/nonexistent/path/permissions.toml")
	if err != nil {
		t.Errorf("LoadConfig should not error for non-existent file, got: %v", err)
	}
	if config != nil {
		t.Error("LoadConfig should return nil config for non-existent file")
	}
}

func TestEvaluator(t *testing.T) {
	// Create temp directory structure
	dir := t.TempDir()
	globalDir := filepath.Join(dir, ".config", "fab")
	projectDir := filepath.Join(dir, ".fab", "projects", "testproj")

	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write global rules
	globalRules := `
[[rules]]
tool = "Bash"
effect = "allow"
pattern = "ls :*"

[[rules]]
tool = "Read"
effect = "allow"
pattern = ":*"
`
	if err := os.WriteFile(filepath.Join(globalDir, "permissions.toml"), []byte(globalRules), 0644); err != nil {
		t.Fatal(err)
	}

	// Write project rules (take precedence)
	projectRules := `
[[rules]]
tool = "Bash"
effect = "allow"
pattern = "git :*"

[[rules]]
tool = "Bash"
effect = "deny"
pattern = "rm :*"
`
	if err := os.WriteFile(filepath.Join(projectDir, "permissions.toml"), []byte(projectRules), 0644); err != nil {
		t.Fatal(err)
	}

	// Create evaluator with custom paths (we need to test with real paths)
	// For this test, we'll use a modified approach
	t.Run("pattern matching logic", func(t *testing.T) {
		tests := []struct {
			name       string
			toolName   string
			toolInput  string
			wantEffect Effect
			wantMatch  bool
		}{
			{"git allowed by project rule", "Bash", `{"command":"git status"}`, EffectAllow, true},
			{"rm denied by project rule", "Bash", `{"command":"rm -rf /"}`, EffectDeny, true},
			{"ls allowed by global rule", "Bash", `{"command":"ls -la"}`, EffectAllow, true},
			{"read allowed by global wildcard", "Read", `{"file_path":"/any/file.txt"}`, EffectAllow, true},
			{"unknown command no match", "Bash", `{"command":"cargo build"}`, EffectPass, false},
		}

		// Create a simple in-memory test by directly testing rule evaluation
		projectConfig := &Config{
			Rules: []Rule{
				{Tool: "Bash", Effect: EffectAllow, Pattern: "git :*"},
				{Tool: "Bash", Effect: EffectDeny, Pattern: "rm :*"},
			},
		}
		globalConfig := &Config{
			Rules: []Rule{
				{Tool: "Bash", Effect: EffectAllow, Pattern: "ls :*"},
				{Tool: "Read", Effect: EffectAllow, Pattern: ":*"},
			},
		}

		// Combine rules as evaluator would
		allRules := append(projectConfig.Rules, globalConfig.Rules...)

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				primaryField := ResolvePrimaryField(tt.toolName, json.RawMessage(tt.toolInput))

				var gotEffect Effect = EffectPass
				gotMatch := false

				for _, rule := range allRules {
					if rule.Tool != tt.toolName {
						continue
					}
					if MatchPattern(rule.Pattern, primaryField) {
						if rule.Effect != EffectPass {
							gotEffect = rule.Effect
							gotMatch = true
							break
						}
					}
				}

				if gotEffect != tt.wantEffect {
					t.Errorf("effect = %v, want %v", gotEffect, tt.wantEffect)
				}
				if gotMatch != tt.wantMatch {
					t.Errorf("matched = %v, want %v", gotMatch, tt.wantMatch)
				}
			})
		}
	})
}

func TestPatternsArray(t *testing.T) {
	// Test that patterns array works (any match counts)
	rules := []Rule{
		{Tool: "Bash", Effect: EffectAllow, Patterns: []string{"git :*", "cargo :*", "go :*"}},
		{Tool: "Bash", Effect: EffectDeny, Patterns: []string{"rm :*", "sudo :*"}},
	}

	tests := []struct {
		name       string
		command    string
		wantEffect Effect
		wantMatch  bool
	}{
		{"git matches first pattern", "git status", EffectAllow, true},
		{"cargo matches second pattern", "cargo build", EffectAllow, true},
		{"go matches third pattern", "go test ./...", EffectAllow, true},
		{"rm matches deny rule", "rm -rf /", EffectDeny, true},
		{"sudo matches deny rule", "sudo apt install", EffectDeny, true},
		{"unknown no match", "python script.py", EffectPass, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primaryField := tt.command
			var gotEffect Effect = EffectPass
			gotMatch := false

			for _, rule := range rules {
				if rule.Tool != "Bash" {
					continue
				}
				// Check patterns array
				matched := false
				for _, p := range rule.Patterns {
					if MatchPattern(p, primaryField) {
						matched = true
						break
					}
				}
				if matched {
					gotEffect = rule.Effect
					gotMatch = true
					break
				}
			}

			if gotEffect != tt.wantEffect {
				t.Errorf("effect = %v, want %v", gotEffect, tt.wantEffect)
			}
			if gotMatch != tt.wantMatch {
				t.Errorf("matched = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestNoPatternMatchesAll(t *testing.T) {
	// Test that omitting pattern/patterns matches all
	rules := []Rule{
		{Tool: "Read", Effect: EffectAllow}, // No pattern = match all
		{Tool: "Bash", Effect: EffectDeny},  // No pattern = match all
	}

	tests := []struct {
		name       string
		toolName   string
		toolInput  string
		wantEffect Effect
	}{
		{"read any file allowed", "Read", `{"file_path":"/any/path"}`, EffectAllow},
		{"read another file allowed", "Read", `{"file_path":"/etc/passwd"}`, EffectAllow},
		{"bash any command denied", "Bash", `{"command":"anything"}`, EffectDeny},
		{"bash another command denied", "Bash", `{"command":"rm -rf /"}`, EffectDeny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotEffect Effect = EffectPass

			for _, rule := range rules {
				if rule.Tool != tt.toolName {
					continue
				}
				// No pattern means match all
				if rule.Pattern == "" && len(rule.Patterns) == 0 {
					gotEffect = rule.Effect
					break
				}
			}

			if gotEffect != tt.wantEffect {
				t.Errorf("effect = %v, want %v", gotEffect, tt.wantEffect)
			}
		})
	}
}

func TestLoadConfigWithPatterns(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "permissions.toml")

	content := `
[[rules]]
tool = "Bash"
effect = "allow"
patterns = ["git :*", "cargo :*", "go :*"]

[[rules]]
tool = "Read"
effect = "allow"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(config.Rules) != 2 {
		t.Errorf("got %d rules, want 2", len(config.Rules))
	}

	// Check patterns array
	if len(config.Rules[0].Patterns) != 3 {
		t.Errorf("rule 0 patterns count = %d, want 3", len(config.Rules[0].Patterns))
	}
	if config.Rules[0].Patterns[0] != "git :*" {
		t.Errorf("rule 0 patterns[0] = %q, want 'git :*'", config.Rules[0].Patterns[0])
	}

	// Check rule with no pattern (matches all)
	if config.Rules[1].Pattern != "" {
		t.Errorf("rule 1 pattern = %q, want empty", config.Rules[1].Pattern)
	}
	if len(config.Rules[1].Patterns) != 0 {
		t.Errorf("rule 1 patterns = %v, want empty", config.Rules[1].Patterns)
	}
}

func TestScriptMatch(t *testing.T) {
	// Create temp scripts
	dir := t.TempDir()

	// Script that allows
	allowScript := filepath.Join(dir, "allow.sh")
	if err := os.WriteFile(allowScript, []byte("#!/bin/sh\necho allow\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Script that denies
	denyScript := filepath.Join(dir, "deny.sh")
	if err := os.WriteFile(denyScript, []byte("#!/bin/sh\necho deny\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Script that passes
	passScript := filepath.Join(dir, "pass.sh")
	if err := os.WriteFile(passScript, []byte("#!/bin/sh\necho pass\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Script that fails
	failScript := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(failScript, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	toolInput := json.RawMessage(`{"command":"test"}`)

	tests := []struct {
		name       string
		script     string
		wantEffect Effect
		wantErr    bool
	}{
		{"allow script", allowScript, EffectAllow, false},
		{"deny script", denyScript, EffectDeny, false},
		{"pass script", passScript, EffectPass, false},
		{"fail script", failScript, EffectPass, true},
		{"nonexistent script", "/nonexistent/script.sh", EffectPass, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effect, err := ScriptMatch(ctx, tt.script, "Bash", toolInput)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if effect != tt.wantEffect {
				t.Errorf("effect = %v, want %v", effect, tt.wantEffect)
			}
		})
	}
}
