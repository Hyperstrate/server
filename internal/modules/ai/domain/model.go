package domain

import "time"

// Model is a user-registered instance of a catalog ModelDefinition.
// It binds a static definition (display schema, provider, model ID) to runtime
// credentials (stored in ModelConfiguration). Use FindModelDefinition(model.ModelDefinitionKey)
// to retrieve the full spec.
type Model struct {
	ID                 string    `json:"id"                 gorm:"primaryKey;size:50"`
	OrgID              string    `json:"-"                  gorm:"size:50;not null;index"`
	ModelDefinitionKey string    `json:"modelDefinitionKey" gorm:"size:255;not null;index"`
	// Alias is an optional human-readable label for this registration.
	Alias              string    `json:"alias,omitempty"    gorm:"size:255"`
	CreatedAt          time.Time `json:"createdAt"`
	ModifiedAt         time.Time `json:"modifiedAt"         gorm:"autoUpdateTime"`
}
