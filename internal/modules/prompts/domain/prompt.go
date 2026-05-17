package domain

import (
	"context"
	"errors"
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

var ErrPromptNotFound = errors.New("prompt not found")

// Prompt is a reusable, named system-prompt template that can be attached to
// a router. Content may contain {{variable}} placeholders that are interpolated
// at inference time from the request's fields map.
type Prompt struct {
	ID          string `json:"id"          gorm:"primaryKey;size:50"`
	OrgID       string `json:"-"           gorm:"size:50;not null;index"`
	Name        string `json:"name"        gorm:"size:255;not null"`
	Description string `json:"description" gorm:"type:text"`
	Content     string `json:"content"     gorm:"type:text;not null"`
	// Variables is the set of {{key}} names found in Content, extracted on save.
	Variables  dbtype.JSONStringSlice `json:"variables"  gorm:"serializer:json;column:variables"`
	CreatedAt  time.Time              `json:"createdAt"`
	ModifiedAt time.Time              `json:"modifiedAt" gorm:"autoUpdateTime"`
}

// PromptRepository is the persistence contract for system prompts.
type PromptRepository interface {
	List(ctx context.Context, orgID, query string, offset, limit int) ([]Prompt, int64, error)
	Create(ctx context.Context, p *Prompt) error
	FindByID(ctx context.Context, orgID, id string) (*Prompt, error)
	Update(ctx context.Context, p *Prompt) error
	Delete(ctx context.Context, orgID, id string) error
}

// PromptVersion is an immutable snapshot of a prompt taken on every save.
type PromptVersion struct {
	ID        string                 `json:"id"        gorm:"primaryKey;size:50"`
	PromptID  string                 `json:"promptId"  gorm:"size:50;not null;index"`
	OrgID     string                 `json:"-"         gorm:"size:50;not null"`
	Version   int                    `json:"version"   gorm:"not null;default:1"`
	Name      string                 `json:"name"      gorm:"size:255;not null"`
	Content   string                 `json:"content"   gorm:"type:text;not null"`
	Variables dbtype.JSONStringSlice `json:"variables" gorm:"serializer:json;column:variables"`
	CreatedAt time.Time              `json:"createdAt"`
}

var ErrPromptVersionNotFound = errors.New("prompt version not found")

// PromptVersionRepository is the persistence contract for prompt version history.
type PromptVersionRepository interface {
	Create(ctx context.Context, v *PromptVersion) error
	ListByPromptID(ctx context.Context, orgID, promptID string, offset, limit int) ([]PromptVersion, int64, error)
	FindByID(ctx context.Context, orgID, id string) (*PromptVersion, error)
	LatestVersion(ctx context.Context, promptID string) (int, error)
}
