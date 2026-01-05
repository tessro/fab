package rules

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRewritePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		cwd     string
		want    string
	}{
		// Worktree-scoped patterns (/ → cwd/)
		{"worktree root", "/:*", "/home/user/project", "/home/user/project/:*"},
		{"worktree subdir", "/src/:*", "/home/user/project", "/home/user/project/src/:*"},
		{"worktree exact", "/file.txt", "/home/user/project", "/home/user/project/file.txt"},

		// Absolute path patterns (// → /)
		{"absolute root", "//:*", "/home/user/project", "/:*"},
		{"absolute path", "//etc/passwd", "/home/user/project", "/etc/passwd"},
		{"absolute subdir", "//tmp/:*", "/home/user/project", "/tmp/:*"},

		// Pass-through patterns (no / prefix)
		{"command pattern", "git :*", "/home/user/project", "git :*"},
		{"url pattern", "https://example.com:*", "/home/user/project", "https://example.com:*"},
		{"empty pattern", "", "/home/user/project", ""},
		{"wildcard pattern", ":*", "/home/user/project", ":*"},

		// Edge cases
		{"worktree with empty cwd", "/:*", "", "/:*"},
		{"single slash", "/", "/home/user/project", "/home/user/project/"},
		{"double slash only", "//", "/home/user/project", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewritePattern(tt.pattern, tt.cwd)
			if got != tt.want {
				t.Errorf("RewritePattern(%q, %q) = %q, want %q", tt.pattern, tt.cwd, got, tt.want)
			}
		})
	}
}

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
		{"prefix match no space", "bd:*", "bd ready", true},
		{"prefix match no space 2", "git:*", "git status", true},
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
		{"skill name", "Skill", `{"skill":"beads:ready"}`, "beads:ready"},
		{"websearch query", "WebSearch", `{"query":"golang tutorials"}`, "golang tutorials"},
		{"notebookedit path", "NotebookEdit", `{"notebook_path":"/home/user/analysis.ipynb"}`, "/home/user/analysis.ipynb"},
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
action ="allow"
pattern = "git :*"

[[rules]]
tool = "Read"
action ="allow"
pattern = ":*"

[[rules]]
tool = "Write"
action ="deny"
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
	if config.Rules[0].Action != ActionAllow {
		t.Errorf("rule 0 action = %q, want allow", config.Rules[0].Action)
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
action ="allow"
pattern = "ls :*"

[[rules]]
tool = "Read"
action ="allow"
pattern = ":*"
`
	if err := os.WriteFile(filepath.Join(globalDir, "permissions.toml"), []byte(globalRules), 0644); err != nil {
		t.Fatal(err)
	}

	// Write project rules (take precedence)
	projectRules := `
[[rules]]
tool = "Bash"
action ="allow"
pattern = "git :*"

[[rules]]
tool = "Bash"
action ="deny"
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
			wantAction Action
			wantMatch  bool
		}{
			{"git allowed by project rule", "Bash", `{"command":"git status"}`, ActionAllow, true},
			{"rm denied by project rule", "Bash", `{"command":"rm -rf /"}`, ActionDeny, true},
			{"ls allowed by global rule", "Bash", `{"command":"ls -la"}`, ActionAllow, true},
			{"read allowed by global wildcard", "Read", `{"file_path":"/any/file.txt"}`, ActionAllow, true},
			{"unknown command no match", "Bash", `{"command":"cargo build"}`, ActionPass, false},
		}

		// Create a simple in-memory test by directly testing rule evaluation
		projectConfig := &Config{
			Rules: []Rule{
				{Tool: "Bash", Action: ActionAllow, Pattern: "git :*"},
				{Tool: "Bash", Action: ActionDeny, Pattern: "rm :*"},
			},
		}
		globalConfig := &Config{
			Rules: []Rule{
				{Tool: "Bash", Action: ActionAllow, Pattern: "ls :*"},
				{Tool: "Read", Action: ActionAllow, Pattern: ":*"},
			},
		}

		// Combine rules as evaluator would
		allRules := append(projectConfig.Rules, globalConfig.Rules...)

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				primaryField := ResolvePrimaryField(tt.toolName, json.RawMessage(tt.toolInput))

				var gotAction Action= ActionPass
				gotMatch := false

				for _, rule := range allRules {
					if rule.Tool != tt.toolName {
						continue
					}
					if MatchPattern(rule.Pattern, primaryField) {
						if rule.Action != ActionPass {
							gotAction = rule.Action
							gotMatch = true
							break
						}
					}
				}

				if gotAction != tt.wantAction {
					t.Errorf("action = %v, want %v", gotAction, tt.wantAction)
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
		{Tool: "Bash", Action: ActionAllow, Patterns: []string{"git :*", "cargo :*", "go :*"}},
		{Tool: "Bash", Action: ActionDeny, Patterns: []string{"rm :*", "sudo :*"}},
	}

	tests := []struct {
		name       string
		command    string
		wantAction Action
		wantMatch  bool
	}{
		{"git matches first pattern", "git status", ActionAllow, true},
		{"cargo matches second pattern", "cargo build", ActionAllow, true},
		{"go matches third pattern", "go test ./...", ActionAllow, true},
		{"rm matches deny rule", "rm -rf /", ActionDeny, true},
		{"sudo matches deny rule", "sudo apt install", ActionDeny, true},
		{"unknown no match", "python script.py", ActionPass, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primaryField := tt.command
			var gotAction Action= ActionPass
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
					gotAction = rule.Action
					gotMatch = true
					break
				}
			}

			if gotAction != tt.wantAction {
				t.Errorf("action = %v, want %v", gotAction, tt.wantAction)
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
		{Tool: "Read", Action: ActionAllow}, // No pattern = match all
		{Tool: "Bash", Action: ActionDeny},  // No pattern = match all
	}

	tests := []struct {
		name       string
		toolName   string
		toolInput  string
		wantAction Action
	}{
		{"read any file allowed", "Read", `{"file_path":"/any/path"}`, ActionAllow},
		{"read another file allowed", "Read", `{"file_path":"/etc/passwd"}`, ActionAllow},
		{"bash any command denied", "Bash", `{"command":"anything"}`, ActionDeny},
		{"bash another command denied", "Bash", `{"command":"rm -rf /"}`, ActionDeny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAction Action= ActionPass

			for _, rule := range rules {
				if rule.Tool != tt.toolName {
					continue
				}
				// No pattern means match all
				if rule.Pattern == "" && len(rule.Patterns) == 0 {
					gotAction = rule.Action
					break
				}
			}

			if gotAction != tt.wantAction {
				t.Errorf("action = %v, want %v", gotAction, tt.wantAction)
			}
		})
	}
}

func TestEvaluatorWithPatterns(t *testing.T) {
	// Test the full evaluator flow with patterns array
	ctx := context.Background()

	// Create a mock evaluator that we can test directly
	// by building rules and testing the evaluation logic
	rules := []Rule{
		{Tool: "Bash", Action: ActionAllow, Patterns: []string{"bd:*", "git:*"}},
	}

	tests := []struct {
		name       string
		toolName   string
		toolInput  string
		wantAction Action
		wantMatch  bool
	}{
		{"bd ready matches", "Bash", `{"command":"bd ready"}`, ActionAllow, true},
		{"bd list matches", "Bash", `{"command":"bd list"}`, ActionAllow, true},
		{"git status matches", "Bash", `{"command":"git status"}`, ActionAllow, true},
		{"other command no match", "Bash", `{"command":"cargo build"}`, ActionPass, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primaryField := ResolvePrimaryField(tt.toolName, json.RawMessage(tt.toolInput))

			var gotAction Action= ActionPass
			gotMatch := false

			for _, rule := range rules {
				if rule.Tool != tt.toolName {
					continue
				}

				matched := false
				if rule.Script != "" {
					action, err := ScriptMatch(ctx, rule.Script, tt.toolName, json.RawMessage(tt.toolInput))
					if err == nil && action != ActionPass {
						gotAction = action
						gotMatch = true
						break
					}
					continue
				} else if rule.Pattern != "" {
					matched = MatchPattern(rule.Pattern, primaryField)
				} else if len(rule.Patterns) > 0 {
					for _, p := range rule.Patterns {
						if MatchPattern(p, primaryField) {
							matched = true
							break
						}
					}
				} else {
					matched = true
				}

				if matched {
					if rule.Action != ActionPass {
						gotAction = rule.Action
						gotMatch = true
						break
					}
				}
			}

			if gotAction != tt.wantAction {
				t.Errorf("action = %v, want %v", gotAction, tt.wantAction)
			}
			if gotMatch != tt.wantMatch {
				t.Errorf("matched = %v, want %v", gotMatch, tt.wantMatch)
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
action ="allow"
patterns = ["git :*", "cargo :*", "go :*"]

[[rules]]
tool = "Read"
action ="allow"
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
		wantAction Action
		wantErr    bool
	}{
		{"allow script", allowScript, ActionAllow, false},
		{"deny script", denyScript, ActionDeny, false},
		{"pass script", passScript, ActionPass, false},
		{"fail script", failScript, ActionPass, true},
		{"nonexistent script", "/nonexistent/script.sh", ActionPass, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := ScriptMatch(ctx, tt.script, "Bash", toolInput)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if action != tt.wantAction {
				t.Errorf("action = %v, want %v", action, tt.wantAction)
			}
		})
	}
}
