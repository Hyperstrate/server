package application

import "testing"

func TestAgentSessionMetaCanonicalizesAgentSessionID(t *testing.T) {
	options := map[string]any{
		"agent_session_id":        "local-session-1",
		"parent_agent_session_id": "parent-session-1",
		"agent":                   "codex",
		"agent_user_id":           "user_a",
	}

	meta := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", options)
	again := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", options)

	if meta.SessionID == "" || meta.SessionID == "local-session-1" {
		t.Fatalf("expected canonical session id, got %q", meta.SessionID)
	}
	if meta.SessionID != again.SessionID {
		t.Fatalf("expected stable canonical id, got %q then %q", meta.SessionID, again.SessionID)
	}
	if meta.ParentSessionID == "" || meta.ParentSessionID == "parent-session-1" {
		t.Fatalf("expected canonical parent session id, got %q", meta.ParentSessionID)
	}
	if meta.Agent != "codex" {
		t.Fatalf("expected agent to remain observable, got %q", meta.Agent)
	}
	if meta.UserID != "user_a" {
		t.Fatalf("expected user id to remain observable, got %q", meta.UserID)
	}
}

func TestAgentSessionMetaUsesParentAgentForRawParentSessionID(t *testing.T) {
	options := map[string]any{
		"agent_session_id":        "worker-raw",
		"agent":                   "codex",
		"agent_user_id":           "worker-user",
		"parent_agent_session_id": "parent-raw",
		"parent_agent":            "claude_code",
		"parent_agent_user_id":    "parent-user",
	}

	meta := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", options)
	parent := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", map[string]any{
		"agent_session_id": "parent-raw",
		"agent":            "claude_code",
		"agent_user_id":    "parent-user",
	})

	if meta.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", meta.Agent)
	}
	if meta.ParentSessionID != parent.SessionID {
		t.Fatalf("ParentSessionID = %q, want %q", meta.ParentSessionID, parent.SessionID)
	}
}

func TestAgentSessionMetaSeparatesSameRawSessionAcrossIsolationDimensions(t *testing.T) {
	base := map[string]any{
		"agent_session_id": "shared-local-session",
		"agent":            "codex",
		"agent_user_id":    "user_a",
	}

	baseID := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", base).SessionID
	cases := []struct {
		name    string
		orgID   string
		options map[string]any
	}{
		{
			name:  "different org",
			orgID: "org_b",
			options: map[string]any{
				"agent_session_id": "shared-local-session",
				"agent":            "codex",
				"agent_user_id":    "user_a",
			},
		},
		{
			name:  "different agent",
			orgID: "org_a",
			options: map[string]any{
				"agent_session_id": "shared-local-session",
				"agent":            "claude_code",
				"agent_user_id":    "user_a",
			},
		},
		{
			name:  "different user",
			orgID: "org_a",
			options: map[string]any{
				"agent_session_id": "shared-local-session",
				"agent":            "codex",
				"agent_user_id":    "user_b",
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := agentSessionMetaFromOptions(tt.orgID, "vk_a", "team_a", tt.options).SessionID
			if got == baseID {
				t.Fatalf("expected isolated session id, got same %q", got)
			}
		})
	}
}

func TestAgentSessionMetaUsesVirtualKeyFallbackForAnonymousAgentUser(t *testing.T) {
	options := map[string]any{
		"agent_session_id": "shared-local-session",
		"agent":            "cursor",
	}

	vkA := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", options).SessionID
	vkB := agentSessionMetaFromOptions("org_a", "vk_b", "team_a", options).SessionID

	if vkA == "" || vkB == "" {
		t.Fatal("expected canonical session ids")
	}
	if vkA == vkB {
		t.Fatalf("expected anonymous sessions to be isolated by virtual key fallback, got %q", vkA)
	}
}

func TestAgentSessionMetaKeepsCanonicalParentSessionID(t *testing.T) {
	options := map[string]any{
		"agent_session_id":        "worker-raw",
		"agent":                   "codex",
		"agent_user_id":           "worker-user",
		"parent_agent_session_id": "asess_1234567890abcdef1234567890abcdef",
		"parent_agent":            "claude_code",
		"parent_agent_user_id":    "parent-user",
	}

	meta := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", options)

	if meta.ParentSessionID != "asess_1234567890abcdef1234567890abcdef" {
		t.Fatalf("expected canonical parent id to round-trip, got %q", meta.ParentSessionID)
	}
}

func TestAgentSessionMetaUsesParentIdentityForRawParentSessionID(t *testing.T) {
	options := map[string]any{
		"agent_session_id":        "worker-raw",
		"agent":                   "codex",
		"agent_user_id":           "worker-user",
		"parent_agent_session_id": "parent-raw",
		"parent_agent":            "claude_code",
		"parent_agent_user_id":    "parent-user",
	}

	meta := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", options)
	parentWithParentIdentity := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", map[string]any{
		"agent_session_id": "parent-raw",
		"agent":            "claude_code",
		"agent_user_id":    "parent-user",
	})
	parentWithChildIdentity := agentSessionMetaFromOptions("org_a", "vk_a", "team_a", map[string]any{
		"agent_session_id": "parent-raw",
		"agent":            "codex",
		"agent_user_id":    "worker-user",
	})

	if meta.ParentSessionID != parentWithParentIdentity.SessionID {
		t.Fatalf("expected parent identity canonical id %q, got %q", parentWithParentIdentity.SessionID, meta.ParentSessionID)
	}
	if meta.ParentSessionID == parentWithChildIdentity.SessionID {
		t.Fatalf("parent session used child identity unexpectedly: %q", meta.ParentSessionID)
	}
}
