package domain

import "errors"

var (
	ErrRouterNotFound            = errors.New("router not found")
	ErrRouterTargetNotFound      = errors.New("router target not found")
	ErrRouterFeatureNotFound     = errors.New("router feature not found")
	ErrRouterInterceptorNotFound = errors.New("router interceptor not found")

	ErrRouterInactive         = errors.New("router is not active")
	ErrNoTargetsAvailable     = errors.New("no enabled targets available for routing")
	ErrAllTargetsFailed       = errors.New("all targets failed — no successful response")
	ErrInvalidPercentages     = errors.New("target percentages must sum to 100")
	ErrRateLimitExceeded      = errors.New("rate limit exceeded")
	ErrBudgetExceeded         = errors.New("budget limit reached for this router")
	ErrRequestBlocked         = errors.New("request blocked by interceptor policy")
	ErrMissingEmbedModel      = errors.New("semantic features require model_id in config")
	ErrMissingBlockedPatterns = errors.New("content_filter requires at least one blocked_patterns entry")
	ErrTeamNotAllowed         = errors.New("your team does not have access to this router")
	ErrLowQuality             = errors.New("response quality below configured threshold")
	ErrStreamingUnsupported   = errors.New("streaming is not supported with one or more enabled router features")
)
