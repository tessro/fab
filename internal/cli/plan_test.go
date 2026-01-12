package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tessro/fab/internal/paths"
)

func TestPlanWrite(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("FAB_DIR", tmpDir)
	defer os.Unsetenv("FAB_DIR")

	t.Run("requires FAB_AGENT_ID", func(t *testing.T) {
		os.Unsetenv("FAB_AGENT_ID")
		err := runPlanWrite(planWriteCmd, nil)
		if err == nil {
			t.Fatal("expected error when FAB_AGENT_ID not set")
		}
		if !strings.Contains(err.Error(), "FAB_AGENT_ID") {
			t.Errorf("error should mention FAB_AGENT_ID, got: %v", err)
		}
	})

	t.Run("writes plan file", func(t *testing.T) {
		os.Setenv("FAB_AGENT_ID", "test-agent-123")
		defer os.Unsetenv("FAB_AGENT_ID")

		// Create a pipe to simulate stdin
		oldStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r

		content := "# Test Plan\n\nThis is a test plan."
		go func() {
			_, _ = w.Write([]byte(content))
			w.Close()
		}()

		// Capture stdout
		oldStdout := os.Stdout
		outR, outW, _ := os.Pipe()
		os.Stdout = outW

		err := runPlanWrite(planWriteCmd, nil)

		os.Stdin = oldStdin
		outW.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runPlanWrite() error = %v", err)
		}

		// Check stdout output (should print plan ID)
		var outBuf bytes.Buffer
		_, _ = outBuf.ReadFrom(outR)
		output := strings.TrimSpace(outBuf.String())
		if output != "test-agent-123" {
			t.Errorf("expected output 'test-agent-123', got %q", output)
		}

		// Verify file was written
		planPath, _ := paths.PlanPath("test-agent-123")
		data, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatalf("failed to read plan file: %v", err)
		}
		if string(data) != content {
			t.Errorf("plan content mismatch: got %q, want %q", string(data), content)
		}
	})

	t.Run("strips plan: prefix from agent ID", func(t *testing.T) {
		os.Setenv("FAB_AGENT_ID", "plan:abc123")
		defer os.Unsetenv("FAB_AGENT_ID")

		oldStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r

		go func() {
			_, _ = w.Write([]byte("test content"))
			w.Close()
		}()

		oldStdout := os.Stdout
		outR, outW, _ := os.Pipe()
		os.Stdout = outW

		err := runPlanWrite(planWriteCmd, nil)

		os.Stdin = oldStdin
		outW.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runPlanWrite() error = %v", err)
		}

		var outBuf bytes.Buffer
		_, _ = outBuf.ReadFrom(outR)
		output := strings.TrimSpace(outBuf.String())
		if output != "abc123" {
			t.Errorf("expected output 'abc123', got %q", output)
		}

		// Verify file was written with stripped ID
		planPath, _ := paths.PlanPath("abc123")
		if _, err := os.Stat(planPath); os.IsNotExist(err) {
			t.Error("plan file should exist at abc123.md")
		}
	})
}

func TestPlanRead(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("FAB_DIR", tmpDir)
	defer os.Unsetenv("FAB_DIR")

	t.Run("reads existing plan", func(t *testing.T) {
		// Write a test plan
		plansDir, _ := paths.PlansDir()
		if err := os.MkdirAll(plansDir, 0755); err != nil {
			t.Fatalf("failed to create plans dir: %v", err)
		}
		planPath := filepath.Join(plansDir, "test-plan.md")
		content := "# Test Plan Content"
		if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test plan: %v", err)
		}

		// Capture stdout
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := runPlanRead(planReadCmd, []string{"test-plan"})

		w.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runPlanRead() error = %v", err)
		}

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		output := buf.String()
		if output != content {
			t.Errorf("expected %q, got %q", content, output)
		}
	})

	t.Run("returns error for non-existent plan", func(t *testing.T) {
		err := runPlanRead(planReadCmd, []string{"nonexistent"})
		if err == nil {
			t.Fatal("expected error for non-existent plan")
		}
		if !strings.Contains(err.Error(), "plan not found") {
			t.Errorf("error should mention 'plan not found', got: %v", err)
		}
	})
}

func TestPlanList(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("FAB_DIR", tmpDir)
	defer os.Unsetenv("FAB_DIR")

	t.Run("shows message when no plans", func(t *testing.T) {
		// Capture stdout
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := runPlanList(planListCmd, nil)

		w.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runPlanList() error = %v", err)
		}

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		output := strings.TrimSpace(buf.String())
		if output != "No stored plans" {
			t.Errorf("expected 'No stored plans', got %q", output)
		}
	})

	t.Run("lists stored plans", func(t *testing.T) {
		// Create test plans
		plansDir, _ := paths.PlansDir()
		if err := os.MkdirAll(plansDir, 0755); err != nil {
			t.Fatalf("failed to create plans dir: %v", err)
		}

		// Create plans with different timestamps
		if err := os.WriteFile(filepath.Join(plansDir, "plan-a.md"), []byte("plan a"), 0644); err != nil {
			t.Fatalf("failed to write plan-a: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		if err := os.WriteFile(filepath.Join(plansDir, "plan-b.md"), []byte("plan b"), 0644); err != nil {
			t.Fatalf("failed to write plan-b: %v", err)
		}

		// Capture stdout
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := runPlanList(planListCmd, nil)

		w.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runPlanList() error = %v", err)
		}

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		output := buf.String()

		// Check that header and both plans are listed
		if !strings.Contains(output, "ID") {
			t.Error("output should contain ID header")
		}
		if !strings.Contains(output, "plan-a") {
			t.Error("output should contain plan-a")
		}
		if !strings.Contains(output, "plan-b") {
			t.Error("output should contain plan-b")
		}
	})

	t.Run("ignores non-md files", func(t *testing.T) {
		plansDir, _ := paths.PlansDir()
		if err := os.MkdirAll(plansDir, 0755); err != nil {
			t.Fatalf("failed to create plans dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(plansDir, "test.txt"), []byte("not a plan"), 0644); err != nil {
			t.Fatalf("failed to write test.txt: %v", err)
		}
		if err := os.WriteFile(filepath.Join(plansDir, "plan-only.md"), []byte("real plan"), 0644); err != nil {
			t.Fatalf("failed to write plan-only.md: %v", err)
		}

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := runPlanList(planListCmd, nil)

		w.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runPlanList() error = %v", err)
		}

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		output := buf.String()

		if strings.Contains(output, "test.txt") {
			t.Error("output should not contain non-md files")
		}
		if !strings.Contains(output, "plan-only") {
			t.Error("output should contain plan-only")
		}
	})
}
