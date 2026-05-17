package domain

import "errors"

var (
	ErrOrganizationNotFound = errors.New("organization not found")
	ErrSetupAlreadyDone     = errors.New("setup has already been completed")
	ErrAPIKeyNotFound       = errors.New("api key not found")
	ErrVirtualKeyNotFound   = errors.New("virtual key not found")
	ErrTeamNotFound         = errors.New("team not found")
	ErrTeamRequired         = errors.New("team is required")
	ErrUserNotFound         = errors.New("user not found")
	ErrUnauthorized         = errors.New("missing or invalid api key")
	ErrSessionInvalid       = errors.New("missing or invalid session token")
	ErrForbidden            = errors.New("admin role required")
	ErrKeyExpired           = errors.New("api key has expired")
	ErrKeyDisabled          = errors.New("api key is disabled")
	ErrTeamDisabled         = errors.New("team is disabled")
	ErrBudgetExceeded       = errors.New("virtual key budget exceeded")
	ErrTeamBudgetExceeded   = errors.New("team budget exceeded")
	ErrRateLimitExceeded    = errors.New("virtual key rate limit exceeded")
	ErrScopeViolation       = errors.New("api key does not have access to this router")
)
