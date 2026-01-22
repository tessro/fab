package plugin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tessro/fab/internal/plugin"
)

func TestPluginStructure(t *testing.T) {
	testDir := t.TempDir()

	err := plugin.Install(testDir)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Check expected structure
	expected := []string{
		".claude-plugin/plugin.json",
		"skills/review/SKILL.md",
		"skills/docs-review/SKILL.md",
	}

	for _, path := range expected {
		fullPath := filepath.Join(testDir, path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("Missing expected file: %s", path)
		} else {
			t.Logf("Found: %s", path)
		}
	}

	// Verify skills are NOT in the old incorrect location
	badPath := filepath.Join(testDir, ".claude-plugin/skills")
	if _, err := os.Stat(badPath); !os.IsNotExist(err) {
		t.Errorf("Skills should not be inside .claude-plugin/: %s", badPath)
	}
}
