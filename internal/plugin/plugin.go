// Package plugin provides installation and management of the fab Claude Code plugin.
package plugin

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:files
var pluginFS embed.FS

// DefaultInstallDir returns the default plugin installation directory.
func DefaultInstallDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/fab/claude-code-plugin"
	}
	return filepath.Join(home, ".fab", "claude-code-plugin")
}

// Install writes the embedded plugin files to the specified directory.
// It performs a fresh install by removing any existing files first.
func Install(dir string) error {
	// Remove existing installation for fresh sync
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove existing plugin: %w", err)
	}

	// Create the plugin directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}

	// Walk the embedded filesystem and write files
	return fs.WalkDir(pluginFS, "files", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get the relative path from "files/" prefix
		relPath, err := filepath.Rel("files", path)
		if err != nil {
			return err
		}

		// Skip the root "." entry
		if relPath == "." {
			return nil
		}

		destPath := filepath.Join(dir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Read the embedded file
		content, err := pluginFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded file %s: %w", path, err)
		}

		// Write to destination
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("write file %s: %w", destPath, err)
		}

		return nil
	})
}
