package application_test

// Org-isolation security tests for the prompt service.

import (
	"context"
	"testing"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/prompts/application"
	"hyperstrate/server/internal/modules/prompts/domain"
	"hyperstrate/server/internal/shared/pagination"
)

// ── org-aware prompt repo stub ────────────────────────────────────────────────

type orgPromptRepo struct {
	prompt *domain.Prompt
}

func (r *orgPromptRepo) List(_ context.Context, orgID, _ string, _, _ int) ([]domain.Prompt, int64, error) {
	if r.prompt == nil || r.prompt.OrgID != orgID {
		return nil, 0, nil
	}
	return []domain.Prompt{*r.prompt}, 1, nil
}

func (r *orgPromptRepo) Create(_ context.Context, p *domain.Prompt) error {
	r.prompt = p
	return nil
}

func (r *orgPromptRepo) FindByID(_ context.Context, orgID, id string) (*domain.Prompt, error) {
	if r.prompt == nil || r.prompt.OrgID != orgID || r.prompt.ID != id {
		return nil, domain.ErrPromptNotFound
	}
	return r.prompt, nil
}

func (r *orgPromptRepo) Update(_ context.Context, p *domain.Prompt) error {
	r.prompt = p
	return nil
}

func (r *orgPromptRepo) Delete(_ context.Context, orgID, id string) error {
	if r.prompt == nil || r.prompt.OrgID != orgID || r.prompt.ID != id {
		return domain.ErrPromptNotFound
	}
	r.prompt = nil
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

const (
	promptOrgA = "org-alpha"
	promptOrgB = "org-bravo"
)

func promptCtx(orgID string) context.Context {
	return authDomain.WithOrgID(context.Background(), orgID)
}

func promptOwnedByOrgA() *domain.Prompt {
	return &domain.Prompt{ID: "prm_a1", OrgID: promptOrgA, Name: "system-prompt-a", Content: "You are helpful."}
}

func buildPromptService(repo domain.PromptRepository) application.Service {
	return application.NewService(repo, &noopVersionRepo{}, application.NewPromptEventBus())
}

type noopVersionRepo struct{}

func (r *noopVersionRepo) Create(_ context.Context, _ *domain.PromptVersion) error { return nil }
func (r *noopVersionRepo) ListByPromptID(_ context.Context, _, _ string, _, _ int) ([]domain.PromptVersion, int64, error) {
	return nil, 0, nil
}
func (r *noopVersionRepo) FindByID(_ context.Context, _, _ string) (*domain.PromptVersion, error) {
	return nil, domain.ErrPromptVersionNotFound
}
func (r *noopVersionRepo) LatestVersion(_ context.Context, _ string) (int, error) { return 0, nil }

// ── tests ─────────────────────────────────────────────────────────────────────

func TestOrgIsolation_GetPrompt_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildPromptService(&orgPromptRepo{prompt: promptOwnedByOrgA()})

	_, err := svc.GetPrompt(promptCtx(promptOrgB), "prm_a1")
	if err != domain.ErrPromptNotFound {
		t.Errorf("want ErrPromptNotFound when org-B reads org-A prompt, got %v", err)
	}
}

func TestOrgIsolation_GetPrompt_ownerOrgSucceeds(t *testing.T) {
	svc := buildPromptService(&orgPromptRepo{prompt: promptOwnedByOrgA()})

	p, err := svc.GetPrompt(promptCtx(promptOrgA), "prm_a1")
	if err != nil {
		t.Fatalf("want success for owner org, got %v", err)
	}
	if p.ID != "prm_a1" {
		t.Errorf("want prompt ID prm_a1, got %s", p.ID)
	}
}

func TestOrgIsolation_UpdatePrompt_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildPromptService(&orgPromptRepo{prompt: promptOwnedByOrgA()})

	name := "hijacked"
	_, err := svc.UpdatePrompt(promptCtx(promptOrgB), "prm_a1", application.UpdatePromptInput{Name: &name})
	if err != domain.ErrPromptNotFound {
		t.Errorf("want ErrPromptNotFound when org-B updates org-A prompt, got %v", err)
	}
}

func TestOrgIsolation_DeletePrompt_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildPromptService(&orgPromptRepo{prompt: promptOwnedByOrgA()})

	err := svc.DeletePrompt(promptCtx(promptOrgB), "prm_a1")
	if err != domain.ErrPromptNotFound {
		t.Errorf("want ErrPromptNotFound when org-B deletes org-A prompt, got %v", err)
	}
}

func TestOrgIsolation_ListPrompts_onlyReturnsCalling(t *testing.T) {
	svc := buildPromptService(&orgPromptRepo{prompt: promptOwnedByOrgA()})

	// org-A sees its own prompt
	resultA, err := svc.ListPrompts(promptCtx(promptOrgA), pagination.Slice{Page: 1, PerPage: 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	if resultA.Meta.Total != 1 {
		t.Errorf("want org-A to see 1 prompt, got %d", resultA.Meta.Total)
	}

	// org-B sees nothing
	resultB, err := svc.ListPrompts(promptCtx(promptOrgB), pagination.Slice{Page: 1, PerPage: 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	if resultB.Meta.Total != 0 {
		t.Errorf("want org-B to see 0 prompts (cross-org), got %d", resultB.Meta.Total)
	}
}

func TestOrgIsolation_CreatePrompt_setsOrgIDFromContext(t *testing.T) {
	repo := &orgPromptRepo{}
	svc := buildPromptService(repo)

	_, err := svc.CreatePrompt(promptCtx(promptOrgA), application.CreatePromptInput{
		Name:    "my-prompt",
		Content: "You are a helpful assistant.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.prompt == nil {
		t.Fatal("want prompt created, got nil")
	}
	if repo.prompt.OrgID != promptOrgA {
		t.Errorf("want OrgID %q set on new prompt, got %q", promptOrgA, repo.prompt.OrgID)
	}
}
