package id

import "testing"

func TestGenerate(t *testing.T) {
	id := Generate()

	// Should be 6 characters (3 bytes = 6 hex chars)
	if len(id) != 6 {
		t.Errorf("expected ID length 6, got %d", len(id))
	}

	// Should be valid hex
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("expected hex character, got %c", c)
		}
	}
}

func TestGenerate_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		id := Generate()
		if seen[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}
}
