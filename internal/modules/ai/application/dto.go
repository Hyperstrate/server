package application

import (
	"encoding/json"
	"time"

	"hyperstrate/server/internal/modules/ai/domain"
)

// --- Model DTOs ---

// RegisterModelInput registers a new AI model using a static catalog model definition.
// ModelDefinitionKey must match an entry in the catalog (e.g. "chatgpt/gpt-4o").
type RegisterModelInput struct {
	ModelDefinitionKey string `json:"modelDefinitionKey" binding:"required"`
	Alias              string `json:"alias,omitempty"`
}

// UpdateModelInput allows renaming the human-readable alias of a registration.
type UpdateModelInput struct {
	Alias *string `json:"alias,omitempty"`
}

// ModelResponse is the API view of a registered model – it merges the thin
// registration record with the static model definition from the catalog.
type ModelResponse struct {
	ID                 string                      `json:"id"                 validate:"required"`
	ModelDefinitionKey string                      `json:"modelDefinitionKey" validate:"required"`
	Alias              string                      `json:"alias,omitempty"`
	Provider           domain.Provider             `json:"provider"           validate:"required"`
	ModelID            string                      `json:"modelId"            validate:"required"`
	DisplayName        string                      `json:"displayName"        validate:"required"`
	Description        string                      `json:"description,omitempty"`
	Capabilities       []string                    `json:"capabilities,omitempty"`
	InputFields        []domain.InputFieldDef      `json:"inputFields"        validate:"required"`
	CredentialFields   []domain.CredentialFieldDef `json:"credentialFields"   validate:"required"`
	DefaultBaseURL     string                      `json:"defaultBaseUrl,omitempty"`
	HasConfiguration   bool                        `json:"hasConfiguration"   validate:"required"`
	CreatedAt          time.Time                   `json:"createdAt"          validate:"required"`
	ModifiedAt         time.Time                   `json:"modifiedAt"         validate:"required"`
}

// --- Model config DTOs ---

// SetModelConfigurationInput configures how the proxy reaches a model's backend.
// APIKey and APISecret are write-only: they are stored but never returned.
// APIKeyPool, when set, overrides APIKey for round-robin rotation.
type SetModelConfigurationInput struct {
	BaseURL   string `json:"baseUrl"            binding:"required,url"`
	APIKey    string `json:"apiKey,omitempty"`
	APISecret string `json:"apiSecret,omitempty"`
	// APIKeyPool is an optional list of API keys for round-robin key rotation.
	// When non-empty, the proxy ignores APIKey and cycles through the pool.
	// Omit to keep the existing pool; pass an empty array [] to clear it.
	APIKeyPool   []string          `json:"apiKeyPool,omitempty"`
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
	TimeoutSecs  int               `json:"timeoutSecs,omitempty"`
}

// ModelConfigurationResponse is the safe view of ModelConfiguration – sensitive
// credentials are replaced by boolean flags indicating whether they have been set.
type ModelConfigurationResponse struct {
	ID           string `json:"id"             validate:"required"`
	ModelID      string `json:"modelId"        validate:"required"`
	BaseURL      string `json:"baseUrl"        validate:"required"`
	HasAPIKey    bool   `json:"hasApiKey"      validate:"required"`
	HasAPISecret bool   `json:"hasApiSecret"   validate:"required"`
	// APIKeyPoolSize is the number of keys in the rotation pool (0 = pool unused).
	APIKeyPoolSize int               `json:"apiKeyPoolSize"`
	ExtraHeaders   map[string]string `json:"extraHeaders,omitempty"`
	TimeoutSecs    int               `json:"timeoutSecs"    validate:"required"`
}

func redactHeaderValues(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	redacted := make(map[string]string, len(headers))
	for k := range headers {
		redacted[k] = "<redacted>"
	}
	return redacted
}

// --- Inference DTOs ---

// HistoryMessage is a single turn in a conversation sent by the client.
// Role is usually "user" or "assistant"; tool loops may also include "tool".
type HistoryMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
}

// --- Conversation DTOs ---

type ConversationResponse struct {
	ID         string    `json:"id"         validate:"required"`
	ModelID    string    `json:"modelId"    validate:"required"`
	Title      string    `json:"title,omitempty"`
	CreatedAt  time.Time `json:"createdAt"  validate:"required"`
	ModifiedAt time.Time `json:"modifiedAt" validate:"required"`
}

type CreateConversationInput struct {
	ModelID string `json:"modelId" binding:"required"`
	Title   string `json:"title,omitempty"`
}

type AddConversationMessageInput struct {
	Role    string `json:"role"    binding:"required,oneof=user assistant"`
	Content string `json:"content" binding:"required"`
}

// --- Inference DTOs ---

// InferRequest is the body for a synchronous or streaming inference call.
// Fields keys must match the InputFieldDef keys in the model's catalog definition
// (e.g. "prompt", "systemPrompt", "image", "referenceImage").
// History is loaded automatically when ConversationID is set; the explicit
// History field is used only for stateless calls without a conversation.
type InferRequest struct {
	ModelID        string            `json:"modelId"        binding:"required"`
	ConversationID string            `json:"conversationId,omitempty"`
	Fields         map[string]string `json:"fields,omitempty"`
	Options        map[string]any    `json:"options,omitempty"`
	History        []HistoryMessage  `json:"history,omitempty"`
}

// InferenceResult is the outcome of a completed synchronous inference.
type InferenceResult struct {
	Content                 string          `json:"content"                validate:"required"`
	Raw                     map[string]any  `json:"raw,omitempty"`
	ModelDefKey             string          `json:"modelDefKey,omitempty"`
	Provider                string          `json:"provider,omitempty"`
	InputTokens             int64           `json:"inputTokens,omitempty"`
	OutputTokens            int64           `json:"outputTokens,omitempty"`
	CachedInputTokens       int64           `json:"cachedInputTokens,omitempty"`
	CacheWriteInputTokens   int64           `json:"cacheWriteInputTokens,omitempty"`
	CacheWrite1hInputTokens int64           `json:"cacheWrite1hInputTokens,omitempty"`
	CostUSD                 float64         `json:"costUsd,omitempty"`
	ToolCalls               json.RawMessage `json:"toolCalls,omitempty"`
}

// --- Job DTOs ---

// SubmitJobRequest is the body for an async job submission (POST /ai/jobs).
// The caller receives a job ID immediately and polls GET /ai/jobs/:id for the result.
// Set CallbackURL to receive a POST webhook when the job reaches COMPLETED or FAILED.
type SubmitJobRequest struct {
	ModelID     string            `json:"modelId"     binding:"required"`
	Fields      map[string]string `json:"fields,omitempty"`
	Options     map[string]any    `json:"options,omitempty"`
	CallbackURL string            `json:"callbackUrl,omitempty" binding:"omitempty,url"`
}

// StreamChunk is a single content delta yielded by a streaming inference response.
// The channel returned by InferStream emits these until Done is true or Err is set.
// InputTokens, OutputTokens, prompt-cache token counts, and ToolCalls are non-zero only on the Done chunk.
type StreamChunk struct {
	Delta                   string
	Done                    bool
	Err                     error
	InputTokens             int64
	OutputTokens            int64
	CachedInputTokens       int64
	CacheWriteInputTokens   int64
	CacheWrite1hInputTokens int64
	// ToolCalls is populated on the Done chunk when the model responded with
	// tool/function calls instead of text content.
	ToolCalls json.RawMessage
}

// ── MCP Servers ───────────────────────────────────────────────────────────────

type CreateMCPServerInput struct {
	Name         string            `json:"name"        binding:"required,max=255"`
	Description  string            `json:"description" binding:"max=1000"`
	URL          string            `json:"url"         binding:"required,url"`
	AuthType     string            `json:"authType"    binding:"omitempty,oneof=none bearer api_key"`
	AuthToken    string            `json:"authToken"`
	AuthHeader   string            `json:"authHeader"  binding:"max=255"`
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
	TimeoutSecs  int               `json:"timeoutSecs" binding:"min=0"`
}

type UpdateMCPServerInput struct {
	Name         *string            `json:"name"        binding:"omitempty,max=255"`
	Description  *string            `json:"description" binding:"omitempty,max=1000"`
	URL          *string            `json:"url"         binding:"omitempty,url"`
	AuthType     *string            `json:"authType"    binding:"omitempty,oneof=none bearer api_key"`
	AuthToken    *string            `json:"authToken"`
	AuthHeader   *string            `json:"authHeader"  binding:"omitempty,max=255"`
	ExtraHeaders *map[string]string `json:"extraHeaders,omitempty"`
	TimeoutSecs  *int               `json:"timeoutSecs" binding:"omitempty,min=0"`
}

type MCPServerResponse struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	URL          string            `json:"url"`
	AuthType     string            `json:"authType"`
	AuthHeader   string            `json:"authHeader,omitempty"`
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
	TimeoutSecs  int               `json:"timeoutSecs"`
	CreatedAt    time.Time         `json:"createdAt"`
	ModifiedAt   time.Time         `json:"modifiedAt"`
}

func toMCPServerResponse(s *domain.MCPServer) MCPServerResponse {
	return MCPServerResponse{
		ID:           s.ID,
		Name:         s.Name,
		Description:  s.Description,
		URL:          s.URL,
		AuthType:     s.AuthType,
		AuthHeader:   s.AuthHeader,
		ExtraHeaders: redactHeaderValues(s.ExtraHeaders),
		TimeoutSecs:  s.TimeoutSecs,
		CreatedAt:    s.CreatedAt,
		ModifiedAt:   s.ModifiedAt,
	}
}
