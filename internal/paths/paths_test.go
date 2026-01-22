package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBaseDir(t *testing.T) {
	t.Run("default uses home directory", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		defer os.Unsetenv(EnvFabDir)

		dir, err := BaseDir()
		if err != nil {
			t.Fatalf("BaseDir() error = %v", err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".fab")
		if dir != expected {
			t.Errorf("BaseDir() = %q, want %q", dir, expected)
		}
	})

	t.Run("FAB_DIR overrides default", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		defer os.Unsetenv(EnvFabDir)

		dir, err := BaseDir()
		if err != nil {
			t.Fatalf("BaseDir() error = %v", err)
		}
		if dir != "/tmp/fab-test" {
			t.Errorf("BaseDir() = %q, want %q", dir, "/tmp/fab-test")
		}
	})
}

func TestConfigDir(t *testing.T) {
	t.Run("default uses home config directory", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		defer os.Unsetenv(EnvFabDir)

		dir, err := ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir() error = %v", err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".config", "fab")
		if dir != expected {
			t.Errorf("ConfigDir() = %q, want %q", dir, expected)
		}
	})

	t.Run("FAB_DIR overrides to FAB_DIR/config", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		defer os.Unsetenv(EnvFabDir)

		dir, err := ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir() error = %v", err)
		}
		expected := "/tmp/fab-test/config"
		if dir != expected {
			t.Errorf("ConfigDir() = %q, want %q", dir, expected)
		}
	})
}

func TestConfigPath(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		defer os.Unsetenv(EnvFabDir)

		path, err := ConfigPath()
		if err != nil {
			t.Fatalf("ConfigPath() error = %v", err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".config", "fab", "config.toml")
		if path != expected {
			t.Errorf("ConfigPath() = %q, want %q", path, expected)
		}
	})

	t.Run("FAB_DIR override", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		defer os.Unsetenv(EnvFabDir)

		path, err := ConfigPath()
		if err != nil {
			t.Fatalf("ConfigPath() error = %v", err)
		}
		expected := "/tmp/fab-test/config/config.toml"
		if path != expected {
			t.Errorf("ConfigPath() = %q, want %q", path, expected)
		}
	})
}

func TestPermissionsPath(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		defer os.Unsetenv(EnvFabDir)

		path, err := PermissionsPath()
		if err != nil {
			t.Fatalf("PermissionsPath() error = %v", err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".config", "fab", "permissions.toml")
		if path != expected {
			t.Errorf("PermissionsPath() = %q, want %q", path, expected)
		}
	})

	t.Run("FAB_DIR override", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		defer os.Unsetenv(EnvFabDir)

		path, err := PermissionsPath()
		if err != nil {
			t.Fatalf("PermissionsPath() error = %v", err)
		}
		expected := "/tmp/fab-test/config/permissions.toml"
		if path != expected {
			t.Errorf("PermissionsPath() = %q, want %q", path, expected)
		}
	})
}

func TestProjectsDir(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		defer os.Unsetenv(EnvFabDir)

		dir, err := ProjectsDir()
		if err != nil {
			t.Fatalf("ProjectsDir() error = %v", err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".fab", "projects")
		if dir != expected {
			t.Errorf("ProjectsDir() = %q, want %q", dir, expected)
		}
	})

	t.Run("FAB_DIR override", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		defer os.Unsetenv(EnvFabDir)

		dir, err := ProjectsDir()
		if err != nil {
			t.Fatalf("ProjectsDir() error = %v", err)
		}
		expected := "/tmp/fab-test/projects"
		if dir != expected {
			t.Errorf("ProjectsDir() = %q, want %q", dir, expected)
		}
	})
}

func TestProjectDir(t *testing.T) {
	os.Setenv(EnvFabDir, "/tmp/fab-test")
	defer os.Unsetenv(EnvFabDir)

	dir, err := ProjectDir("myproject")
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	expected := "/tmp/fab-test/projects/myproject"
	if dir != expected {
		t.Errorf("ProjectDir() = %q, want %q", dir, expected)
	}
}

func TestProjectPermissionsPath(t *testing.T) {
	os.Setenv(EnvFabDir, "/tmp/fab-test")
	defer os.Unsetenv(EnvFabDir)

	path, err := ProjectPermissionsPath("myproject")
	if err != nil {
		t.Fatalf("ProjectPermissionsPath() error = %v", err)
	}
	expected := "/tmp/fab-test/projects/myproject/permissions.toml"
	if path != expected {
		t.Errorf("ProjectPermissionsPath() = %q, want %q", path, expected)
	}
}

func TestSocketPath(t *testing.T) {
	t.Run("default uses home directory", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		os.Unsetenv(EnvSocketPath)
		defer func() {
			os.Unsetenv(EnvFabDir)
			os.Unsetenv(EnvSocketPath)
		}()

		path := SocketPath()
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".fab", "fab.sock")
		if path != expected {
			t.Errorf("SocketPath() = %q, want %q", path, expected)
		}
	})

	t.Run("FAB_DIR derives socket path", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		os.Unsetenv(EnvSocketPath)
		defer func() {
			os.Unsetenv(EnvFabDir)
			os.Unsetenv(EnvSocketPath)
		}()

		path := SocketPath()
		expected := "/tmp/fab-test/fab.sock"
		if path != expected {
			t.Errorf("SocketPath() = %q, want %q", path, expected)
		}
	})

	t.Run("FAB_SOCKET_PATH overrides FAB_DIR", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		os.Setenv(EnvSocketPath, "/custom/path.sock")
		defer func() {
			os.Unsetenv(EnvFabDir)
			os.Unsetenv(EnvSocketPath)
		}()

		path := SocketPath()
		expected := "/custom/path.sock"
		if path != expected {
			t.Errorf("SocketPath() = %q, want %q", path, expected)
		}
	})
}

func TestPIDPath(t *testing.T) {
	t.Run("default uses home directory", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		os.Unsetenv(EnvPIDPath)
		defer func() {
			os.Unsetenv(EnvFabDir)
			os.Unsetenv(EnvPIDPath)
		}()

		path := PIDPath()
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".fab", "fab.pid")
		if path != expected {
			t.Errorf("PIDPath() = %q, want %q", path, expected)
		}
	})

	t.Run("FAB_DIR derives PID path", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		os.Unsetenv(EnvPIDPath)
		defer func() {
			os.Unsetenv(EnvFabDir)
			os.Unsetenv(EnvPIDPath)
		}()

		path := PIDPath()
		expected := "/tmp/fab-test/fab.pid"
		if path != expected {
			t.Errorf("PIDPath() = %q, want %q", path, expected)
		}
	})

	t.Run("FAB_PID_PATH overrides FAB_DIR", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		os.Setenv(EnvPIDPath, "/custom/path.pid")
		defer func() {
			os.Unsetenv(EnvFabDir)
			os.Unsetenv(EnvPIDPath)
		}()

		path := PIDPath()
		expected := "/custom/path.pid"
		if path != expected {
			t.Errorf("PIDPath() = %q, want %q", path, expected)
		}
	})
}

func TestPlansDir(t *testing.T) {
	t.Run("default uses home directory", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		defer os.Unsetenv(EnvFabDir)

		dir, err := PlansDir()
		if err != nil {
			t.Fatalf("PlansDir() error = %v", err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".fab", "plans")
		if dir != expected {
			t.Errorf("PlansDir() = %q, want %q", dir, expected)
		}
	})

	t.Run("FAB_DIR override", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		defer os.Unsetenv(EnvFabDir)

		dir, err := PlansDir()
		if err != nil {
			t.Fatalf("PlansDir() error = %v", err)
		}
		expected := "/tmp/fab-test/plans"
		if dir != expected {
			t.Errorf("PlansDir() = %q, want %q", dir, expected)
		}
	})
}

func TestPlanPath(t *testing.T) {
	os.Setenv(EnvFabDir, "/tmp/fab-test")
	defer os.Unsetenv(EnvFabDir)

	path, err := PlanPath("abc123")
	if err != nil {
		t.Fatalf("PlanPath() error = %v", err)
	}
	expected := "/tmp/fab-test/plans/abc123.md"
	if path != expected {
		t.Errorf("PlanPath() = %q, want %q", path, expected)
	}
}

func TestDirectorWorkDir(t *testing.T) {
	t.Run("default uses projects directory", func(t *testing.T) {
		os.Unsetenv(EnvFabDir)
		defer os.Unsetenv(EnvFabDir)

		dir, err := DirectorWorkDir()
		if err != nil {
			t.Fatalf("DirectorWorkDir() error = %v", err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".fab", "projects")
		if dir != expected {
			t.Errorf("DirectorWorkDir() = %q, want %q", dir, expected)
		}
	})

	t.Run("FAB_DIR override", func(t *testing.T) {
		os.Setenv(EnvFabDir, "/tmp/fab-test")
		defer os.Unsetenv(EnvFabDir)

		dir, err := DirectorWorkDir()
		if err != nil {
			t.Fatalf("DirectorWorkDir() error = %v", err)
		}
		expected := "/tmp/fab-test/projects"
		if dir != expected {
			t.Errorf("DirectorWorkDir() = %q, want %q", dir, expected)
		}
	})
}
