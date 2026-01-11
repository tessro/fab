package tui

import "testing"

func TestInputLine_AddToHistory(t *testing.T) {
	il := NewInputLine()

	// Empty string should not be added
	il.AddToHistory("")
	if len(il.history) != 0 {
		t.Error("empty string should not be added to history")
	}

	// Add some entries
	il.AddToHistory("first")
	il.AddToHistory("second")
	il.AddToHistory("third")

	if len(il.history) != 3 {
		t.Errorf("history length = %d, want 3", len(il.history))
	}

	// Check order (oldest first)
	if il.history[0] != "first" {
		t.Errorf("history[0] = %q, want %q", il.history[0], "first")
	}
	if il.history[2] != "third" {
		t.Errorf("history[2] = %q, want %q", il.history[2], "third")
	}
}

func TestInputLine_AddToHistory_DuplicatePrevention(t *testing.T) {
	il := NewInputLine()

	il.AddToHistory("same")
	il.AddToHistory("same")

	if len(il.history) != 1 {
		t.Errorf("duplicate entry should not be added, got length %d", len(il.history))
	}

	// Non-consecutive duplicates should be added
	il.AddToHistory("different")
	il.AddToHistory("same")
	if len(il.history) != 3 {
		t.Errorf("non-consecutive duplicate should be added, got length %d", len(il.history))
	}
}

func TestInputLine_AddToHistory_MaxSize(t *testing.T) {
	il := NewInputLine()

	// Add more than maxHistorySize entries
	for i := 0; i < maxHistorySize+10; i++ {
		il.AddToHistory(string(rune('a' + i%26)))
	}

	if len(il.history) > maxHistorySize {
		t.Errorf("history length = %d, should not exceed %d", len(il.history), maxHistorySize)
	}
}

func TestInputLine_HistoryNavigation(t *testing.T) {
	il := NewInputLine()

	// Navigation with no history should return false
	if il.HistoryUp() {
		t.Error("HistoryUp should return false with empty history")
	}
	if il.HistoryDown() {
		t.Error("HistoryDown should return false with empty history")
	}

	// Add some history
	il.AddToHistory("first")
	il.AddToHistory("second")
	il.AddToHistory("third")

	// Set current input
	il.input.SetValue("current")

	// Navigate up - should get "third" (most recent)
	if !il.HistoryUp() {
		t.Error("HistoryUp should return true")
	}
	if il.Value() != "third" {
		t.Errorf("after HistoryUp, value = %q, want %q", il.Value(), "third")
	}

	// Navigate up again - should get "second"
	if !il.HistoryUp() {
		t.Error("HistoryUp should return true")
	}
	if il.Value() != "second" {
		t.Errorf("after second HistoryUp, value = %q, want %q", il.Value(), "second")
	}

	// Navigate up again - should get "first"
	if !il.HistoryUp() {
		t.Error("HistoryUp should return true")
	}
	if il.Value() != "first" {
		t.Errorf("after third HistoryUp, value = %q, want %q", il.Value(), "first")
	}

	// Navigate up at oldest - should return false and stay at "first"
	if il.HistoryUp() {
		t.Error("HistoryUp at oldest entry should return false")
	}
	if il.Value() != "first" {
		t.Errorf("value should still be %q, got %q", "first", il.Value())
	}

	// Navigate down - should get "second"
	if !il.HistoryDown() {
		t.Error("HistoryDown should return true")
	}
	if il.Value() != "second" {
		t.Errorf("after HistoryDown, value = %q, want %q", il.Value(), "second")
	}

	// Navigate down to "third"
	il.HistoryDown()
	if il.Value() != "third" {
		t.Errorf("value = %q, want %q", il.Value(), "third")
	}

	// Navigate down past newest - should restore original input
	if !il.HistoryDown() {
		t.Error("HistoryDown past newest should return true")
	}
	if il.Value() != "current" {
		t.Errorf("value = %q, want %q (original)", il.Value(), "current")
	}

	// Navigate down again when not browsing - should return false
	if il.HistoryDown() {
		t.Error("HistoryDown when not browsing should return false")
	}
}

func TestInputLine_ResetHistoryNavigation(t *testing.T) {
	il := NewInputLine()

	il.AddToHistory("entry")
	il.input.SetValue("current")

	// Start navigating
	il.HistoryUp()

	// Reset
	il.ResetHistoryNavigation()

	if il.historyIndex != -1 {
		t.Errorf("historyIndex = %d, want -1", il.historyIndex)
	}
	if il.savedInput != "" {
		t.Errorf("savedInput = %q, want empty", il.savedInput)
	}
}

func TestInputLine_ContentHeight(t *testing.T) {
	il := NewInputLine()

	// Initial height should be 1
	if il.ContentHeight() != 1 {
		t.Errorf("initial ContentHeight = %d, want 1", il.ContentHeight())
	}

	// After inserting a newline, height should be 2
	il.InsertNewline()
	if il.ContentHeight() != 2 {
		t.Errorf("after InsertNewline, ContentHeight = %d, want 2", il.ContentHeight())
	}

	// Insert more newlines up to max
	for i := 0; i < maxInputHeight; i++ {
		il.InsertNewline()
	}

	// Height should be capped at maxInputHeight
	if il.ContentHeight() != maxInputHeight {
		t.Errorf("ContentHeight = %d, should be capped at %d", il.ContentHeight(), maxInputHeight)
	}

	// Clear should reset height
	il.Clear()
	if il.ContentHeight() != 1 {
		t.Errorf("after Clear, ContentHeight = %d, want 1", il.ContentHeight())
	}
}
