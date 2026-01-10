// Package id provides utilities for generating unique identifiers.
package id

import (
	"crypto/rand"
	"encoding/hex"
)

// Generate returns a random 6-character hex ID.
func Generate() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
