package domain

import (
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

// ModelConfiguration stores the runtime configuration for a Model: where to reach it and
// how to authenticate. Sensitive fields (APIKey, APISecret, APIKeyPool) are never included
// in JSON responses – callers receive ModelConfigurationResponse from the application layer.
type ModelConfiguration struct {
	ID           string                  `json:"id" gorm:"primaryKey;size:50"`
	ModelID      string                  `json:"modelId" gorm:"size:50;uniqueIndex;not null"`
	BaseURL      string                  `json:"baseUrl" gorm:"size:2000;not null"`
	APIKey       string                  `json:"-" gorm:"size:1000"` // excluded from JSON output
	APISecret    string                  `json:"-" gorm:"size:1000"` // excluded from JSON output
	// APIKeyPool is an optional pool of API keys for round-robin rotation.
	// When non-empty, the proxy ignores APIKey and selects from the pool.
	APIKeyPool   dbtype.JSONStringSlice  `json:"-" gorm:"serializer:json;column:api_key_pool"`
	ExtraHeaders dbtype.JSONStringMap    `json:"extraHeaders,omitempty" gorm:"serializer:json"`
	TimeoutSecs  int                     `json:"timeoutSecs" gorm:"not null;default:30"`
	CreatedAt    time.Time               `json:"createdAt"`
	ModifiedAt   time.Time               `json:"modifiedAt" gorm:"autoUpdateTime"`
}

// ModelKeyRotation records each provider API key rotation event.
// During the grace period both the previous and the new key are accepted by
// the proxy. After GraceEndsAt the old key is considered superseded.
type ModelKeyRotation struct {
	ID          string    `json:"id"          gorm:"primaryKey;size:50"`
	ModelID     string    `json:"modelId"     gorm:"size:50;not null;index"`
	OldKeyHint  string    `json:"oldKeyHint"  gorm:"size:10;not null;default:''"` // last 4 chars of the old key
	NewKeyHint  string    `json:"newKeyHint"  gorm:"size:10;not null;default:''"` // last 4 chars of the new key
	GraceEndsAt time.Time `json:"graceEndsAt" gorm:"not null"`
	CreatedAt   time.Time `json:"createdAt"   gorm:"not null;index"`
}

func (ModelKeyRotation) TableName() string { return "model_key_rotations" }
