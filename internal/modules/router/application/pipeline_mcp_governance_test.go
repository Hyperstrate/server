package application

import (
	"context"
	"testing"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
)

func TestMCPGovernanceAllowsAnyCallerTeam(t *testing.T) {
	ctx := authDomain.WithCallerTeamIDs(context.Background(), "team_a", "team_allowed")
	cfg := map[string]any{"allowed_team_ids": []any{"team_allowed"}}

	blocked, detail := mcpGovernanceBlocked(ctx, cfg, nil)

	if blocked {
		t.Fatalf("mcpGovernanceBlocked blocked multi-team caller: %s", detail)
	}
}
