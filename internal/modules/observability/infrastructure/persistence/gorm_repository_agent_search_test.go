package persistence

import (
	"testing"
	"time"

	"hyperstrate/server/internal/modules/observability/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInferenceLogAgentSearchIsLoose(t *testing.T) {
	db := newObservabilitySearchTestDB(t)
	repo := NewInferenceLogRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	logs := []domain.InferenceLog{
		{ID: "log_claude", OrgID: "org_1", AgentSessionID: "org_1:claude_code:user_a:sess_1", Agent: "claude_code", CreatedAt: now},
		{ID: "log_copilot", OrgID: "org_1", AgentSessionID: "org_1:github_copilot:user_a:sess_2", Agent: "github_copilot", CreatedAt: now.Add(time.Minute)},
		{ID: "log_jetbrains", OrgID: "org_1", AgentSessionID: "org_1:jetbrains_ai:user_a:sess_3", Agent: "jetbrains_ai", CreatedAt: now.Add(2 * time.Minute)},
	}
	for i := range logs {
		if err := repo.Create(&logs[i]); err != nil {
			t.Fatalf("create log %s: %v", logs[i].ID, err)
		}
	}

	tests := []struct {
		name     string
		query    string
		wantLogs []string
	}{
		{name: "space separated query matches underscore agent", query: "Claude Code", wantLogs: []string{"log_claude"}},
		{name: "dash separated query matches underscore agent", query: "claude-code", wantLogs: []string{"log_claude"}},
		{name: "compacted query matches separated agent", query: "githubcopilot", wantLogs: []string{"log_copilot"}},
		{name: "partial query matches agent token", query: "copilot", wantLogs: []string{"log_copilot"}},
		{name: "space separated IDE query matches underscore agent", query: "jetbrains ai", wantLogs: []string{"log_jetbrains"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := repo.List(domain.InferenceLogFilter{OrgID: "org_1", Agent: tt.query}, 20, 0)
			if err != nil {
				t.Fatalf("list logs: %v", err)
			}
			if total != int64(len(tt.wantLogs)) {
				t.Fatalf("total = %d, want %d", total, len(tt.wantLogs))
			}
			if len(got) != len(tt.wantLogs) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(tt.wantLogs))
			}
			for i, want := range tt.wantLogs {
				if got[i].ID != want {
					t.Fatalf("got[%d].ID = %q, want %q", i, got[i].ID, want)
				}
			}
		})
	}
}

func TestAgentSessionSearchUsesLooseAgentFilter(t *testing.T) {
	db := newObservabilitySearchTestDB(t)
	repo := NewInferenceLogRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	for _, log := range []domain.InferenceLog{
		{ID: "log_claude", OrgID: "org_1", AgentSessionID: "org_1:claude_code:user_a:sess_1", Agent: "claude_code", CreatedAt: now},
		{ID: "log_codex", OrgID: "org_1", AgentSessionID: "org_1:codex:user_a:sess_2", Agent: "codex", CreatedAt: now.Add(time.Minute)},
	} {
		entry := log
		if err := repo.Create(&entry); err != nil {
			t.Fatalf("create log %s: %v", entry.ID, err)
		}
	}

	sessions, total, err := repo.ListAgentSessions(domain.InferenceLogFilter{OrgID: "org_1", Agent: "claude code"}, 20, 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if total != 1 || len(sessions) != 1 {
		t.Fatalf("got total=%d len=%d, want one matching session", total, len(sessions))
	}
	if sessions[0].Agent != "claude_code" {
		t.Fatalf("agent = %q, want claude_code", sessions[0].Agent)
	}
}

func TestLooseAgentSearchDoesNotBypassOrgFilter(t *testing.T) {
	db := newObservabilitySearchTestDB(t)
	repo := NewInferenceLogRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	for _, log := range []domain.InferenceLog{
		{ID: "log_org_1", OrgID: "org_1", AgentSessionID: "org_1:codex:user_a:sess_1", Agent: "codex", CreatedAt: now},
		{ID: "log_org_2", OrgID: "org_2", AgentSessionID: "org_2:claude_code:user_a:sess_1", Agent: "claude_code", CreatedAt: now.Add(time.Minute)},
	} {
		entry := log
		if err := repo.Create(&entry); err != nil {
			t.Fatalf("create log %s: %v", entry.ID, err)
		}
	}

	logs, total, err := repo.List(domain.InferenceLogFilter{OrgID: "org_1", Agent: "claude code"}, 20, 0)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if total != 0 || len(logs) != 0 {
		t.Fatalf("got total=%d len=%d, want no cross-org matches", total, len(logs))
	}
}

func TestAgentSessionEventSearchUsesLooseAgentFilter(t *testing.T) {
	db := newObservabilitySearchTestDB(t)
	repo := NewAgentSessionEventRepository(db)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	for _, event := range []domain.AgentSessionEvent{
		{ID: "event_cursor", OrgID: "org_1", AgentSessionID: "sess_1", Agent: "cursor_ide", EventType: "checkpoint_created", CreatedAt: now},
		{ID: "event_windsurf", OrgID: "org_1", AgentSessionID: "sess_2", Agent: "windsurf", EventType: "session_end", CreatedAt: now.Add(time.Minute)},
	} {
		entry := event
		if err := repo.Create(&entry); err != nil {
			t.Fatalf("create event %s: %v", entry.ID, err)
		}
	}

	events, total, err := repo.List(domain.InferenceLogFilter{OrgID: "org_1", Agent: "cursor ide"}, 20, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("got total=%d len=%d, want one matching event", total, len(events))
	}
	if events[0].ID != "event_cursor" {
		t.Fatalf("event ID = %q, want event_cursor", events[0].ID)
	}
}

func newObservabilitySearchTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&domain.InferenceLog{}, &domain.AgentSessionEvent{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}
