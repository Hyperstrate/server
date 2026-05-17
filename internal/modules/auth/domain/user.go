package domain

import "time"

// UserRole controls what a user can do within their organisation.
type UserRole string

const (
	UserRoleAdmin  UserRole = "admin"
	UserRoleMember UserRole = "member"
)

func (User) TableName() string { return "auth_users" }

// User represents a human who has authenticated via SSO.
// OrgID is empty until an admin assigns the user to an organisation.
type User struct {
	ID          string     `json:"id"          gorm:"primaryKey;size:50"`
	OrgID       string     `json:"orgId"       gorm:"size:50;default:''"`
	Email       string     `json:"email"       gorm:"size:255;not null;uniqueIndex"`
	Name        string     `json:"name"        gorm:"size:255;not null;default:''"`
	Avatar      string     `json:"avatar"      gorm:"size:1000;not null;default:''"`
	Role        UserRole   `json:"role"        gorm:"size:20;not null;default:'member'"`
	LastLoginAt *time.Time `json:"lastLoginAt" gorm:"default:null"`
	CreatedAt   time.Time  `json:"createdAt"`
	ModifiedAt  time.Time  `json:"modifiedAt"  gorm:"autoUpdateTime"`
}
