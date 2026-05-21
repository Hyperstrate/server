package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/modules/router/infrastructure/mcp"
)

// mcpMessage is a lightweight conversation turn for the MCP tool loop.
type mcpMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCalls  any    `json:"tool_calls,omitempty"`
}

type mcpExecutionTrace struct {
	Turns           int
	ToolCalls       int
	ToolSuccesses   int
	ToolErrors      int
	ReinferCalls    int
	InputTokens     int64
	OutputTokens    int64
	CostUSD         float64
	MaxTurnsReached bool
	LastError       string
	ToolNames       []string
	ToolCaptures    []ToolCallCapture
}

func (t mcpExecutionTrace) detail() string {
	parts := []string{
		fmt.Sprintf("%d turn(s)", t.Turns),
		fmt.Sprintf("%d tool call(s)", t.ToolCalls),
		fmt.Sprintf("%d ok", t.ToolSuccesses),
	}
	if t.ToolErrors > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", t.ToolErrors))
	}
	if t.ReinferCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d follow-up inference(s)", t.ReinferCalls))
	}
	if t.InputTokens > 0 || t.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d↑ %d↓ tok", t.InputTokens, t.OutputTokens))
	}
	if t.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.5f", t.CostUSD))
	}
	if len(t.ToolNames) > 0 {
		parts = append(parts, "tools: "+strings.Join(uniqueStrings(t.ToolNames), ", "))
	}
	if t.MaxTurnsReached {
		parts = append(parts, "max turns reached")
	}
	if t.LastError != "" {
		parts = append(parts, "last error: "+truncateStepDetail(t.LastError, 180))
	}
	return strings.Join(parts, " · ")
}

func (p *featurePipeline) prepareMCPToolOptions(
	ctx context.Context,
	features []domain.RouterFeature,
	options map[string]any,
	addStep func(PipelineStep, time.Duration),
) map[string]any {
	for _, f := range features {
		if f.FeatureType != domain.FeatureMCPTools || !f.IsEnabled {
			continue
		}
		if blocked, detail := mcpGovernanceBlocked(ctx, f.Config, options); blocked {
			addStep(PipelineStep{Phase: 4, Kind: "mcp_governance", Name: "MCP Governance", Outcome: "blocked", Detail: detail}, 0)
			next := copyOptions(options)
			next["_mcp_governance_blocked"] = detail
			return next
		}
		clients := p.mcpClientsFromCfg(ctx, f.Config)
		tools, lastErr := p.listMCPTools(ctx, clients, 4, addStep)
		tools = filterMCPTools(tools, f.Config)
		if len(tools) == 0 {
			if lastErr == "" {
				lastErr = "no MCP tools discovered"
			}
			if len(clients) == 0 {
				lastErr = "no MCP clients configured"
			}
			addStep(PipelineStep{Phase: 4, Kind: "mcp_tools_list", Name: "MCP Tool Discovery", Outcome: "skipped", Detail: lastErr}, 0)
			return options
		}
		next := copyOptions(options)
		next["tools"] = appendToolOptions(next["tools"], tools)
		next["_mcp_tools_injected"] = true
		next["_mcp_tool_count"] = len(tools)
		return next
	}
	return options
}

func (p *featurePipeline) listMCPTools(
	ctx context.Context,
	clients []*mcp.Client,
	phase int,
	addTraceStep func(PipelineStep, time.Duration),
) ([]map[string]any, string) {
	if len(clients) == 0 {
		return nil, "no MCP clients configured"
	}
	// Fast path: single server needs no goroutine overhead.
	if len(clients) == 1 {
		t0 := time.Now()
		tools, err := clients[0].ListTools(ctx)
		if err != nil {
			addTraceStep(PipelineStep{Phase: phase, Kind: "mcp_tools_list", Name: "MCP Tool Discovery", Outcome: "error", Detail: fmt.Sprintf("server 1: %s", truncateStepDetail(err.Error(), 220))}, time.Since(t0))
			return nil, err.Error()
		}
		addTraceStep(PipelineStep{Phase: phase, Kind: "mcp_tools_list", Name: "MCP Tool Discovery", Outcome: "success", Detail: fmt.Sprintf("server 1: %d tool(s)", len(tools))}, time.Since(t0))
		return tools, ""
	}

	// Poll all servers in parallel — sequential polling adds latencies; parallel
	// polling is bounded by the slowest server.
	type result struct {
		tools []map[string]any
		err   error
		dur   time.Duration
	}
	results := make([]result, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(idx int, client *mcp.Client) {
			defer wg.Done()
			t0 := time.Now()
			tools, err := client.ListTools(ctx)
			results[idx] = result{tools: tools, err: err, dur: time.Since(t0)}
		}(i, c)
	}
	wg.Wait()

	// Aggregate results in server order so trace steps are deterministic.
	allTools := make([]map[string]any, 0)
	lastErr := ""
	for i, r := range results {
		if r.err != nil {
			lastErr = r.err.Error()
			addTraceStep(PipelineStep{Phase: phase, Kind: "mcp_tools_list", Name: "MCP Tool Discovery", Outcome: "error", Detail: fmt.Sprintf("server %d: %s", i+1, truncateStepDetail(r.err.Error(), 220))}, r.dur)
		} else {
			allTools = append(allTools, r.tools...)
			addTraceStep(PipelineStep{Phase: phase, Kind: "mcp_tools_list", Name: "MCP Tool Discovery", Outcome: "success", Detail: fmt.Sprintf("server %d: %d tool(s)", i+1, len(r.tools))}, r.dur)
		}
	}
	return allTools, lastErr
}

func appendToolOptions(existing any, tools []map[string]any) any {
	if len(tools) == 0 {
		return existing
	}
	if existing == nil {
		return tools
	}
	switch v := existing.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(v)+len(tools))
		out = append(out, v...)
		out = append(out, tools...)
		return out
	case []any:
		out := make([]any, 0, len(v)+len(tools))
		out = append(out, v...)
		for _, tool := range tools {
			out = append(out, tool)
		}
		return out
	default:
		return tools
	}
}

func toolOptionsFromAny(value any) []map[string]any {
	switch v := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if tool, ok := item.(map[string]any); ok {
				out = append(out, tool)
			}
		}
		return out
	default:
		return nil
	}
}

func mcpGovernanceBlocked(ctx context.Context, cfg map[string]any, options map[string]any) (bool, string) {
	if boolCfg(cfg, "require_approval", false) {
		approved, _ := options["mcp_approved"].(bool)
		if !approved {
			return true, "request requires mcp_approved=true"
		}
	}
	allowedTeams := extractStringSlice(cfg["allowed_team_ids"])
	if len(allowedTeams) > 0 {
		for _, teamID := range authDomain.CallerTeamIDsFromContext(ctx) {
			if containsString(allowedTeams, teamID) {
				return false, ""
			}
		}
		return true, "caller team is not allowed to use MCP tools"
	}
	return false, ""
}

func filterMCPTools(tools []map[string]any, cfg map[string]any) []map[string]any {
	if len(tools) == 0 {
		return tools
	}
	allowed := stringSet(extractStringSlice(cfg["allowed_tools"]))
	blocked := stringSet(extractStringSlice(cfg["blocked_tools"]))
	if len(allowed) == 0 && len(blocked) == 0 {
		return tools
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := mcpToolOptionName(tool)
		if name == "" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[name]; !ok {
				continue
			}
		}
		if _, ok := blocked[name]; ok {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func mcpToolOptionName(tool map[string]any) string {
	fn, _ := tool["function"].(map[string]any)
	if fn != nil {
		name, _ := fn["name"].(string)
		return name
	}
	name, _ := tool["name"].(string)
	return name
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

// executeMCPTools runs the MCP agentic loop: dispatches tool_calls to configured
// MCP servers, injects results as tool messages, and re-infers until the model
// returns a plain content response or max_turns is reached.
func (p *featurePipeline) executeMCPTools(
	ctx context.Context,
	inferencer ModelInferencer,
	modelID string,
	fields map[string]string,
	options map[string]any,
	initial *RouteInferResult,
	cfg map[string]any,
	steps *[]PipelineStep,
	start time.Time,
) (*RouteInferResult, mcpExecutionTrace, error) {
	var trace mcpExecutionTrace
	addTraceStep := func(s PipelineStep, d time.Duration) {
		s.DurationMs = float64(d.Microseconds()) / 1000.0
		s.OffsetMs = float64((time.Since(start) - d).Microseconds()) / 1000.0
		*steps = append(*steps, s)
	}

	maxTurns := 10
	if v, ok := cfg["max_turns"]; ok {
		if n, ok := v.(float64); ok && int(n) > 0 {
			maxTurns = int(n)
		}
	}
	if blocked, detail := mcpGovernanceBlocked(ctx, cfg, options); blocked {
		trace.LastError = detail
		addTraceStep(PipelineStep{Phase: 6, Kind: "mcp_governance", Name: "MCP Governance", Outcome: "blocked", Detail: detail}, 0)
		return initial, trace, domain.ErrRequestBlocked
	}

	clients := p.mcpClientsFromCfg(ctx, cfg)
	if len(clients) == 0 {
		trace.LastError = "no MCP clients configured"
		return initial, trace, nil
	}

	history := make([]mcpMessage, 0, 8)
	if sp := fields["systemPrompt"]; sp != "" {
		history = append(history, mcpMessage{Role: "system", Content: sp})
	}
	if prompt := fields["prompt"]; prompt != "" {
		history = append(history, mcpMessage{Role: "user", Content: prompt})
	}

	allTools := toolOptionsFromAny(options["tools"])
	if _, alreadyInjected := options["_mcp_tools_injected"]; !alreadyInjected || len(allTools) == 0 {
		var lastErr string
		allTools, lastErr = p.listMCPTools(ctx, clients, 6, addTraceStep)
		allTools = filterMCPTools(allTools, cfg)
		trace.LastError = lastErr
	}
	if len(allTools) > 0 {
		options = copyOptions(options)
		options["tools"] = allTools
		// A client may force a specific tool for the first turn. After Hyperstrate
		// executes that tool, follow-up inference must be allowed to answer instead
		// of being forced to call the same tool until max_turns.
		if _, forced := options["tool_choice"]; forced {
			options["tool_choice"] = "auto"
		}
	} else {
		trace.LastError = "no MCP tools discovered"
		return initial, trace, nil
	}

	current := initial
	for turn := 0; turn < maxTurns && len(current.ToolCalls) > 0; turn++ {
		trace.Turns++
		var assistantToolCalls any
		if err := json.Unmarshal(current.ToolCalls, &assistantToolCalls); err == nil {
			history = append(history, mcpMessage{Role: "assistant", ToolCalls: assistantToolCalls})
		} else {
			history = append(history, mcpMessage{Role: "assistant", Content: string(current.ToolCalls)})
		}

		var toolResults []mcpMessage
		var rawCalls []map[string]any
		if err := json.Unmarshal(current.ToolCalls, &rawCalls); err != nil {
			trace.LastError = err.Error()
			addTraceStep(PipelineStep{Phase: 6, Kind: "mcp_tool_call", Name: "MCP Tool Call", Outcome: "error", Detail: "invalid tool_calls JSON: " + truncateStepDetail(err.Error(), 220)}, 0)
			return current, trace, err
		}
		for _, tc := range rawCalls {
			t0 := time.Now()
			name, args, parseErr := mcpToolCallNameArgs(tc)
			callID, _ := tc["id"].(string)
			trace.ToolCalls++
			if name != "" {
				trace.ToolNames = append(trace.ToolNames, name)
			}

			result := ""
			var callErr error
			if parseErr != nil {
				callErr = parseErr
			}
			if callErr == nil {
				for _, client := range clients {
					if r, err := client.CallTool(ctx, name, args); err == nil {
						result = r
						callErr = nil
						break
					} else {
						callErr = err
					}
				}
			}
			if callErr != nil {
				trace.ToolErrors++
				trace.LastError = callErr.Error()
				addTraceStep(PipelineStep{Phase: 6, Kind: "mcp_tool_call", Name: "MCP Tool Call", Outcome: "error", Detail: mcpToolCallDetail(name, callID, args, "", callErr)}, time.Since(t0))
			} else {
				trace.ToolSuccesses++
				addTraceStep(PipelineStep{Phase: 6, Kind: "mcp_tool_call", Name: "MCP Tool Call", Outcome: "success", Detail: mcpToolCallDetail(name, callID, args, result, nil)}, time.Since(t0))
			}
			capture := ToolCallCapture{
				ToolName:        name,
				ToolCallID:      callID,
				RequestPayload:  marshalStepValue(args, 100_000),
				ResponsePayload: result,
				ResponseChars:   len(result),
			}
			if callErr != nil {
				capture.ErrorMessage = callErr.Error()
			}
			trace.ToolCaptures = append(trace.ToolCaptures, capture)
			toolResults = append(toolResults, mcpMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: callID,
				ToolName:   name,
			})
		}
		history = append(history, toolResults...)

		loopFields := make(map[string]string)
		for k, v := range fields {
			loopFields[k] = v
		}
		loopFields["prompt"] = ""
		histJSON, _ := json.Marshal(history)
		loopFields["_history"] = string(histJSON)

		t0Reinfer := time.Now()
		next, err := inferencer.InferModel(ctx, modelID, loopFields, options)
		if err != nil {
			trace.LastError = err.Error()
			addTraceStep(PipelineStep{Phase: 6, Kind: "mcp_reinfer", Name: "MCP Follow-up Inference", Outcome: "error", Detail: truncateStepDetail(err.Error(), 220)}, time.Since(t0Reinfer))
			current.ToolCallCaptures = append([]ToolCallCapture(nil), trace.ToolCaptures...)
			return current, trace, err
		}
		trace.ReinferCalls++
		trace.InputTokens += next.InputTokens
		trace.OutputTokens += next.OutputTokens
		trace.CostUSD += next.CostUSD
		addTraceStep(PipelineStep{Phase: 6, Kind: "mcp_reinfer", Name: "MCP Follow-up Inference", Outcome: "success", Detail: fmt.Sprintf("%s · %d↑ %d↓ tok · $%.5f", next.ModelDefKey, next.InputTokens, next.OutputTokens, next.CostUSD)}, time.Since(t0Reinfer))
		current = &RouteInferResult{
			Content:                 next.Content,
			SelectedModelID:         modelID,
			ModelDefKey:             next.ModelDefKey,
			Provider:                next.Provider,
			InputTokens:             current.InputTokens + next.InputTokens,
			OutputTokens:            current.OutputTokens + next.OutputTokens,
			CachedInputTokens:       current.CachedInputTokens + next.CachedInputTokens,
			CacheWriteInputTokens:   current.CacheWriteInputTokens + next.CacheWriteInputTokens,
			CacheWrite1hInputTokens: current.CacheWrite1hInputTokens + next.CacheWrite1hInputTokens,
			CostUSD:                 current.CostUSD + next.CostUSD,
			ABVariant:               current.ABVariant,
			ToolCalls:               next.ToolCalls,
			ToolCallCaptures:        append([]ToolCallCapture(nil), trace.ToolCaptures...),
		}
	}
	if current != nil {
		current.ToolCallCaptures = append([]ToolCallCapture(nil), trace.ToolCaptures...)
	}
	if len(current.ToolCalls) > 0 {
		trace.MaxTurnsReached = true
	}
	return current, trace, nil
}

func mcpToolCallNameArgs(tc map[string]any) (string, map[string]any, error) {
	fn, _ := tc["function"].(map[string]any)
	name, _ := fn["name"].(string)
	if name == "" {
		return "", nil, fmt.Errorf("missing function name")
	}
	args := map[string]any{}
	switch raw := fn["arguments"].(type) {
	case string:
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return name, args, fmt.Errorf("invalid arguments JSON: %w", err)
			}
		}
	case map[string]any:
		args = raw
	case nil:
	default:
		return name, args, fmt.Errorf("unsupported arguments type %T", raw)
	}
	return name, args, nil
}

func mcpToolCallDetail(name, callID string, args map[string]any, response string, callErr error) string {
	parts := []string{
		fmt.Sprintf("%s (%s)", emptyDefault(name, "unknown"), emptyDefault(callID, "no id")),
		"payload=" + marshalStepValue(args, 600),
	}
	if callErr != nil {
		parts = append(parts, "error="+truncateStepDetail(callErr.Error(), 300))
	} else {
		parts = append(parts, fmt.Sprintf("response=%s", truncateStepDetail(response, 1200)))
		parts = append(parts, fmt.Sprintf("response_chars=%d", len(response)))
	}
	return strings.Join(parts, " · ")
}

func marshalStepValue(value any, max int) string {
	if value == nil {
		return "{}"
	}
	b, err := json.Marshal(value)
	if err != nil {
		return truncateStepDetail(fmt.Sprintf("%v", value), max)
	}
	return truncateStepDetail(string(b), max)
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func truncateStepDetail(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func emptyDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// mcpClientsFromCfg builds MCP clients for a feature config. It prefers
// managed server IDs (resolved via mcpLoader for auth headers) over the legacy
// inline servers list (URL-only, no auth). Returns nil when nothing is configured.
func (p *featurePipeline) mcpClientsFromCfg(ctx context.Context, cfg map[string]any) []*mcp.Client {
	if p.mcpLoader != nil {
		if rawIDs, ok := cfg["server_ids"].([]any); ok && len(rawIDs) > 0 {
			ids := make([]string, 0, len(rawIDs))
			for _, v := range rawIDs {
				if s, ok := v.(string); ok && s != "" {
					ids = append(ids, s)
				}
			}
			if len(ids) > 0 {
				resolved, err := p.mcpLoader.GetMCPServers(ctx, ids)
				if err != nil {
					slog.Error("mcp loader failed", "err", err)
				} else {
					clients := make([]*mcp.Client, 0, len(resolved))
					for _, r := range resolved {
						clients = append(clients, mcp.NewClientWithConfig(r.URL, r.Headers, r.TimeoutSecs))
					}
					return clients
				}
			}
		}
	}

	servers, _ := cfg["servers"].([]any)
	clients := make([]*mcp.Client, 0, len(servers))
	for _, s := range servers {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if u, _ := m["url"].(string); u != "" {
			clients = append(clients, mcp.NewClient(u))
		}
	}
	return clients
}
