package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/ai/domain"
	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"

	"go.jetify.com/typeid/v2"
)

// SkipInferenceLog returns a context that signals the AI service not to emit
// an InferenceLoggedEvent for this call. Used by the router adapter so the
// router service can emit its own RouterInferenceLoggedEvent instead.
type skipLogKey struct{}

func SkipInferenceLog(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipLogKey{}, true)
}

func shouldLog(ctx context.Context) bool {
	v, _ := ctx.Value(skipLogKey{}).(bool)
	return !v
}

// Proxy forwards inference requests to an upstream AI provider.
// Implementations live in infrastructure/proxy.
type Proxy interface {
	Send(ctx context.Context, def *domain.ModelDefinition, config *domain.ModelConfiguration, req *ProxyRequest) (*ProxyResponse, error)
	// SendStream opens a streaming connection and returns a channel of content deltas.
	// The channel is closed after a Done chunk or an Err chunk is emitted.
	SendStream(ctx context.Context, def *domain.ModelDefinition, config *domain.ModelConfiguration, req *ProxyRequest) (<-chan StreamChunk, error)
	// SendEmbedding calls the provider's embedding endpoint and returns a dense float vector.
	// Returns an error for providers that do not support embeddings — callers degrade gracefully.
	SendEmbedding(ctx context.Context, def *domain.ModelDefinition, config *domain.ModelConfiguration, text string) ([]float32, error)
}

// JobProcessor executes a single persisted job synchronously.
// Separated from Service to avoid the circular dep: Service→Dispatcher→Service.
type JobProcessor interface {
	ProcessJob(ctx context.Context, jobID string) error
}

// JobDispatcher enqueues a job ID for background execution.
// Two implementations exist: GoroutineDispatcher (local) and SQSDispatcher (Lambda).
type JobDispatcher interface {
	Dispatch(ctx context.Context, jobID string) error
}

// ProxyRequest is the normalised payload passed to Proxy.Send.
type ProxyRequest struct {
	Fields  map[string]string
	Options map[string]any
	History []HistoryMessage
}

// ProxyResponse is what the upstream model returned.
type ProxyResponse struct {
	Content                 string
	Raw                     map[string]any
	InputTokens             int64
	OutputTokens            int64
	CachedInputTokens       int64 // tokens served from provider-side prompt cache
	CacheWriteInputTokens   int64 // tokens written to provider-side prompt cache
	CacheWrite1hInputTokens int64 // Anthropic 1-hour cache-write tokens, when reported
	// ToolCalls carries serialised tool/function call objects when the model
	// responded with a tool_calls turn instead of plain text content.
	ToolCalls json.RawMessage
}

// Service defines all AI module use-cases.
type Service interface {
	// Catalog (static, no DB)
	ListCatalog(query string) []domain.ModelDefinition
	GetCatalogEntry(key string) (*domain.ModelDefinition, error)

	// Model registrations
	// ListModels returns registered models, optionally filtered to those whose catalog
	// definition includes ALL of the requested capabilities.
	ListModels(ctx context.Context, slice pagination.Slice, capabilities []string, query string) (pagination.Paginated[ModelResponse], error)
	// GetModelsByIDs fetches specific models by ID in a single query.
	// IDs not found are silently omitted; order is not guaranteed.
	GetModelsByIDs(ctx context.Context, ids []string) ([]ModelResponse, error)
	RegisterModel(ctx context.Context, input RegisterModelInput) (*ModelResponse, error)
	GetModel(ctx context.Context, id string) (*ModelResponse, error)
	UpdateModel(ctx context.Context, id string, input UpdateModelInput) (*ModelResponse, error)
	DeleteModel(ctx context.Context, id string) error

	// Model config management
	SetModelConfiguration(ctx context.Context, modelID string, input SetModelConfigurationInput) (*ModelConfigurationResponse, error)
	GetModelConfiguration(ctx context.Context, modelID string) (*ModelConfigurationResponse, error)
	ListModelConfigurations(ctx context.Context, modelIDs []string) ([]*ModelConfigurationResponse, error)
	RotateAPIKey(ctx context.Context, modelID, newKey string, gracePeriodHours int) (*domain.ModelKeyRotation, error)
	ListKeyRotations(ctx context.Context, modelID string, limit, offset int) ([]domain.ModelKeyRotation, int64, error)

	// Conversations
	ListConversations(ctx context.Context, slice pagination.Slice) (pagination.Paginated[ConversationResponse], error)
	CreateConversation(ctx context.Context, input CreateConversationInput) (*ConversationResponse, error)
	GetConversation(ctx context.Context, id string) (*ConversationResponse, error)
	DeleteConversation(ctx context.Context, id string) error
	ListConversationMessages(ctx context.Context, conversationID string) ([]domain.ConversationMessage, error)
	AddConversationMessage(ctx context.Context, conversationID string, input AddConversationMessageInput) (*domain.ConversationMessage, error)

	// Synchronous inference
	Infer(ctx context.Context, input InferRequest) (*InferenceResult, error)
	// InferStream performs inference over a server-sent events stream.
	// Returns a channel of StreamChunk; closed when the stream ends.
	InferStream(ctx context.Context, input InferRequest) (<-chan StreamChunk, error)
	// Embed returns a dense vector for text using the registered embedding model identified
	// by modelID. Used internally by router semantic features; not exposed over HTTP.
	Embed(ctx context.Context, modelID string, text string) ([]float32, error)

	// Asynchronous job management
	SubmitJob(ctx context.Context, input SubmitJobRequest) (*domain.Job, error)
	GetJob(ctx context.Context, id string) (*domain.Job, error)
	ListJobs(ctx context.Context, slice pagination.Slice) (pagination.Paginated[domain.Job], error)
	ProcessJob(ctx context.Context, jobID string) error

	// Health monitoring (cross-org, used only by the observability health monitor)
	ListHealthTargets(ctx context.Context) ([]HealthTarget, error)

	// MCP Servers
	ListMCPServers(ctx context.Context) ([]MCPServerResponse, error)
	CreateMCPServer(ctx context.Context, input CreateMCPServerInput) (*MCPServerResponse, error)
	GetMCPServer(ctx context.Context, serverID string) (*MCPServerResponse, error)
	UpdateMCPServer(ctx context.Context, serverID string, input UpdateMCPServerInput) (*MCPServerResponse, error)
	DeleteMCPServer(ctx context.Context, serverID string) error
}

// HealthTarget carries the minimal info needed to probe a registered model's provider.
type HealthTarget struct {
	ModelID     string
	ModelDefKey string
	Provider    string
	BaseURL     string
	APIKey      string
}

type service struct {
	modelRepo        domain.ModelRepository
	configRepo       domain.ModelConfigurationRepository
	rotationRepo     domain.ModelKeyRotationRepository
	conversationRepo domain.ConversationRepository
	jobRepo          domain.JobRepository
	proxy            Proxy
	processor        JobProcessor
	dispatcher       JobDispatcher
	events           *ModelEventBus
	inferBus         *InferenceEventBus
	mcpServerRepo    domain.MCPServerRepository
	mcpServerBus     *MCPServerEventBus
}

func NewService(
	modelRepo domain.ModelRepository,
	configRepo domain.ModelConfigurationRepository,
	rotationRepo domain.ModelKeyRotationRepository,
	conversationRepo domain.ConversationRepository,
	jobRepo domain.JobRepository,
	proxy Proxy,
	processor JobProcessor,
	dispatcher JobDispatcher,
	events *ModelEventBus,
	inferBus *InferenceEventBus,
	mcpServerRepo domain.MCPServerRepository,
	mcpServerBus *MCPServerEventBus,
) Service {
	return &service{
		modelRepo:        modelRepo,
		configRepo:       configRepo,
		rotationRepo:     rotationRepo,
		conversationRepo: conversationRepo,
		jobRepo:          jobRepo,
		proxy:            proxy,
		processor:        processor,
		dispatcher:       dispatcher,
		events:           events,
		inferBus:         inferBus,
		mcpServerRepo:    mcpServerRepo,
		mcpServerBus:     mcpServerBus,
	}
}

// --- Catalog ---

func (s *service) ListCatalog(query string) []domain.ModelDefinition {
	definitions := domain.AllDefinitions()
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return definitions
	}
	out := make([]domain.ModelDefinition, 0, len(definitions))
	for _, def := range definitions {
		if modelDefinitionMatchesQuery(def, query) {
			out = append(out, def)
		}
	}
	return out
}

func (s *service) GetCatalogEntry(key string) (*domain.ModelDefinition, error) {
	def, ok := domain.FindModelDefinition(key)
	if !ok {
		return nil, domain.ErrModelDefinitionNotFound
	}
	return &def, nil
}

// --- Model registrations ---

func (s *service) ListModels(ctx context.Context, slice pagination.Slice, capabilities []string, query string) (pagination.Paginated[ModelResponse], error) {
	var models []domain.Model
	var total int64
	var err error

	orgID := authDomain.OrgIDFromContext(ctx)
	if len(capabilities) > 0 {
		// Resolve which model_definition_keys satisfy all requested capabilities.
		eligibleKeys := domain.DefinitionKeysWithCapabilities(capabilities)
		if len(eligibleKeys) == 0 {
			return pagination.New([]ModelResponse{}, 0, slice), nil
		}
		models, total, err = s.modelRepo.ListByDefinitionKeys(ctx, orgID, eligibleKeys, query, slice.Offset(), slice.PerPage)
	} else {
		models, total, err = s.modelRepo.List(ctx, orgID, query, slice.Offset(), slice.PerPage)
	}
	if err != nil {
		return pagination.Paginated[ModelResponse]{}, err
	}

	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	configuredIDs, err := s.configRepo.ListConfiguredModelIDs(ctx, authDomain.OrgIDFromContext(ctx), ids)
	if err != nil {
		return pagination.Paginated[ModelResponse]{}, err
	}
	configured := make(map[string]bool, len(configuredIDs))
	for _, id := range configuredIDs {
		configured[id] = true
	}

	out := make([]ModelResponse, 0, len(models))
	for _, m := range models {
		r := toModelResponse(&m)
		r.HasConfiguration = configured[m.ID]
		out = append(out, r)
	}
	return pagination.New(out, total, slice), nil
}

func modelDefinitionMatchesQuery(def domain.ModelDefinition, query string) bool {
	values := []string{
		def.Key,
		def.DisplayName,
		def.ModelID,
		string(def.Provider),
		def.Description,
	}
	values = append(values, def.Capabilities...)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (s *service) GetModelsByIDs(ctx context.Context, ids []string) ([]ModelResponse, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	models, err := s.modelRepo.ListByIDs(ctx, authDomain.OrgIDFromContext(ctx), ids)
	if err != nil {
		return nil, err
	}
	allIDs := make([]string, len(models))
	for i, m := range models {
		allIDs[i] = m.ID
	}
	configuredIDs, err := s.configRepo.ListConfiguredModelIDs(ctx, authDomain.OrgIDFromContext(ctx), allIDs)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]bool, len(configuredIDs))
	for _, id := range configuredIDs {
		configured[id] = true
	}
	out := make([]ModelResponse, 0, len(models))
	for _, m := range models {
		r := toModelResponse(&m)
		r.HasConfiguration = configured[m.ID]
		out = append(out, r)
	}
	return out, nil
}

func (s *service) RegisterModel(ctx context.Context, input RegisterModelInput) (*ModelResponse, error) {
	if _, ok := domain.FindModelDefinition(input.ModelDefinitionKey); !ok {
		return nil, domain.ErrModelDefinitionNotFound
	}

	model := &domain.Model{
		ID:                 typeid.MustGenerate("mdl").String(),
		OrgID:              authDomain.OrgIDFromContext(ctx),
		ModelDefinitionKey: input.ModelDefinitionKey,
		Alias:              input.Alias,
	}

	if err := s.modelRepo.Create(ctx, model); err != nil {
		return nil, err
	}
	resp := toModelResponse(model)
	return &resp, nil
}

func (s *service) GetModel(ctx context.Context, id string) (*ModelResponse, error) {
	model, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
	if err != nil {
		return nil, err
	}
	resp := toModelResponse(model)
	return &resp, nil
}

func (s *service) UpdateModel(ctx context.Context, id string, input UpdateModelInput) (*ModelResponse, error) {
	model, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
	if err != nil {
		return nil, err
	}
	if input.Alias != nil {
		model.Alias = *input.Alias
	}
	if err := s.modelRepo.Update(ctx, model); err != nil {
		return nil, err
	}
	resp := toModelResponse(model)
	return &resp, nil
}

func (s *service) DeleteModel(ctx context.Context, id string) error {
	if err := s.modelRepo.Delete(ctx, authDomain.OrgIDFromContext(ctx), id); err != nil {
		return err
	}
	if err := s.configRepo.DeleteByModelID(ctx, authDomain.OrgIDFromContext(ctx), id); err != nil {
		slog.Error("DeleteModel: failed to delete config", "modelID", id, "err", err)
	}
	return s.events.EmitDeleted(ctx, ModelDeletedEvent{ModelID: id, OrgID: authDomain.OrgIDFromContext(ctx)})
}

// toModelResponse merges a registration record with its static catalog model definition.
func toModelResponse(m *domain.Model) ModelResponse {
	resp := ModelResponse{
		ID:                 m.ID,
		ModelDefinitionKey: m.ModelDefinitionKey,
		Alias:              m.Alias,
		CreatedAt:          m.CreatedAt,
		ModifiedAt:         m.ModifiedAt,
	}
	if def, ok := domain.FindModelDefinition(m.ModelDefinitionKey); ok {
		resp.Provider = def.Provider
		resp.ModelID = def.ModelID
		resp.DisplayName = def.DisplayName
		resp.Description = def.Description
		resp.Capabilities = def.Capabilities
		resp.InputFields = def.InputFields
		resp.CredentialFields = def.CredentialFields
		resp.DefaultBaseURL = def.DefaultBaseURL
		if resp.Alias == "" {
			resp.Alias = def.DisplayName
		}
	}
	if resp.DisplayName == "" {
		resp.DisplayName = m.ModelDefinitionKey
	}
	return resp
}

// --- Model config management ---

func (s *service) SetModelConfiguration(ctx context.Context, modelID string, input SetModelConfigurationInput) (*ModelConfigurationResponse, error) {
	_, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), modelID)
	if err != nil {
		return nil, err
	}

	timeout := input.TimeoutSecs
	if timeout <= 0 {
		timeout = 30
	}

	// Preserve existing credentials when the caller omits them (blank = keep existing).
	apiKey := input.APIKey
	apiSecret := input.APISecret
	var apiKeyPool dbtype.JSONStringSlice
	if input.APIKeyPool != nil {
		apiKeyPool = dbtype.JSONStringSlice(input.APIKeyPool)
	}
	if existing, err := s.configRepo.FindByModelID(ctx, authDomain.OrgIDFromContext(ctx), modelID); err == nil {
		if apiKey == "" {
			apiKey = existing.APIKey
		}
		if apiSecret == "" {
			apiSecret = existing.APISecret
		}
		// nil means "not provided" → keep existing pool.
		// Empty slice means "clear the pool".
		if input.APIKeyPool == nil {
			apiKeyPool = existing.APIKeyPool
		}
	}

	cfg := &domain.ModelConfiguration{
		ID:           typeid.MustGenerate("mcfg").String(),
		ModelID:      modelID,
		BaseURL:      input.BaseURL,
		APIKey:       apiKey,
		APISecret:    apiSecret,
		APIKeyPool:   apiKeyPool,
		ExtraHeaders: input.ExtraHeaders,
		TimeoutSecs:  timeout,
	}

	if err := s.configRepo.Upsert(ctx, authDomain.OrgIDFromContext(ctx), cfg); err != nil {
		return nil, err
	}
	return toConfigResponse(cfg), nil
}

func (s *service) GetModelConfiguration(ctx context.Context, modelID string) (*ModelConfigurationResponse, error) {
	cfg, err := s.configRepo.FindByModelID(ctx, authDomain.OrgIDFromContext(ctx), modelID)
	if err != nil {
		return nil, err
	}
	return toConfigResponse(cfg), nil
}

func (s *service) ListModelConfigurations(ctx context.Context, modelIDs []string) ([]*ModelConfigurationResponse, error) {
	cfgs, err := s.configRepo.ListByModelIDs(ctx, authDomain.OrgIDFromContext(ctx), modelIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*ModelConfigurationResponse, 0, len(cfgs))
	for i := range cfgs {
		out = append(out, toConfigResponse(&cfgs[i]))
	}
	return out, nil
}

func toConfigResponse(cfg *domain.ModelConfiguration) *ModelConfigurationResponse {
	return &ModelConfigurationResponse{
		ID:             cfg.ID,
		ModelID:        cfg.ModelID,
		BaseURL:        cfg.BaseURL,
		HasAPIKey:      cfg.APIKey != "",
		HasAPISecret:   cfg.APISecret != "",
		APIKeyPoolSize: len(cfg.APIKeyPool),
		ExtraHeaders:   redactHeaderValues(cfg.ExtraHeaders),
		TimeoutSecs:    cfg.TimeoutSecs,
	}
}

// ── Key rotation ──────────────────────────────────────────────────────────────

func (s *service) RotateAPIKey(ctx context.Context, modelID, newKey string, gracePeriodHours int) (*domain.ModelKeyRotation, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	cfg, err := s.configRepo.FindByModelID(ctx, orgID, modelID)
	if err != nil {
		return nil, err
	}
	oldHint := keyHint(cfg.APIKey)
	cfg.APIKey = newKey
	if err := s.configRepo.Upsert(ctx, orgID, cfg); err != nil {
		return nil, err
	}
	if gracePeriodHours <= 0 {
		gracePeriodHours = 24
	}
	rot := &domain.ModelKeyRotation{
		ID:          typeid.MustGenerate("krot").String(),
		ModelID:     modelID,
		OldKeyHint:  oldHint,
		NewKeyHint:  keyHint(newKey),
		GraceEndsAt: time.Now().Add(time.Duration(gracePeriodHours) * time.Hour),
		CreatedAt:   time.Now(),
	}
	if err := s.rotationRepo.Create(ctx, rot); err != nil {
		return nil, err
	}
	return rot, nil
}

func (s *service) ListKeyRotations(ctx context.Context, modelID string, limit, offset int) ([]domain.ModelKeyRotation, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.rotationRepo.ListByModelID(ctx, modelID, limit, offset)
}

func keyHint(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return "***" + key[len(key)-4:]
}

// --- Conversations ---

func (s *service) ListConversations(ctx context.Context, slice pagination.Slice) (pagination.Paginated[ConversationResponse], error) {
	items, total, err := s.conversationRepo.List(ctx, authDomain.OrgIDFromContext(ctx), slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[ConversationResponse]{}, err
	}
	out := make([]ConversationResponse, 0, len(items))
	for _, c := range items {
		out = append(out, toConversationResponse(&c))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) CreateConversation(ctx context.Context, input CreateConversationInput) (*ConversationResponse, error) {
	if _, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), input.ModelID); err != nil {
		return nil, err
	}
	c := &domain.Conversation{
		ID:      typeid.MustGenerate("conv").String(),
		OrgID:   authDomain.OrgIDFromContext(ctx),
		ModelID: input.ModelID,
		Title:   input.Title,
	}
	if err := s.conversationRepo.Create(ctx, c); err != nil {
		return nil, err
	}
	resp := toConversationResponse(c)
	return &resp, nil
}

func (s *service) GetConversation(ctx context.Context, id string) (*ConversationResponse, error) {
	c, err := s.conversationRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
	if err != nil {
		return nil, err
	}
	resp := toConversationResponse(c)
	return &resp, nil
}

func (s *service) DeleteConversation(ctx context.Context, id string) error {
	return s.conversationRepo.Delete(ctx, authDomain.OrgIDFromContext(ctx), id)
}

func (s *service) ListConversationMessages(ctx context.Context, conversationID string) ([]domain.ConversationMessage, error) {
	if _, err := s.conversationRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), conversationID); err != nil {
		return nil, err
	}
	return s.conversationRepo.ListMessages(ctx, authDomain.OrgIDFromContext(ctx), conversationID)
}

func (s *service) AddConversationMessage(ctx context.Context, conversationID string, input AddConversationMessageInput) (*domain.ConversationMessage, error) {
	if _, err := s.conversationRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), conversationID); err != nil {
		return nil, err
	}
	msg := &domain.ConversationMessage{
		ID:             typeid.MustGenerate("cmsg").String(),
		ConversationID: conversationID,
		Role:           input.Role,
		Content:        input.Content,
	}
	if err := s.conversationRepo.AddMessage(ctx, authDomain.OrgIDFromContext(ctx), msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func toConversationResponse(c *domain.Conversation) ConversationResponse {
	return ConversationResponse{
		ID:         c.ID,
		ModelID:    c.ModelID,
		Title:      c.Title,
		CreatedAt:  c.CreatedAt,
		ModifiedAt: c.ModifiedAt,
	}
}

// --- Synchronous inference ---

func (s *service) Infer(ctx context.Context, input InferRequest) (*InferenceResult, error) {
	model, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), input.ModelID)
	if err != nil {
		return nil, err
	}

	def, ok := domain.FindModelDefinition(model.ModelDefinitionKey)
	if !ok {
		return nil, domain.ErrModelDefinitionNotFound
	}

	cfg, err := s.configRepo.FindByModelID(ctx, authDomain.OrgIDFromContext(ctx), input.ModelID)
	if err != nil {
		return nil, domain.ErrModelNotConfigured
	}

	start := time.Now()
	resp, inferErr := s.proxy.Send(ctx, &def, cfg, &ProxyRequest{
		Fields:  input.Fields,
		Options: input.Options,
		History: input.History,
	})
	latencyMs := time.Since(start).Milliseconds()

	if inferErr != nil {
		if shouldLog(ctx) {
			s.inferBus.Emit(InferenceLoggedEvent{
				OrgID:        authDomain.OrgIDFromContext(ctx),
				ModelID:      input.ModelID,
				ModelDefKey:  model.ModelDefinitionKey,
				Provider:     string(def.Provider),
				LatencyMs:    latencyMs,
				Status:       "error",
				ErrorMessage: inferErr.Error(),
				Source:       "direct",
			})
		}
		return nil, inferErr
	}

	costUSD := def.ComputeUsageCostUSD(domain.TokenUsage{
		InputTokens:             resp.InputTokens,
		CachedInputTokens:       resp.CachedInputTokens,
		CacheWriteInputTokens:   resp.CacheWriteInputTokens,
		CacheWrite1hInputTokens: resp.CacheWrite1hInputTokens,
		OutputTokens:            resp.OutputTokens,
	})
	if shouldLog(ctx) {
		s.inferBus.Emit(InferenceLoggedEvent{
			OrgID:             authDomain.OrgIDFromContext(ctx),
			ModelID:           input.ModelID,
			ModelDefKey:       model.ModelDefinitionKey,
			Provider:          string(def.Provider),
			InputTokens:       resp.InputTokens,
			OutputTokens:      resp.OutputTokens,
			CachedInputTokens: resp.CachedInputTokens,
			CostUSD:           costUSD,
			LatencyMs:         latencyMs,
			Status:            "success",
			Source:            "direct",
		})
	}

	return &InferenceResult{
		Content:                 resp.Content,
		Raw:                     resp.Raw,
		ModelDefKey:             model.ModelDefinitionKey,
		Provider:                string(def.Provider),
		InputTokens:             resp.InputTokens,
		OutputTokens:            resp.OutputTokens,
		CachedInputTokens:       resp.CachedInputTokens,
		CacheWriteInputTokens:   resp.CacheWriteInputTokens,
		CacheWrite1hInputTokens: resp.CacheWrite1hInputTokens,
		CostUSD:                 costUSD,
		ToolCalls:               resp.ToolCalls,
	}, nil
}

func (s *service) InferStream(ctx context.Context, input InferRequest) (<-chan StreamChunk, error) {
	model, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), input.ModelID)
	if err != nil {
		return nil, err
	}

	def, ok := domain.FindModelDefinition(model.ModelDefinitionKey)
	if !ok {
		return nil, domain.ErrModelDefinitionNotFound
	}

	history := input.History

	// When a conversation is provided, load history and persist the user message
	// BEFORE fetching the model config so the turn is always recorded even if
	// inference later fails (e.g. model not configured).
	var convID string
	if input.ConversationID != "" {
		if _, err := s.conversationRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), input.ConversationID); err != nil {
			return nil, err
		}
		convID = input.ConversationID

		msgs, err := s.conversationRepo.ListMessages(ctx, authDomain.OrgIDFromContext(ctx), convID)
		if err != nil {
			return nil, err
		}
		history = make([]HistoryMessage, len(msgs))
		for i, m := range msgs {
			history[i] = HistoryMessage{Role: m.Role, Content: m.Content}
		}

		// Persist the user message (including full fields for frontend replay).
		prompt := input.Fields["prompt"]
		fieldsJSON, _ := json.Marshal(input.Fields)
		if err := s.conversationRepo.AddMessage(ctx, authDomain.OrgIDFromContext(ctx), &domain.ConversationMessage{
			ID:             typeid.MustGenerate("cmsg").String(),
			ConversationID: convID,
			Role:           "user",
			Content:        prompt,
			Fields:         string(fieldsJSON),
		}); err != nil {
			return nil, err
		}
	}

	cfg, err := s.configRepo.FindByModelID(ctx, authDomain.OrgIDFromContext(ctx), input.ModelID)
	if err != nil {
		return nil, domain.ErrModelNotConfigured
	}

	streamStart := time.Now()
	upstream, err := s.proxy.SendStream(ctx, &def, cfg, &ProxyRequest{
		Fields:  input.Fields,
		Options: input.Options,
		History: history,
	})
	if err != nil {
		if shouldLog(ctx) {
			s.inferBus.Emit(InferenceLoggedEvent{
				OrgID:        authDomain.OrgIDFromContext(ctx),
				ModelID:      input.ModelID,
				ModelDefKey:  model.ModelDefinitionKey,
				Provider:     string(def.Provider),
				LatencyMs:    time.Since(streamStart).Milliseconds(),
				Status:       "error",
				ErrorMessage: err.Error(),
				Source:       "direct",
			})
		}
		return nil, err
	}

	if convID == "" {
		if !shouldLog(ctx) {
			return upstream, nil
		}
		out := make(chan StreamChunk, 16)
		go func() {
			defer close(out)
			var lastErr error
			var inputTokens, outputTokens, cachedInputTokens, cacheWriteInputTokens, cacheWrite1hInputTokens int64
		loop:
			for chunk := range upstream {
				if chunk.Done {
					inputTokens = chunk.InputTokens
					outputTokens = chunk.OutputTokens
					cachedInputTokens = chunk.CachedInputTokens
					cacheWriteInputTokens = chunk.CacheWriteInputTokens
					cacheWrite1hInputTokens = chunk.CacheWrite1hInputTokens
				}
				if chunk.Err != nil {
					lastErr = chunk.Err
				}
				select {
				case out <- chunk:
				case <-ctx.Done():
					lastErr = ctx.Err()
					break loop
				}
			}
			costUSD := def.ComputeUsageCostUSD(domain.TokenUsage{
				InputTokens:             inputTokens,
				CachedInputTokens:       cachedInputTokens,
				CacheWriteInputTokens:   cacheWriteInputTokens,
				CacheWrite1hInputTokens: cacheWrite1hInputTokens,
				OutputTokens:            outputTokens,
			})
			ev := InferenceLoggedEvent{
				OrgID:             authDomain.OrgIDFromContext(ctx),
				ModelID:           input.ModelID,
				ModelDefKey:       model.ModelDefinitionKey,
				Provider:          string(def.Provider),
				InputTokens:       inputTokens,
				OutputTokens:      outputTokens,
				CachedInputTokens: cachedInputTokens,
				CostUSD:           costUSD,
				LatencyMs:         time.Since(streamStart).Milliseconds(),
				Source:            "direct",
			}
			if lastErr != nil {
				ev.Status = "error"
				ev.ErrorMessage = lastErr.Error()
			} else {
				ev.Status = "success"
			}
			s.inferBus.Emit(ev)
		}()
		return out, nil
	}

	// Wrap the channel to accumulate and persist the assistant reply.
	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		var buf []byte
		var lastErr error
		var inputTokens, outputTokens, cachedInputTokens, cacheWriteInputTokens, cacheWrite1hInputTokens int64
	loop:
		for chunk := range upstream {
			if chunk.Delta != "" {
				buf = append(buf, chunk.Delta...)
			}
			if chunk.Done {
				inputTokens = chunk.InputTokens
				outputTokens = chunk.OutputTokens
				cachedInputTokens = chunk.CachedInputTokens
				cacheWriteInputTokens = chunk.CacheWriteInputTokens
				cacheWrite1hInputTokens = chunk.CacheWrite1hInputTokens
			}
			if chunk.Err != nil {
				lastErr = chunk.Err
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				// Client disconnected; drain upstream so we still capture the full reply.
				for rem := range upstream {
					if rem.Delta != "" {
						buf = append(buf, rem.Delta...)
					}
					if rem.Done {
						inputTokens = rem.InputTokens
						outputTokens = rem.OutputTokens
						cachedInputTokens = rem.CachedInputTokens
						cacheWriteInputTokens = rem.CacheWriteInputTokens
						cacheWrite1hInputTokens = rem.CacheWrite1hInputTokens
					}
				}
				break loop
			}
		}
		if len(buf) > 0 {
			persistOrgID := authDomain.OrgIDFromContext(ctx)
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := s.conversationRepo.AddMessage(persistCtx, persistOrgID, &domain.ConversationMessage{
				ID:             typeid.MustGenerate("cmsg").String(),
				ConversationID: convID,
				Role:           "assistant",
				Content:        string(buf),
			}); err != nil {
				slog.Error("persist assistant message", "convID", convID, "err", err)
			}
		}
		if shouldLog(ctx) {
			costUSD := def.ComputeUsageCostUSD(domain.TokenUsage{
				InputTokens:             inputTokens,
				CachedInputTokens:       cachedInputTokens,
				CacheWriteInputTokens:   cacheWriteInputTokens,
				CacheWrite1hInputTokens: cacheWrite1hInputTokens,
				OutputTokens:            outputTokens,
			})
			ev := InferenceLoggedEvent{
				OrgID:             authDomain.OrgIDFromContext(ctx),
				ModelID:           input.ModelID,
				ModelDefKey:       model.ModelDefinitionKey,
				Provider:          string(def.Provider),
				InputTokens:       inputTokens,
				OutputTokens:      outputTokens,
				CachedInputTokens: cachedInputTokens,
				CostUSD:           costUSD,
				LatencyMs:         time.Since(streamStart).Milliseconds(),
				Source:            "direct",
			}
			if lastErr != nil {
				ev.Status = "error"
				ev.ErrorMessage = lastErr.Error()
			} else {
				ev.Status = "success"
			}
			s.inferBus.Emit(ev)
		}
	}()
	return out, nil
}

// --- Embedding ---

func (s *service) Embed(ctx context.Context, modelID string, text string) ([]float32, error) {
	model, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), modelID)
	if err != nil {
		return nil, fmt.Errorf("embedding model not found: %w", err)
	}
	def, ok := domain.FindModelDefinition(model.ModelDefinitionKey)
	if !ok {
		return nil, domain.ErrModelDefinitionNotFound
	}
	if !def.HasCapability("embedding") {
		return nil, fmt.Errorf("model %q (%s) does not have the 'embedding' capability — register an embedding model (e.g. openai/text-embedding-3-small) and use its ID instead", model.ModelDefinitionKey, modelID)
	}
	cfg, err := s.configRepo.FindByModelID(ctx, authDomain.OrgIDFromContext(ctx), modelID)
	if err != nil {
		return nil, domain.ErrModelNotConfigured
	}
	return s.proxy.SendEmbedding(ctx, &def, cfg, text)
}

// --- Asynchronous job management ---

func (s *service) SubmitJob(ctx context.Context, input SubmitJobRequest) (*domain.Job, error) {
	model, err := s.modelRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), input.ModelID)
	if err != nil {
		return nil, err
	}
	if _, ok := domain.FindModelDefinition(model.ModelDefinitionKey); !ok {
		return nil, domain.ErrModelDefinitionNotFound
	}

	job := &domain.Job{
		ID:          typeid.MustGenerate("job").String(),
		OrgID:       authDomain.OrgIDFromContext(ctx),
		ModelID:     input.ModelID,
		Status:      domain.JobStatusPending,
		Fields:      input.Fields,
		Options:     input.Options,
		CallbackURL: input.CallbackURL,
	}

	if err := s.jobRepo.Create(ctx, job); err != nil {
		return nil, err
	}

	// Dispatch for background processing.
	// In Lambda mode this publishes to SQS (SQSQueueURL config is set).
	// In local dev mode the dispatcher spawns a goroutine.
	if err := s.dispatcher.Dispatch(ctx, job.ID); err != nil {
		job.Status = domain.JobStatusFailed
		_ = s.jobRepo.Update(ctx, job)
		return nil, err
	}

	return job, nil
}

func (s *service) GetJob(ctx context.Context, id string) (*domain.Job, error) {
	return s.jobRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
}

func (s *service) ListJobs(ctx context.Context, slice pagination.Slice) (pagination.Paginated[domain.Job], error) {
	jobs, total, err := s.jobRepo.List(ctx, authDomain.OrgIDFromContext(ctx), slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[domain.Job]{}, err
	}
	return pagination.New(jobs, total, slice), nil
}

// ProcessJob delegates to the injected JobProcessor.
// Keeps the method on Service so the HTTP handler (/jobs/:id/process) works unchanged.
func (s *service) ProcessJob(ctx context.Context, jobID string) error {
	return s.processor.ProcessJob(ctx, jobID)
}

func (s *service) ListHealthTargets(ctx context.Context) ([]HealthTarget, error) {
	models, err := s.modelRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	cfgs, err := s.configRepo.ListAllByModelIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	cfgByID := make(map[string]*domain.ModelConfiguration, len(cfgs))
	for i := range cfgs {
		cfgByID[cfgs[i].ModelID] = &cfgs[i]
	}
	targets := make([]HealthTarget, 0, len(models))
	for _, m := range models {
		cfg, ok := cfgByID[m.ID]
		if !ok {
			continue
		}
		def, found := domain.FindModelDefinition(m.ModelDefinitionKey)
		if !found {
			continue
		}
		apiKey := cfg.APIKey
		if len(cfg.APIKeyPool) > 0 {
			apiKey = cfg.APIKeyPool[0]
		}
		targets = append(targets, HealthTarget{
			ModelID:     m.ID,
			ModelDefKey: m.ModelDefinitionKey,
			Provider:    string(def.Provider),
			BaseURL:     cfg.BaseURL,
			APIKey:      apiKey,
		})
	}
	return targets, nil
}

// ── MCP Server CRUD ───────────────────────────────────────────────────────────

func (s *service) ListMCPServers(ctx context.Context) ([]MCPServerResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	servers, err := s.mcpServerRepo.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]MCPServerResponse, len(servers))
	for i := range servers {
		out[i] = toMCPServerResponse(&servers[i])
	}
	return out, nil
}

func (s *service) CreateMCPServer(ctx context.Context, input CreateMCPServerInput) (*MCPServerResponse, error) {
	authType := input.AuthType
	if authType == "" {
		authType = "none"
	}
	server := &domain.MCPServer{
		ID:           typeid.MustGenerate("rmcp").String(),
		OrgID:        authDomain.OrgIDFromContext(ctx),
		Name:         input.Name,
		Description:  input.Description,
		URL:          input.URL,
		AuthType:     authType,
		AuthToken:    input.AuthToken,
		AuthHeader:   input.AuthHeader,
		ExtraHeaders: input.ExtraHeaders,
		TimeoutSecs:  input.TimeoutSecs,
	}
	if server.TimeoutSecs <= 0 {
		server.TimeoutSecs = 30
	}
	if err := s.mcpServerRepo.Create(ctx, server); err != nil {
		return nil, err
	}
	resp := toMCPServerResponse(server)
	return &resp, nil
}

func (s *service) GetMCPServer(ctx context.Context, serverID string) (*MCPServerResponse, error) {
	server, err := s.mcpServerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), serverID)
	if err != nil {
		return nil, err
	}
	resp := toMCPServerResponse(server)
	return &resp, nil
}

func (s *service) UpdateMCPServer(ctx context.Context, serverID string, input UpdateMCPServerInput) (*MCPServerResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	server, err := s.mcpServerRepo.FindByID(ctx, orgID, serverID)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		server.Name = *input.Name
	}
	if input.Description != nil {
		server.Description = *input.Description
	}
	if input.URL != nil {
		server.URL = *input.URL
	}
	if input.AuthType != nil {
		server.AuthType = *input.AuthType
	}
	if input.AuthToken != nil {
		server.AuthToken = *input.AuthToken
	}
	if input.AuthHeader != nil {
		server.AuthHeader = *input.AuthHeader
	}
	if input.ExtraHeaders != nil {
		server.ExtraHeaders = *input.ExtraHeaders
	}
	if input.TimeoutSecs != nil {
		server.TimeoutSecs = *input.TimeoutSecs
	}
	if err := s.mcpServerRepo.Update(ctx, server); err != nil {
		return nil, err
	}
	resp := toMCPServerResponse(server)
	return &resp, nil
}

func (s *service) DeleteMCPServer(ctx context.Context, serverID string) error {
	orgID := authDomain.OrgIDFromContext(ctx)
	if err := s.mcpServerRepo.Delete(ctx, orgID, serverID); err != nil {
		return err
	}
	s.mcpServerBus.EmitDeleted(MCPServerDeletedEvent{OrgID: orgID, ServerID: serverID})
	return nil
}
