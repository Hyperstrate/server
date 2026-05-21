package observability

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	aiApplication "hyperstrate/server/internal/modules/ai/application"
	aiDomain "hyperstrate/server/internal/modules/ai/domain"
	authApplication "hyperstrate/server/internal/modules/auth/application"
	authHTTP "hyperstrate/server/internal/modules/auth/interfaces/http"
	"hyperstrate/server/internal/modules/observability/application"
	obsDomain "hyperstrate/server/internal/modules/observability/domain"
	"hyperstrate/server/internal/modules/observability/infrastructure/persistence"
	httptransport "hyperstrate/server/internal/modules/observability/interfaces/http"
	routerApplication "hyperstrate/server/internal/modules/router/application"
	"hyperstrate/server/internal/shared/audit"
	"hyperstrate/server/internal/shared/webhook"

	"github.com/gin-gonic/gin"
	"go.jetify.com/typeid/v2"
	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			persistence.NewInferenceLogRepository,
			persistence.NewAuditLogRepository,
			persistence.NewProviderHealthRepository,
			persistence.NewWebhookDeliveryRepository,
			persistence.NewInferencePayloadRepository,
			persistence.NewAgentSessionEventRepository,
			persistence.NewToolCallArchiveRepository,
			persistence.NewCompressionEventRepository,
			application.NewService,
			newModelNameFunc,
			newModelAliasFunc,
			newRouterNameFunc,
			httptransport.NewHandler,
		),
		fx.Invoke(registerRoutes),
		fx.Invoke(registerInferenceListeners),
		fx.Invoke(registerWebhookRecorder),
		fx.Invoke(registerAuditLogger),
		fx.Invoke(registerReplayFunc),
		fx.Invoke(startHealthMonitor),
	)
}

func newModelNameFunc() httptransport.ModelNameFunc {
	return func(defKey string) string {
		if def, ok := aiDomain.FindModelDefinition(defKey); ok && def.DisplayName != "" {
			return def.DisplayName
		}
		return defKey
	}
}

func newModelAliasFunc(svc aiApplication.Service) httptransport.ModelAliasFunc {
	return func(ctx context.Context, modelID string) string {
		if modelID == "" {
			return ""
		}
		m, err := svc.GetModel(ctx, modelID)
		if err != nil || m == nil {
			return ""
		}
		return m.Alias
	}
}

func newRouterNameFunc(svc routerApplication.Service) httptransport.RouterNameFunc {
	return func(ctx context.Context, id string) string {
		if id == "" {
			return ""
		}
		r, err := svc.GetRouter(ctx, id)
		if err != nil || r == nil {
			return id
		}
		return r.Name
	}
}

func registerRoutes(
	r *gin.Engine,
	handler *httptransport.Handler,
	sessionValidator authApplication.SessionValidator,
) {
	adminGroup := r.Group("")
	adminGroup.Use(authHTTP.RequireAdmin(sessionValidator))
	handler.RegisterRoutes(adminGroup)
}

// registerWebhookRecorder installs a global webhook delivery recorder so every
// Fire() call persists its result via the observability service.
func registerWebhookRecorder(obs application.Service) {
	webhook.SetRecorder(func(r webhook.DeliveryRecord) {
		if r.RouterID == "" {
			return
		}
		_ = obs.RecordWebhookDelivery(&obsDomain.WebhookDelivery{
			ID:         typeid.MustGenerate("whdlv").String(),
			RouterID:   r.RouterID,
			Event:      r.Event,
			URL:        r.URL,
			StatusCode: r.StatusCode,
			Success:    r.Success,
			ErrorMsg:   r.ErrorMsg,
			CreatedAt:  time.Now().UTC(),
		})
	})
}

// registerInferenceListeners wires the observability service as a listener on
// both the AI and router inference event buses. This keeps the observability
// module self-contained: it knows what it cares about, not the other way round.
func registerInferenceListeners(
	aiBus *aiApplication.InferenceEventBus,
	modelBus *aiApplication.ModelEventBus,
	routerBus *routerApplication.RouterInferenceEventBus,
	obs application.Service,
) {
	modelBus.OnDeleted(func(_ context.Context, e aiApplication.ModelDeletedEvent) error {
		return obs.DeleteProviderHealth(e.ModelID)
	})

	aiBus.OnLogged(func(e aiApplication.InferenceLoggedEvent) {
		obs.LogInference(application.InferenceEntry{
			OrgID:             e.OrgID,
			ModelID:           e.ModelID,
			ModelDefKey:       e.ModelDefKey,
			Provider:          e.Provider,
			InputTokens:       e.InputTokens,
			OutputTokens:      e.OutputTokens,
			CachedInputTokens: e.CachedInputTokens,
			CostUSD:           e.CostUSD,
			LatencyMs:         e.LatencyMs,
			Status:            e.Status,
			ErrorMessage:      e.ErrorMessage,
			Source:            e.Source,
		})
	})

	routerBus.OnLogged(func(e routerApplication.RouterInferenceLoggedEvent) {
		var traceJSON string
		if len(e.PipelineSteps) > 0 {
			if b, err := json.Marshal(e.PipelineSteps); err == nil {
				traceJSON = string(b)
			}
		}
		entry := application.InferenceEntry{
			OrgID:             e.OrgID,
			RouterID:          e.RouterID,
			VirtualKeyID:      e.VirtualKeyID,
			TeamID:            e.TeamID,
			UserID:            e.UserID,
			ModelID:           e.ModelID,
			SelectedTargetID:  e.SelectedTargetID,
			ModelDefKey:       e.ModelDefKey,
			Provider:          e.Provider,
			InputTokens:       e.InputTokens,
			OutputTokens:      e.OutputTokens,
			CachedInputTokens: e.CachedInputTokens,
			CostUSD:           e.CostUSD,
			LatencyMs:         e.LatencyMs,
			TTFTMs:            e.TTFTMs,
			Status:            e.Status,
			ErrorMessage:      e.ErrorMessage,
			Source:            "router",
			ABVariant:         e.ABVariant,
			PipelineTrace:     traceJSON,
			AgentSessionID:    e.AgentSessionID,
			Agent:             e.Agent,
			AgentRole:         e.AgentRole,
			ParentSessionID:   e.ParentSessionID,
			TurnIndex:         e.TurnIndex,
			CacheHit:          e.CacheHit,
			CacheHitType:      e.CacheHitType,
			StorePayloads:     e.StorePayloads,
			RequestFields:     e.RequestFields,
			ResponseContent:   e.ResponseContent,
		}
		entry.ToolCallCount, entry.ToolResultChars = toolStatsFromPipeline(e.PipelineSteps)
		entry.ToolArchives = toolArchivesFromCaptures(e.ToolCallCaptures)
		entry.CompressionEvents = compressionEventsFromPipeline(e.PipelineSteps)
		entry.QualityScore = qualityScoreForInference(entry)
		obs.LogInference(entry)
	})
}

func toolStatsFromPipeline(steps []routerApplication.PipelineStep) (int, int) {
	calls := 0
	chars := 0
	for _, step := range steps {
		if step.Kind != "mcp_tool_call" {
			continue
		}
		calls++
		if marker := "response_chars="; strings.Contains(step.Detail, marker) {
			after := step.Detail[strings.LastIndex(step.Detail, marker)+len(marker):]
			end := strings.IndexAny(after, " ·")
			if end >= 0 {
				after = after[:end]
			}
			if n, err := strconv.Atoi(strings.TrimSpace(after)); err == nil {
				chars += n
			}
		}
	}
	return calls, chars
}

func qualityScoreForInference(e application.InferenceEntry) int {
	score := 100
	if e.Status == "error" {
		score -= 25
	}
	if e.ToolCallCount >= 5 {
		score -= 10
	}
	if e.ToolResultChars > 20000 {
		score -= 10
	}
	if e.OutputTokens > 0 && e.InputTokens > 0 && e.OutputTokens > e.InputTokens*2 {
		score -= 5
	}
	if e.LatencyMs > 30000 {
		score -= 5
	}
	if score < 0 {
		return 0
	}
	return score
}

func toolArchivesFromCaptures(captures []routerApplication.ToolCallCapture) []obsDomain.ToolCallArchive {
	if len(captures) == 0 {
		return nil
	}
	out := make([]obsDomain.ToolCallArchive, 0, len(captures))
	for _, capture := range captures {
		out = append(out, obsDomain.ToolCallArchive{
			ToolName:        capture.ToolName,
			ToolCallID:      capture.ToolCallID,
			RequestPayload:  capture.RequestPayload,
			ResponsePayload: capture.ResponsePayload,
			ResponseChars:   capture.ResponseChars,
			ErrorMessage:    capture.ErrorMessage,
		})
	}
	return out
}

var compressionDetailRE = regexp.MustCompile(`(\d+)\s*(?:→|to)\s*(\d+)\s*chars`)

func compressionEventsFromPipeline(steps []routerApplication.PipelineStep) []obsDomain.CompressionEvent {
	var out []obsDomain.CompressionEvent
	for _, step := range steps {
		if step.Kind != "transform" || step.Outcome != "applied" {
			continue
		}
		match := compressionDetailRE.FindStringSubmatch(step.Detail)
		if len(match) != 3 {
			continue
		}
		before, _ := strconv.Atoi(match[1])
		after, _ := strconv.Atoi(match[2])
		if before <= 0 || after <= 0 || after >= before {
			continue
		}
		out = append(out, obsDomain.CompressionEvent{
			FeatureName:          step.Name,
			BeforeChars:          before,
			AfterChars:           after,
			SavedChars:           before - after,
			EstimatedTokensSaved: (before - after) / 4,
			Exact:                true,
			Detail:               step.Detail,
		})
	}
	return out
}

func registerReplayFunc(handler *httptransport.Handler, routerSvc routerApplication.Service) {
	handler.SetReplayFunc(func(ctx context.Context, routerID string, fields map[string]string) (*httptransport.ReplayResult, error) {
		start := time.Now()
		result, err := routerSvc.RouteInfer(ctx, routerID, routerApplication.RouteInferInput{Fields: fields, BypassCache: true})
		if err != nil {
			return nil, err
		}
		return &httptransport.ReplayResult{
			Content:      result.Content,
			InputTokens:  result.InputTokens,
			OutputTokens: result.OutputTokens,
			CostUSD:      result.CostUSD,
			LatencyMs:    time.Since(start).Milliseconds(),
		}, nil
	})
}

func registerAuditLogger(obs application.Service) {
	audit.SetLogger(func(ctx context.Context, r audit.Record) {
		obs.LogAudit(ctx, application.AuditEntry{
			OrgID:      r.OrgID,
			UserEmail:  r.UserEmail,
			Action:     r.Action,
			Resource:   r.Resource,
			ResourceID: r.ResourceID,
			Details:    r.Details,
			IPAddress:  r.IPAddress,
		})
	})
}

func startHealthMonitor(lc fx.Lifecycle, aiSvc aiApplication.Service, obs application.Service) {
	getModels := func(ctx context.Context) ([]application.ModelHealthTarget, error) {
		targets, err := aiSvc.ListHealthTargets(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]application.ModelHealthTarget, len(targets))
		for i, t := range targets {
			out[i] = application.ModelHealthTarget{
				ModelID:     t.ModelID,
				ModelDefKey: t.ModelDefKey,
				Provider:    t.Provider,
				BaseURL:     t.BaseURL,
				APIKey:      t.APIKey,
			}
		}
		return out, nil
	}
	monitor := application.NewHealthMonitor(obs, getModels, 2*time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go monitor.Start(ctx)
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}
