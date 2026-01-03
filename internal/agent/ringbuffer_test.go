package agent

import (
	"bytes"
	"sync"
	"testing"
)

func TestNewRingBuffer(t *testing.T) {
	t.Run("default size", func(t *testing.T) {
		rb := NewRingBuffer(0)
		if rb.Cap() != DefaultBufferSize {
			t.Errorf("expected capacity %d, got %d", DefaultBufferSize, rb.Cap())
		}
	})

	t.Run("custom size", func(t *testing.T) {
		rb := NewRingBuffer(100)
		if rb.Cap() != 100 {
			t.Errorf("expected capacity 100, got %d", rb.Cap())
		}
	})

	t.Run("negative size uses default", func(t *testing.T) {
		rb := NewRingBuffer(-5)
		if rb.Cap() != DefaultBufferSize {
			t.Errorf("expected capacity %d, got %d", DefaultBufferSize, rb.Cap())
		}
	})
}

func TestRingBuffer_Write(t *testing.T) {
	t.Run("single line", func(t *testing.T) {
		rb := NewRingBuffer(10)
		n, err := rb.Write([]byte("hello\n"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 6 {
			t.Errorf("expected 6 bytes written, got %d", n)
		}
		if rb.Len() != 1 {
			t.Errorf("expected 1 line, got %d", rb.Len())
		}
	})

	t.Run("multiple lines", func(t *testing.T) {
		rb := NewRingBuffer(10)
		rb.Write([]byte("line1\nline2\nline3\n"))
		if rb.Len() != 3 {
			t.Errorf("expected 3 lines, got %d", rb.Len())
		}
	})

	t.Run("partial line", func(t *testing.T) {
		rb := NewRingBuffer(10)
		rb.Write([]byte("partial"))
		if rb.Len() != 0 {
			t.Errorf("expected 0 lines before newline, got %d", rb.Len())
		}
		rb.Write([]byte(" continued\n"))
		if rb.Len() != 1 {
			t.Errorf("expected 1 line after newline, got %d", rb.Len())
		}
		lines := rb.Lines(1)
		if string(lines[0]) != "partial continued" {
			t.Errorf("expected 'partial continued', got '%s'", lines[0])
		}
	})

	t.Run("empty write", func(t *testing.T) {
		rb := NewRingBuffer(10)
		n, err := rb.Write([]byte{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 0 {
			t.Errorf("expected 0 bytes, got %d", n)
		}
	})
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer(3)

	// Write 5 lines to a buffer of capacity 3
	for i := 0; i < 5; i++ {
		rb.WriteString("line" + string(rune('A'+i)) + "\n")
	}

	if rb.Len() != 3 {
		t.Errorf("expected 3 lines (capacity), got %d", rb.Len())
	}

	lines := rb.Lines(0) // Get all
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Should have the last 3 lines: C, D, E
	expected := []string{"lineC", "lineD", "lineE"}
	for i, want := range expected {
		if string(lines[i]) != want {
			t.Errorf("line %d: expected '%s', got '%s'", i, want, lines[i])
		}
	}
}

func TestRingBuffer_Lines(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.WriteString("one\ntwo\nthree\nfour\nfive\n")

	t.Run("get all", func(t *testing.T) {
		lines := rb.Lines(0)
		if len(lines) != 5 {
			t.Errorf("expected 5 lines, got %d", len(lines))
		}
	})

	t.Run("get last 2", func(t *testing.T) {
		lines := rb.Lines(2)
		if len(lines) != 2 {
			t.Errorf("expected 2 lines, got %d", len(lines))
		}
		if string(lines[0]) != "four" {
			t.Errorf("expected 'four', got '%s'", lines[0])
		}
		if string(lines[1]) != "five" {
			t.Errorf("expected 'five', got '%s'", lines[1])
		}
	})

	t.Run("request more than available", func(t *testing.T) {
		lines := rb.Lines(100)
		if len(lines) != 5 {
			t.Errorf("expected 5 lines (all available), got %d", len(lines))
		}
	})

	t.Run("empty buffer", func(t *testing.T) {
		empty := NewRingBuffer(10)
		lines := empty.Lines(5)
		if lines != nil {
			t.Errorf("expected nil for empty buffer, got %v", lines)
		}
	})
}

func TestRingBuffer_Last(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.WriteString("alpha\nbeta\ngamma\n")

	result := rb.Last(2)
	expected := "beta\ngamma\n"
	if string(result) != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestRingBuffer_Flush(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.WriteString("no newline at end")

	if rb.Len() != 0 {
		t.Errorf("expected 0 lines before flush, got %d", rb.Len())
	}

	rb.Flush()

	if rb.Len() != 1 {
		t.Errorf("expected 1 line after flush, got %d", rb.Len())
	}

	lines := rb.Lines(1)
	if string(lines[0]) != "no newline at end" {
		t.Errorf("expected 'no newline at end', got '%s'", lines[0])
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.WriteString("line1\nline2\nline3\n")

	if rb.Len() != 3 {
		t.Fatalf("expected 3 lines, got %d", rb.Len())
	}

	rb.Clear()

	if rb.Len() != 0 {
		t.Errorf("expected 0 lines after clear, got %d", rb.Len())
	}
}

func TestRingBuffer_Contains(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.WriteString("error: something went wrong\n")
	rb.WriteString("info: all is well\n")

	if !rb.ContainsString("error:") {
		t.Error("expected to find 'error:'")
	}

	if !rb.ContainsString("all is well") {
		t.Error("expected to find 'all is well'")
	}

	if rb.ContainsString("not present") {
		t.Error("should not find 'not present'")
	}

	// Test partial line search
	rb.WriteString("partial without newline")
	if !rb.ContainsString("partial") {
		t.Error("expected to find 'partial' in partial line")
	}
}

func TestRingBuffer_Partial(t *testing.T) {
	rb := NewRingBuffer(10)

	// No partial initially
	if rb.Partial() != nil {
		t.Error("expected nil partial initially")
	}

	// Write partial line
	rb.WriteString("incomplete")
	partial := rb.Partial()
	if string(partial) != "incomplete" {
		t.Errorf("expected 'incomplete', got '%s'", partial)
	}

	// Complete the line
	rb.WriteString(" line\n")
	if rb.Partial() != nil {
		t.Error("expected nil partial after newline")
	}
}

func TestRingBuffer_Stats(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.WriteString("line1\nline2\n")
	rb.WriteString("partial")

	stats := rb.Stats()
	if stats.Lines != 2 {
		t.Errorf("expected 2 lines, got %d", stats.Lines)
	}
	if stats.Capacity != 5 {
		t.Errorf("expected capacity 5, got %d", stats.Capacity)
	}
	if stats.BytesIn != 19 { // "line1\nline2\n" + "partial" = 12 + 7 = 19 bytes
		t.Errorf("expected 19 bytes in, got %d", stats.BytesIn)
	}
	if !stats.HasPartial {
		t.Error("expected HasPartial to be true")
	}
}

func TestRingBuffer_Concurrent(t *testing.T) {
	rb := NewRingBuffer(1000)
	var wg sync.WaitGroup

	// Multiple writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rb.WriteString("writer goroutine output\n")
			}
		}(i)
	}

	// Multiple readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = rb.Lines(10)
				_ = rb.Len()
				_ = rb.Stats()
			}
		}()
	}

	wg.Wait()

	// Buffer should have 1000 lines (capacity limit)
	if rb.Len() != 1000 {
		t.Errorf("expected 1000 lines after concurrent writes, got %d", rb.Len())
	}
}

func TestRingBuffer_IOWriter(t *testing.T) {
	rb := NewRingBuffer(10)

	// Should work with io.Copy
	src := bytes.NewReader([]byte("from\nio.Copy\n"))
	n, err := src.WriteTo(rb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 13 {
		t.Errorf("expected 13 bytes copied, got %d", n)
	}
	if rb.Len() != 2 {
		t.Errorf("expected 2 lines, got %d", rb.Len())
	}
}

func BenchmarkRingBuffer_Write(b *testing.B) {
	rb := NewRingBuffer(10000)
	line := []byte("benchmark line with some content\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Write(line)
	}
}

func BenchmarkRingBuffer_Lines(b *testing.B) {
	rb := NewRingBuffer(10000)
	for i := 0; i < 10000; i++ {
		rb.WriteString("benchmark line content\n")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rb.Lines(100)
	}
}
