package domain

import (
	"context"
	"time"
)

func (Organization) TableName() string { return "auth_organizations" }

// Organization is the top-level tenant. All resources (teams, API keys,
// virtual keys, routers) are scoped to an organization.
type Organization struct {
	ID         string    `json:"id"          gorm:"primaryKey;size:50"`
	Name       string    `json:"name"        gorm:"size:255;not null"`
	Slug       string    `json:"slug"        gorm:"size:100;not null"`
	IsEnabled  bool      `json:"isEnabled"   gorm:"not null;default:true"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt" gorm:"autoUpdateTime"`
}

// OIDCGroupMapping maps an OIDC groups claim value to a team.
// When a user logs in via OIDC and their JWT contains a matching group,
// they are automatically added as a member of the corresponding team.
type OIDCGroupMapping struct {
	ID        string    `json:"id"        gorm:"primaryKey;size:50"`
	OrgID     string    `json:"orgId"     gorm:"size:50;not null;index"`
	GroupName string    `json:"groupName" gorm:"size:255;not null"`
	TeamID    string    `json:"teamId"    gorm:"size:50;not null"`
	CreatedAt time.Time `json:"createdAt"`
}

func (OIDCGroupMapping) TableName() string { return "auth_oidc_group_mappings" }

// UserTeam is the join record for the many-to-many user ↔ team relationship.
type UserTeam struct {
	UserID    string    `json:"userId"    gorm:"primaryKey;size:50"`
	TeamID    string    `json:"teamId"    gorm:"primaryKey;size:50"`
	CreatedAt time.Time `json:"createdAt"`
}

func (UserTeam) TableName() string { return "auth_user_teams" }

// ── Context helpers ───────────────────────────────────────────────────────────

type orgIDContextKey struct{}

// WithOrgID stores orgID in ctx so service methods can read it without
// changing their signatures.
func WithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgIDContextKey{}, orgID)
}

// OrgIDFromContext returns the org ID stored by WithOrgID, or "" if absent.
func OrgIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(orgIDContextKey{}).(string)
	return v
}

// ── Virtual-key context (for post-inference cost recording) ───────────────────

type vkContextKey struct{}
type callerTeamContextKey struct{}
type callerTeamBypassContextKey struct{}

// WithVirtualKeyID stores the matched virtual key ID in ctx so services can
// record actual inference cost after the call completes.
func WithVirtualKeyID(ctx context.Context, vkID, teamID string) context.Context {
	ctx = context.WithValue(ctx, vkContextKey{}, [2]string{vkID, teamID})
	return WithCallerTeamID(ctx, teamID)
}

// VirtualKeyIDFromContext returns the (virtualKeyID, teamID) pair stored by
// WithVirtualKeyID, or ("","") if no virtual key was used.
func VirtualKeyIDFromContext(ctx context.Context) (vkID, teamID string) {
	pair, _ := ctx.Value(vkContextKey{}).([2]string)
	return pair[0], pair[1]
}

// WithCallerTeamID stores the authenticated caller's team independently from
// virtual-key accounting. Plain API keys also belong to teams and must be
// subject to router team allow-lists.
func WithCallerTeamID(ctx context.Context, teamID string) context.Context {
	return WithCallerTeamIDs(ctx, teamID)
}

// WithCallerTeamIDs stores all teams the authenticated caller belongs to.
// Session callers can belong to multiple teams; API and virtual keys usually
// carry one team. Empty IDs are ignored and do not grant access to restricted
// routers.
func WithCallerTeamIDs(ctx context.Context, teamIDs ...string) context.Context {
	seen := make(map[string]struct{}, len(teamIDs))
	ids := make([]string, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		if teamID == "" {
			continue
		}
		if _, ok := seen[teamID]; ok {
			continue
		}
		seen[teamID] = struct{}{}
		ids = append(ids, teamID)
	}
	return context.WithValue(ctx, callerTeamContextKey{}, ids)
}

// CallerTeamIDFromContext returns the team ID associated with the current
// inference credential, or "" when the caller has no team on the context.
func CallerTeamIDFromContext(ctx context.Context) string {
	ids := CallerTeamIDsFromContext(ctx)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// CallerTeamIDsFromContext returns all team IDs associated with the current
// inference caller.
func CallerTeamIDsFromContext(ctx context.Context) []string {
	switch v := ctx.Value(callerTeamContextKey{}).(type) {
	case []string:
		return append([]string(nil), v...)
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

// WithCallerTeamAccessBypass marks a trusted session, such as an organisation
// admin, as exempt from router team allow-lists.
func WithCallerTeamAccessBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, callerTeamBypassContextKey{}, true)
}

func CallerCanBypassTeamAccess(ctx context.Context) bool {
	ok, _ := ctx.Value(callerTeamBypassContextKey{}).(bool)
	return ok
}
