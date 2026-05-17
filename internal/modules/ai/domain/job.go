package domain

import (
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

// JobStatus tracks where an inference job is in its lifecycle.
type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusFailed    JobStatus = "FAILED"
)

// Job represents an asynchronous AI inference request persisted in the database.
// In a fully serverless deployment the dispatcher triggers a separate execution
// (e.g. via SQS/EventBridge) so the caller does not have to wait. In local mode a
// goroutine is used for simplicity.
type Job struct {
	ID           string               `json:"id"                    gorm:"primaryKey;size:50"`
	OrgID        string               `json:"-"                     gorm:"size:50;not null;index"`
	ModelID      string               `json:"modelId"               gorm:"size:50;not null;index"`
	Status       JobStatus            `json:"status"                gorm:"size:50;not null;default:'PENDING'"`
	Fields       dbtype.JSONStringMap `json:"fields"                gorm:"serializer:json"`
	Options      dbtype.JSONMap       `json:"options,omitempty"     gorm:"serializer:json"`
	Result       string               `json:"result,omitempty"      gorm:"type:text"`
	ErrorMessage string               `json:"error,omitempty"       gorm:"column:error_message;type:text"`
	CallbackURL  string               `json:"callbackUrl,omitempty" gorm:"column:callback_url;type:text"`
	StartedAt    *time.Time           `json:"startedAt,omitempty"`
	FinishedAt   *time.Time           `json:"finishedAt,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	ModifiedAt   time.Time            `json:"modifiedAt"            gorm:"autoUpdateTime"`
}
