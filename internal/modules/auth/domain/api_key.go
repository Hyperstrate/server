package domain

import "time"

// APIKeyScope determines which resources a key can access.
type APIKeyScope string

const (
	APIKeyScopeRouter APIKeyScope = "router"
	APIKeyScopeGlobal APIKeyScope = "global"
)

// APIKey grants bearer access to one or all router inference endpoints.
// Every key must belong to a team and an organisation.
type APIKey struct {
	ID           string      `json:"id"           gorm:"primaryKey;size:50"`
	OrgID        string      `json:"orgId"        gorm:"size:50;not null"`
	TeamID       string      `json:"teamId"       gorm:"size:50;not null"`
	RouterID     string      `json:"routerId"     gorm:"size:50;not null;default:''"`
	VirtualKeyID string      `json:"virtualKeyId" gorm:"size:50;not null;default:''"`
	Name         string      `json:"name"         gorm:"size:255;not null"`
	Description  string      `json:"description"  gorm:"type:text"`
	KeyHash      string      `json:"-"            gorm:"size:64;not null;uniqueIndex"`
	Scope        APIKeyScope `json:"scope"        gorm:"size:50;not null;default:'router'"`
	ExpiresAt    *time.Time  `json:"expiresAt"    gorm:"index"`
	LastUsedAt   *time.Time  `json:"lastUsedAt"`
	IsEnabled    bool        `json:"isEnabled"    gorm:"not null;default:true"`
	CreatedAt    time.Time   `json:"createdAt"`
	ModifiedAt   time.Time   `json:"modifiedAt"   gorm:"autoUpdateTime"`
}

func (APIKey) TableName() string { return "auth_api_keys" }

func (k *APIKey) IsExpired() bool {
	return k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt)
}
