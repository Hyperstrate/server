package agentsession

import "testing"

func TestCanonicalIDIsStableAndIsolated(t *testing.T) {
	base := CanonicalID("org_a", "codex", "user_a", "session_1")
	if base == "" || base == "session_1" {
		t.Fatalf("expected opaque canonical id, got %q", base)
	}
	if again := CanonicalID("org_a", "codex", "user_a", "session_1"); again != base {
		t.Fatalf("expected stable id, got %q then %q", base, again)
	}
	if got := CanonicalID("org_b", "codex", "user_a", "session_1"); got == base {
		t.Fatalf("expected org isolation, got %q", got)
	}
	if got := CanonicalID("org_a", "cursor", "user_a", "session_1"); got == base {
		t.Fatalf("expected client isolation, got %q", got)
	}
	if got := CanonicalID("org_a", "codex", "user_b", "session_1"); got == base {
		t.Fatalf("expected actor isolation, got %q", got)
	}
}

func TestCanonicalIDPassesThroughCanonicalID(t *testing.T) {
	const canonical = "asess_1234567890abcdef1234567890abcdef"
	if got := CanonicalID("org_a", "codex", "user_a", canonical); got != canonical {
		t.Fatalf("expected canonical id passthrough, got %q", got)
	}
}
