package domain

import "errors"

var (
	ErrConversationNotFound = errors.New("Conversation not found")
	ErrModelNotFound        = errors.New("Model not found")
	ErrModelConfigurationNotFound = errors.New("Model configuration not found")
	ErrJobNotFound         = errors.New("Job not found")
	ErrModelDefinitionNotFound   = errors.New("Model definition not found in catalog")
	ErrInvalidProvider     = errors.New("Invalid provider")
	ErrInvalidInputType    = errors.New("Invalid input type")
	ErrModelNotConfigured  = errors.New("Model is not configured – set a configuration via PUT /ai/models/:id/configuration")
	ErrProxyFailed              = errors.New("Proxy request to upstream model failed")
	ErrEmbeddingNotSupported    = errors.New("Embedding not supported for this provider")
)
