package agent

import (
	"regexp"
	"sync"
	"testing"
)

func TestDefaultPatterns(t *testing.T) {
	patterns := DefaultPatterns()

	if len(patterns) == 0 {
		t.Fatal("expected default patterns, got none")
	}

	// Check that all patterns have required fields
	for i, p := range patterns {
		if p.Name == "" {
			t.Errorf("pattern %d has empty name", i)
		}
		if p.Regex == nil {
			t.Errorf("pattern %d (%s) has nil regex", i, p.Name)
		}
	}
}

func TestNewDetector(t *testing.T) {
	t.Run("nil patterns uses defaults", func(t *testing.T) {
		d := NewDetector(nil)
		if len(d.Patterns()) == 0 {
			t.Error("expected default patterns")
		}
	})

	t.Run("empty patterns uses defaults", func(t *testing.T) {
		d := NewDetector([]*Pattern{})
		if len(d.Patterns()) == 0 {
			t.Error("expected default patterns")
		}
	})

	t.Run("custom patterns", func(t *testing.T) {
		custom := []*Pattern{
			{Name: "test", Regex: regexp.MustCompile("test")},
		}
		d := NewDetector(custom)
		if len(d.Patterns()) != 1 {
			t.Errorf("expected 1 pattern, got %d", len(d.Patterns()))
		}
	})
}

func TestNewDefaultDetector(t *testing.T) {
	d := NewDefaultDetector()
	if len(d.Patterns()) == 0 {
		t.Error("expected default patterns")
	}
}

func TestDetector_AddPattern(t *testing.T) {
	d := NewDetector([]*Pattern{})
	initial := len(d.Patterns())

	d.AddPattern(&Pattern{
		Name:  "added",
		Regex: regexp.MustCompile("added"),
	})

	if len(d.Patterns()) != initial+1 {
		t.Errorf("expected %d patterns, got %d", initial+1, len(d.Patterns()))
	}
}

func TestDetector_Check(t *testing.T) {
	d := NewDefaultDetector()

	tests := []struct {
		name        string
		text        string
		wantMatch   bool
		wantPattern string
	}{
		{
			name:        "bd close command",
			text:        "Running: bd close",
			wantMatch:   true,
			wantPattern: "beads_close",
		},
		{
			name:        "bd close with issue ID",
			text:        "Executing bd close FAB-123",
			wantMatch:   true,
			wantPattern: "beads_close",
		},
		{
			name:        "bd close with short ID",
			text:        "bd close fa-abc",
			wantMatch:   true,
			wantPattern: "beads_close",
		},
		{
			name:        "beads skill invocation",
			text:        "Running skill: /beads:close",
			wantMatch:   true,
			wantPattern: "beads_skill_close",
		},
		{
			name:        "task completed message",
			text:        "Task completed successfully",
			wantMatch:   true,
			wantPattern: "task_completed",
		},
		{
			name:        "issue closed message",
			text:        "The issue closed properly",
			wantMatch:   true,
			wantPattern: "task_completed",
		},
		{
			name:        "marked as completed",
			text:        "FAB-42 has been marked as completed",
			wantMatch:   true,
			wantPattern: "task_completed",
		},
		{
			name:      "no match",
			text:      "normal output without completion signals",
			wantMatch: false,
		},
		{
			name:      "empty string",
			text:      "",
			wantMatch: false,
		},
		{
			name:      "partial match - bd without close",
			text:      "bd list shows items",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := d.Check(tt.text)
			if tt.wantMatch {
				if match == nil {
					t.Error("expected match, got nil")
					return
				}
				if match.Pattern.Name != tt.wantPattern {
					t.Errorf("expected pattern %q, got %q", tt.wantPattern, match.Pattern.Name)
				}
			} else {
				if match != nil {
					t.Errorf("expected no match, got %+v", match)
				}
			}
		})
	}
}

func TestDetector_CheckAll(t *testing.T) {
	d := NewDefaultDetector()

	// Text with multiple matches
	text := "Task completed. Running bd close FAB-123"
	matches := d.CheckAll(text)

	if len(matches) < 2 {
		t.Errorf("expected at least 2 matches, got %d", len(matches))
	}

	// Verify we got different patterns
	patterns := make(map[string]bool)
	for _, m := range matches {
		patterns[m.Pattern.Name] = true
	}

	if !patterns["beads_close"] {
		t.Error("expected beads_close match")
	}
	if !patterns["task_completed"] {
		t.Error("expected task_completed match")
	}
}

func TestDetector_CheckBuffer(t *testing.T) {
	d := NewDefaultDetector()

	t.Run("nil buffer", func(t *testing.T) {
		match := d.CheckBuffer(nil)
		if match != nil {
			t.Error("expected nil match for nil buffer")
		}
	})

	t.Run("empty buffer", func(t *testing.T) {
		buf := NewRingBuffer(10)
		match := d.CheckBuffer(buf)
		if match != nil {
			t.Error("expected nil match for empty buffer")
		}
	})

	t.Run("buffer with match", func(t *testing.T) {
		buf := NewRingBuffer(10)
		buf.WriteString("Starting task...\n")
		buf.WriteString("Working on FAB-42\n")
		buf.WriteString("bd close FAB-42\n")
		buf.WriteString("Done\n")

		match := d.CheckBuffer(buf)
		if match == nil {
			t.Fatal("expected match")
		}
		if match.Pattern.Name != "beads_close" {
			t.Errorf("expected beads_close, got %s", match.Pattern.Name)
		}
	})

	t.Run("buffer without match", func(t *testing.T) {
		buf := NewRingBuffer(10)
		buf.WriteString("Normal output\n")
		buf.WriteString("More output\n")

		match := d.CheckBuffer(buf)
		if match != nil {
			t.Errorf("expected no match, got %+v", match)
		}
	})
}

func TestDetector_CheckLines(t *testing.T) {
	d := NewDefaultDetector()

	t.Run("match in lines", func(t *testing.T) {
		lines := [][]byte{
			[]byte("line one"),
			[]byte("bd close FAB-99"),
			[]byte("line three"),
		}
		match := d.CheckLines(lines)
		if match == nil {
			t.Fatal("expected match")
		}
		if match.Pattern.Name != "beads_close" {
			t.Errorf("expected beads_close, got %s", match.Pattern.Name)
		}
	})

	t.Run("no match in lines", func(t *testing.T) {
		lines := [][]byte{
			[]byte("line one"),
			[]byte("line two"),
		}
		match := d.CheckLines(lines)
		if match != nil {
			t.Errorf("expected no match, got %+v", match)
		}
	})

	t.Run("empty lines", func(t *testing.T) {
		match := d.CheckLines([][]byte{})
		if match != nil {
			t.Errorf("expected no match, got %+v", match)
		}
	})

	t.Run("nil lines", func(t *testing.T) {
		match := d.CheckLines(nil)
		if match != nil {
			t.Errorf("expected no match, got %+v", match)
		}
	})
}

func TestCompilePattern(t *testing.T) {
	t.Run("valid regex", func(t *testing.T) {
		p, err := CompilePattern("test", "desc", `\w+`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "test" {
			t.Errorf("expected name 'test', got %q", p.Name)
		}
		if p.Description != "desc" {
			t.Errorf("expected description 'desc', got %q", p.Description)
		}
	})

	t.Run("invalid regex", func(t *testing.T) {
		_, err := CompilePattern("bad", "desc", `[invalid`)
		if err == nil {
			t.Error("expected error for invalid regex")
		}
	})
}

func TestMustCompilePattern(t *testing.T) {
	t.Run("valid regex", func(t *testing.T) {
		p := MustCompilePattern("test", "desc", `\w+`)
		if p.Name != "test" {
			t.Errorf("expected name 'test', got %q", p.Name)
		}
	})

	t.Run("invalid regex panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for invalid regex")
			}
		}()
		MustCompilePattern("bad", "desc", `[invalid`)
	})
}

func TestDetector_Concurrent(t *testing.T) {
	d := NewDefaultDetector()
	var wg sync.WaitGroup

	// Concurrent checks
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Check("bd close FAB-123")
			_ = d.Patterns()
		}()
	}

	// Concurrent adds
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			d.AddPattern(&Pattern{
				Name:  "concurrent",
				Regex: regexp.MustCompile("test"),
			})
		}(i)
	}

	wg.Wait()
}

func TestMatch_Fields(t *testing.T) {
	d := NewDefaultDetector()
	match := d.Check("bd close FAB-42")

	if match == nil {
		t.Fatal("expected match")
	}

	if match.Pattern == nil {
		t.Error("expected pattern to be set")
	}

	if match.Text == "" {
		t.Error("expected matched text to be set")
	}

	// The matched text should contain "bd close"
	if match.Text != "bd close FAB-42" {
		t.Errorf("expected 'bd close FAB-42', got %q", match.Text)
	}
}

func BenchmarkDetector_Check(b *testing.B) {
	d := NewDefaultDetector()
	text := "Working on task... bd close FAB-123 done"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.Check(text)
	}
}

func BenchmarkDetector_CheckBuffer(b *testing.B) {
	d := NewDefaultDetector()
	buf := NewRingBuffer(1000)
	for i := 0; i < 1000; i++ {
		buf.WriteString("output line content\n")
	}
	buf.WriteString("bd close FAB-99\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.CheckBuffer(buf)
	}
}
