package application

import (
	"context"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/prompts/domain"
	"hyperstrate/server/internal/shared/pagination"
	tmpl "hyperstrate/server/internal/shared/template"

	"go.jetify.com/typeid/v2"
)

// Service defines all prompt use-cases.
type Service interface {
	ListPrompts(ctx context.Context, slice pagination.Slice, query string) (pagination.Paginated[PromptResponse], error)
	CreatePrompt(ctx context.Context, input CreatePromptInput) (*PromptResponse, error)
	GetPrompt(ctx context.Context, id string) (*PromptResponse, error)
	UpdatePrompt(ctx context.Context, id string, input UpdatePromptInput) (*PromptResponse, error)
	DeletePrompt(ctx context.Context, id string) error
	// Version history
	ListPromptVersions(ctx context.Context, promptID string, slice pagination.Slice) (pagination.Paginated[PromptVersionResponse], error)
	GetPromptVersion(ctx context.Context, promptID, versionID string) (*PromptVersionResponse, error)
	RestorePromptVersion(ctx context.Context, promptID, versionID string) (*PromptResponse, error)
}

type service struct {
	repo        domain.PromptRepository
	versionRepo domain.PromptVersionRepository
	bus         *PromptEventBus
}

func NewService(repo domain.PromptRepository, versionRepo domain.PromptVersionRepository, bus *PromptEventBus) Service {
	return &service{repo: repo, versionRepo: versionRepo, bus: bus}
}

func (s *service) ListPrompts(ctx context.Context, slice pagination.Slice, query string) (pagination.Paginated[PromptResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	items, total, err := s.repo.List(ctx, orgID, query, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[PromptResponse]{}, err
	}
	out := make([]PromptResponse, len(items))
	for i, p := range items {
		out[i] = toResponse(&p)
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) CreatePrompt(ctx context.Context, input CreatePromptInput) (*PromptResponse, error) {
	p := &domain.Prompt{
		ID:          typeid.MustGenerate("prm").String(),
		OrgID:       authDomain.OrgIDFromContext(ctx),
		Name:        input.Name,
		Description: input.Description,
		Content:     input.Content,
		Variables:   tmpl.ExtractVariables(input.Content),
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	resp := toResponse(p)
	return &resp, nil
}

func (s *service) GetPrompt(ctx context.Context, id string) (*PromptResponse, error) {
	p, err := s.repo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
	if err != nil {
		return nil, err
	}
	resp := toResponse(p)
	return &resp, nil
}

func (s *service) UpdatePrompt(ctx context.Context, id string, input UpdatePromptInput) (*PromptResponse, error) {
	p, err := s.repo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		p.Name = *input.Name
	}
	if input.Description != nil {
		p.Description = *input.Description
	}
	if input.Content != nil {
		p.Content = *input.Content
		p.Variables = tmpl.ExtractVariables(*input.Content)
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	// Snapshot the new state as an immutable version.
	_ = s.saveVersion(ctx, p)
	resp := toResponse(p)
	return &resp, nil
}

func (s *service) ListPromptVersions(ctx context.Context, promptID string, slice pagination.Slice) (pagination.Paginated[PromptVersionResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	if _, err := s.repo.FindByID(ctx, orgID, promptID); err != nil {
		return pagination.Paginated[PromptVersionResponse]{}, err
	}
	items, total, err := s.versionRepo.ListByPromptID(ctx, orgID, promptID, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[PromptVersionResponse]{}, err
	}
	out := make([]PromptVersionResponse, len(items))
	for i, v := range items {
		out[i] = toVersionResponse(&v)
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) GetPromptVersion(ctx context.Context, promptID, versionID string) (*PromptVersionResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	v, err := s.versionRepo.FindByID(ctx, orgID, versionID)
	if err != nil {
		return nil, err
	}
	if v.PromptID != promptID {
		return nil, domain.ErrPromptVersionNotFound
	}
	resp := toVersionResponse(v)
	return &resp, nil
}

func (s *service) RestorePromptVersion(ctx context.Context, promptID, versionID string) (*PromptResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	v, err := s.versionRepo.FindByID(ctx, orgID, versionID)
	if err != nil {
		return nil, err
	}
	if v.PromptID != promptID {
		return nil, domain.ErrPromptVersionNotFound
	}
	p, err := s.repo.FindByID(ctx, orgID, promptID)
	if err != nil {
		return nil, err
	}
	p.Name = v.Name
	p.Content = v.Content
	p.Variables = v.Variables
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	_ = s.saveVersion(ctx, p)
	resp := toResponse(p)
	return &resp, nil
}

func (s *service) saveVersion(ctx context.Context, p *domain.Prompt) error {
	next, err := s.versionRepo.LatestVersion(ctx, p.ID)
	if err != nil {
		return err
	}
	v := &domain.PromptVersion{
		ID:        typeid.MustGenerate("prmv").String(),
		PromptID:  p.ID,
		OrgID:     p.OrgID,
		Version:   next + 1,
		Name:      p.Name,
		Content:   p.Content,
		Variables: p.Variables,
	}
	return s.versionRepo.Create(ctx, v)
}

func (s *service) DeletePrompt(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, authDomain.OrgIDFromContext(ctx), id); err != nil {
		return err
	}
	_ = s.bus.EmitDeleted(ctx, PromptDeletedEvent{PromptID: id})
	return nil
}

func toVersionResponse(v *domain.PromptVersion) PromptVersionResponse {
	vars := []string(v.Variables)
	if vars == nil {
		vars = []string{}
	}
	return PromptVersionResponse{
		ID:        v.ID,
		PromptID:  v.PromptID,
		Version:   v.Version,
		Name:      v.Name,
		Content:   v.Content,
		Variables: vars,
		CreatedAt: v.CreatedAt,
	}
}

func toResponse(p *domain.Prompt) PromptResponse {
	vars := []string(p.Variables)
	if vars == nil {
		vars = []string{}
	}
	return PromptResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Content:     p.Content,
		Variables:   vars,
		CreatedAt:   p.CreatedAt,
		ModifiedAt:  p.ModifiedAt,
	}
}
