package domain

import (
	"context"
	"errors"
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

// MCPServer is a managed MCP (Model Context Protocol) server configuration.
// Auth credentials live here, not in the router feature config, so they can
// be rotated in one place and reused across multiple routers.
type MCPServer struct {
	ID          string `json:"id"          gorm:"primaryKey;size:50"`
	OrgID       string `json:"orgId"       gorm:"size:50;not null;index"`
	Name        string `json:"name"        gorm:"size:255;not null"`
	Description string `json:"description" gorm:"size:1000"`
	URL         string `json:"url"         gorm:"size:2000;not null"`
	// AuthType: "none" | "bearer" | "api_key"
	AuthType string `json:"authType"    gorm:"size:50;not null;default:'none'"`
	// AuthToken stores the bearer token or API key value; never returned in API responses.
	AuthToken string `json:"-"           gorm:"size:2000"`
	// AuthHeader is the custom header name used when AuthType is "api_key" (default "X-API-Key").
	AuthHeader   string               `json:"authHeader"    gorm:"size:255"`
	ExtraHeaders dbtype.JSONStringMap `json:"extraHeaders,omitempty" gorm:"serializer:json"`
	TimeoutSecs  int                  `json:"timeoutSecs"   gorm:"not null;default:30"`
	CreatedAt    time.Time            `json:"createdAt"`
	ModifiedAt   time.Time            `json:"modifiedAt" gorm:"autoUpdateTime"`
}

// AuthHeaders returns the HTTP request headers needed to authenticate with this server.
func (s *MCPServer) AuthHeaders() map[string]string {
	headers := make(map[string]string)
	for k, v := range s.ExtraHeaders {
		if k != "" {
			headers[k] = v
		}
	}
	switch s.AuthType {
	case "bearer":
		if s.AuthToken != "" {
			headers["Authorization"] = "Bearer " + s.AuthToken
		}
	case "api_key":
		h := s.AuthHeader
		if h == "" {
			h = "X-API-Key"
		}
		if s.AuthToken != "" {
			headers[h] = s.AuthToken
		}
	}
	return headers
}

// MCPServerRepository defines persistence operations for MCP server configs.
type MCPServerRepository interface {
	Create(ctx context.Context, server *MCPServer) error
	FindByID(ctx context.Context, orgID, serverID string) (*MCPServer, error)
	FindByIDs(ctx context.Context, orgID string, serverIDs []string) ([]MCPServer, error)
	List(ctx context.Context, orgID string) ([]MCPServer, error)
	Update(ctx context.Context, server *MCPServer) error
	Delete(ctx context.Context, orgID, serverID string) error
}

var ErrMCPServerNotFound = errors.New("mcp server not found")
