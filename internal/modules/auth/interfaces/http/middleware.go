package http

import (
	"context"
	"net/http"
	"strings"

	"hyperstrate/server/internal/modules/auth/application"
	"hyperstrate/server/internal/modules/auth/domain"

	"github.com/gin-gonic/gin"
)

const (
	headerAPIKey        = "X-API-Key"
	headerAuthorization = "Authorization"
	ctxKeySessionUser   = "session_user"
)

type ErrorResponse struct {
	Error  string              `json:"error"`
	Fields map[string][]string `json:"fields,omitempty"`
}

// InferAuth enforces authentication on inference endpoints.
// Accepts either an API key (or virtual key) or a valid session token.
// API key is tried first so that budget enforcement and virtual-key cost
// recording still apply. Only falls back to session when the key is not
// found (ErrUnauthorized) — budget/scope errors are returned as-is.
func InferAuth(keyValidator application.KeyValidator, sessionValidator application.SessionValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/swagger") {
			c.Next()
			return
		}
		raw := extractBearerToken(c)
		if raw == "" {
			_ = c.Error(domain.ErrUnauthorized)
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: domain.ErrUnauthorized.Error()})
			return
		}

		routerID := c.Param("id")
		enrichedCtx, keyErr := keyValidator.ValidateInferKey(c.Request.Context(), raw, routerID)
		if keyErr == nil {
			c.Request = c.Request.WithContext(enrichedCtx)
			c.Next()
			return
		}

		// Key not found — try session token as fallback.
		if keyErr == domain.ErrUnauthorized {
			if su, err := sessionValidator.ValidateSession(raw); err == nil {
				c.Set(ctxKeySessionUser, su)
				c.Request = c.Request.WithContext(contextForSession(c.Request.Context(), su))
				c.Next()
				return
			}
		}

		status := http.StatusUnauthorized
		switch keyErr {
		case domain.ErrBudgetExceeded, domain.ErrTeamBudgetExceeded:
			status = http.StatusPaymentRequired
		case domain.ErrRateLimitExceeded:
			status = http.StatusTooManyRequests
		case domain.ErrScopeViolation:
			status = http.StatusForbidden
		}
		_ = c.Error(keyErr)
		c.AbortWithStatusJSON(status, ErrorResponse{Error: keyErr.Error()})
	}
}

// RequireSession validates the session token, sets the session user on the
// context, and embeds the OrgID into the request context for service methods.
func RequireSession(validator application.SessionValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		su, ok := validateSessionToken(c, validator)
		if !ok {
			return
		}
		c.Set(ctxKeySessionUser, su)
		c.Request = c.Request.WithContext(contextForSession(c.Request.Context(), su))
		c.Next()
	}
}

// RequireAdmin validates the session token and additionally enforces the admin role.
func RequireAdmin(validator application.SessionValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		su, ok := validateSessionToken(c, validator)
		if !ok {
			return
		}
		if su.Role != domain.UserRoleAdmin {
			_ = c.Error(domain.ErrForbidden)
			c.AbortWithStatusJSON(http.StatusForbidden, ErrorResponse{Error: domain.ErrForbidden.Error()})
			return
		}
		c.Set(ctxKeySessionUser, su)
		c.Request = c.Request.WithContext(contextForSession(c.Request.Context(), su))
		c.Next()
	}
}

func contextForSession(ctx context.Context, su *application.SessionUser) context.Context {
	ctx = domain.WithOrgID(ctx, su.OrgID)
	ctx = domain.WithCallerTeamIDs(ctx, su.TeamIDs...)
	if su.Role == domain.UserRoleAdmin {
		ctx = domain.WithCallerTeamAccessBypass(ctx)
	}
	return ctx
}

func sessionUserFrom(c *gin.Context) *application.SessionUser {
	return SessionUserFrom(c)
}

// SessionUserFrom returns the authenticated session user stored by the auth
// middleware, or nil if the request was authenticated via API key or not at all.
func SessionUserFrom(c *gin.Context) *application.SessionUser {
	v, exists := c.Get(ctxKeySessionUser)
	if !exists {
		return nil
	}
	su, _ := v.(*application.SessionUser)
	return su
}

func validateSessionToken(c *gin.Context, validator application.SessionValidator) (*application.SessionUser, bool) {
	token := extractBearerToken(c)
	if token == "" {
		_ = c.Error(domain.ErrSessionInvalid)
		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: domain.ErrSessionInvalid.Error()})
		return nil, false
	}
	su, err := validator.ValidateSession(token)
	if err != nil {
		_ = c.Error(err)
		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: domain.ErrSessionInvalid.Error()})
		return nil, false
	}
	return su, true
}

func extractBearerToken(c *gin.Context) string {
	if v := c.GetHeader(headerAPIKey); v != "" {
		return v
	}
	auth := c.GetHeader(headerAuthorization)
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
