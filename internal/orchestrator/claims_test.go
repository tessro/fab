package orchestrator

import (
	"testing"
)

func TestClaimRegistry_Claim(t *testing.T) {
	r := NewClaimRegistry()

	// First claim should succeed
	if err := r.Claim("TICKET-1", "agent-1"); err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Same agent claiming same ticket should be idempotent
	if err := r.Claim("TICKET-1", "agent-1"); err != nil {
		t.Errorf("expected idempotent claim, got %v", err)
	}

	// Different agent claiming same ticket should fail
	err := r.Claim("TICKET-1", "agent-2")
	if err != ErrAlreadyClaimed {
		t.Errorf("expected ErrAlreadyClaimed, got %v", err)
	}

	// Same agent can claim different tickets
	if err := r.Claim("TICKET-2", "agent-1"); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestClaimRegistry_Release(t *testing.T) {
	r := NewClaimRegistry()

	r.Claim("TICKET-1", "agent-1")
	r.Release("TICKET-1")

	// After release, another agent can claim
	if err := r.Claim("TICKET-1", "agent-2"); err != nil {
		t.Errorf("expected claim after release to succeed, got %v", err)
	}
}

func TestClaimRegistry_ReleaseByAgent(t *testing.T) {
	r := NewClaimRegistry()

	r.Claim("TICKET-1", "agent-1")
	r.Claim("TICKET-2", "agent-1")
	r.Claim("TICKET-3", "agent-2")

	released := r.ReleaseByAgent("agent-1")
	if released != 2 {
		t.Errorf("expected 2 claims released, got %d", released)
	}

	// agent-1's tickets should now be available
	if err := r.Claim("TICKET-1", "agent-3"); err != nil {
		t.Errorf("expected claim to succeed, got %v", err)
	}
	if err := r.Claim("TICKET-2", "agent-3"); err != nil {
		t.Errorf("expected claim to succeed, got %v", err)
	}

	// agent-2's ticket should still be claimed
	err := r.Claim("TICKET-3", "agent-3")
	if err != ErrAlreadyClaimed {
		t.Errorf("expected ErrAlreadyClaimed, got %v", err)
	}
}

func TestClaimRegistry_ClaimedBy(t *testing.T) {
	r := NewClaimRegistry()

	r.Claim("TICKET-1", "agent-1")

	if got := r.ClaimedBy("TICKET-1"); got != "agent-1" {
		t.Errorf("expected agent-1, got %s", got)
	}
	if got := r.ClaimedBy("TICKET-2"); got != "" {
		t.Errorf("expected empty string for unclaimed ticket, got %s", got)
	}
}

func TestClaimRegistry_IsClaimed(t *testing.T) {
	r := NewClaimRegistry()

	r.Claim("TICKET-1", "agent-1")

	if !r.IsClaimed("TICKET-1") {
		t.Error("expected TICKET-1 to be claimed")
	}
	if r.IsClaimed("TICKET-2") {
		t.Error("expected TICKET-2 to not be claimed")
	}
}

func TestClaimRegistry_List(t *testing.T) {
	r := NewClaimRegistry()

	r.Claim("TICKET-1", "agent-1")
	r.Claim("TICKET-2", "agent-2")

	claims := r.List()
	if len(claims) != 2 {
		t.Errorf("expected 2 claims, got %d", len(claims))
	}
	if claims["TICKET-1"] != "agent-1" {
		t.Errorf("expected TICKET-1 claimed by agent-1, got %s", claims["TICKET-1"])
	}
	if claims["TICKET-2"] != "agent-2" {
		t.Errorf("expected TICKET-2 claimed by agent-2, got %s", claims["TICKET-2"])
	}

	// Modifying the returned map shouldn't affect the registry
	claims["TICKET-3"] = "agent-3"
	if r.IsClaimed("TICKET-3") {
		t.Error("modifying returned map should not affect registry")
	}
}

func TestClaimRegistry_Count(t *testing.T) {
	r := NewClaimRegistry()

	if r.Count() != 0 {
		t.Errorf("expected 0 claims, got %d", r.Count())
	}

	r.Claim("TICKET-1", "agent-1")
	r.Claim("TICKET-2", "agent-2")

	if r.Count() != 2 {
		t.Errorf("expected 2 claims, got %d", r.Count())
	}
}
