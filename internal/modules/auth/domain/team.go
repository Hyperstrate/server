package domain

import "time"

func (Team) TableName() string { return "auth_teams" }

// Team groups users and virtual keys under a shared budget ceiling.
// Every team belongs to exactly one organisation.
type Team struct {
	ID           string    `json:"id"           gorm:"primaryKey;size:50"`
	OrgID        string    `json:"orgId"        gorm:"size:50;not null"`
	Name         string    `json:"name"         gorm:"size:255;not null"`
	Description  string    `json:"description"  gorm:"type:text"`
	MaxRequests  int64     `json:"maxRequests"  gorm:"not null;default:0"`
	MaxCostUSD   float64   `json:"maxCostUsd"   gorm:"column:max_cost_usd;not null;default:0"`
	IsEnabled    bool      `json:"isEnabled"    gorm:"not null;default:true"`
	CreatedAt    time.Time `json:"createdAt"`
	ModifiedAt   time.Time `json:"modifiedAt" gorm:"autoUpdateTime"`
}
