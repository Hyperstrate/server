package application

import "time"

// CreatePromptInput is the body for POST /prompts.
type CreatePromptInput struct {
	Name        string `json:"name"        binding:"required,max=255"`
	Description string `json:"description"`
	Content     string `json:"content"     binding:"required"`
}

// UpdatePromptInput is the body for PATCH /prompts/:id.
// Nil fields are not updated.
type UpdatePromptInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
}

// PromptResponse is the API view of a stored prompt.
type PromptResponse struct {
	ID          string    `json:"id"          validate:"required"`
	Name        string    `json:"name"        validate:"required"`
	Description string    `json:"description"`
	Content     string    `json:"content"     validate:"required"`
	// Variables is the list of {{key}} placeholder names found in Content.
	Variables   []string  `json:"variables"`
	CreatedAt   time.Time `json:"createdAt"   validate:"required"`
	ModifiedAt  time.Time `json:"modifiedAt"  validate:"required"`
}

// PromptVersionResponse is the API view of one immutable prompt snapshot.
type PromptVersionResponse struct {
	ID        string    `json:"id"`
	PromptID  string    `json:"promptId"`
	Version   int       `json:"version"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Variables []string  `json:"variables"`
	CreatedAt time.Time `json:"createdAt"`
}
