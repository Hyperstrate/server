package main

import (
	"strings"
	"testing"
)

func TestSchemaForDialectIncludesAllPersistedTables(t *testing.T) {
	stmts, err := schemaForDialect("sqlite")
	if err != nil {
		t.Fatalf("schemaForDialect returned error: %v", err)
	}

	for _, table := range []string{
		"prompts",
		"prompt_versions",
		"mcp_servers",
		"model_key_rotations",
		"router_configurations",
		"router_team_accesses",
		"router_evaluations",
		"router_evaluation_cases",
		"router_evaluation_runs",
		"auth_oidc_group_mappings",
		"webhook_deliveries",
		"inference_payloads",
		"agent_session_events",
		"tool_call_archives",
		"compression_events",
	} {
		if !strings.Contains(stmts, table) {
			t.Fatalf("generated schema missing table %q", table)
		}
	}
}
