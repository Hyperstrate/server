package domain

import "time"

type ResetPeriod string

const (
	ResetPeriodNone    ResetPeriod = ""
	ResetPeriodDaily   ResetPeriod = "daily"
	ResetPeriodWeekly  ResetPeriod = "weekly"
	ResetPeriodMonthly ResetPeriod = "monthly"
)

func (VirtualKey) TableName() string { return "auth_virtual_keys" }

// VirtualKey is a budget-scoped key issued to a team or customer.
type VirtualKey struct {
	ID           string      `json:"id"           gorm:"primaryKey;size:50"`
	OrgID        string      `json:"orgId"        gorm:"size:50;not null"`
	RouterID     string      `json:"routerId"     gorm:"size:50;not null"`
	TeamID       *string     `json:"teamId"       gorm:"size:50"`
	Name         string      `json:"name"         gorm:"size:255;not null"`
	Description  string      `json:"description"  gorm:"type:text"`
	KeyHash      string      `json:"-"            gorm:"size:64;not null;uniqueIndex"`
	MaxRequests  int64       `json:"maxRequests"  gorm:"not null;default:0"`
	MaxCostUSD   float64     `json:"maxCostUsd"   gorm:"column:max_cost_usd;not null;default:0"`
	ResetPeriod  ResetPeriod `json:"resetPeriod"  gorm:"size:20;not null;default:''"`
	// RateLimitRPS is a gateway-level cap on requests per second for this key.
	// 0 means unlimited. Enforced in-process via a token bucket; state is not persisted.
	RateLimitRPS float64     `json:"rateLimitRps" gorm:"column:rate_limit_rps;not null;default:0"`
	IsEnabled    bool        `json:"isEnabled"    gorm:"not null;default:true"`
	CreatedAt    time.Time   `json:"createdAt"`
	ModifiedAt   time.Time   `json:"modifiedAt"  gorm:"autoUpdateTime"`
}
