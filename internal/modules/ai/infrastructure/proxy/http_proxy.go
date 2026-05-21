package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/domain"
)

// HTTPProxy forwards inference requests to upstream AI providers over HTTP.
// It formats requests according to each provider's API contract and extracts
// the generated content from the response.
//
// keyCounters holds per-config atomic counters for lock-free round-robin
// selection across the APIKeyPool when multiple keys are configured.
type HTTPProxy struct {
	keyCounters sync.Map // configID → *atomic.Uint64
}

func NewHTTPProxy() application.Proxy {
	return &HTTPProxy{}
}

// selectAPIKey returns the API key to use for a request. When cfg.APIKeyPool
// is non-empty it round-robins across the pool; otherwise it falls back to
// cfg.APIKey.
func (p *HTTPProxy) selectAPIKey(cfg *domain.ModelConfiguration) string {
	if len(cfg.APIKeyPool) == 0 {
		return cfg.APIKey
	}
	// Load or create an atomic counter keyed by config ID.
	v, _ := p.keyCounters.LoadOrStore(cfg.ID, new(atomic.Uint64))
	counter := v.(*atomic.Uint64)
	idx := counter.Add(1) - 1
	return cfg.APIKeyPool[idx%uint64(len(cfg.APIKeyPool))]
}

func (p *HTTPProxy) Send(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (*application.ProxyResponse, error) {
	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	targetURL, body, headers := p.buildRequest(def, cfg, req)

	// Merge per-model extra headers. Authorization is reserved for the
	// provider-specific auth set above; skip it from ExtraHeaders to prevent
	// accidentally overwriting a correctly-formatted Bearer token.
	for k, v := range cfg.ExtraHeaders {
		if strings.EqualFold(k, "authorization") {
			continue
		}
		headers[k] = v
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build proxy request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	// Bedrock uses SigV4 signing via its own HTTP helper.
	if def.Provider == domain.ProviderBedrock {
		resp, err := bedrockDo(ctx, targetURL, body, headers)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", domain.ErrProxyFailed, err.Error())
		}
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		return p.parseResponse(def.Provider, respBytes)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrProxyFailed, err.Error())
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read proxy response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: upstream returned %d: %s",
			domain.ErrProxyFailed, resp.StatusCode, string(respBytes))
	}

	return p.parseResponse(def.Provider, respBytes)
}

var internalProxyOptionKeys = map[string]struct{}{
	"agent":                   {},
	"agent_role":              {},
	"agent_session_id":        {},
	"agent_user_id":           {},
	"conversation_id":         {},
	"parent_agent":            {},
	"parent_agent_session_id": {},
	"parent_agent_user_id":    {},
	"parent_session_id":       {},
	"parent_subject_user_id":  {},
	"parent_user_id":          {},
	"session_id":              {},
	"subject_user_id":         {},
	"turn_index":              {},
	"user_id":                 {},
}

// dropOrphanToolChoice removes tool_choice from body when tools is absent or
// empty. OpenAI-compatible APIs reject tool_choice without a tools array.
func dropOrphanToolChoice(body map[string]any) {
	tools, _ := body["tools"].([]any)
	if len(tools) == 0 {
		delete(body, "tool_choice")
	}
}

func shouldForwardOption(key string) bool {
	if strings.HasPrefix(key, "_") {
		return false
	}
	_, internal := internalProxyOptionKeys[key]
	return !internal
}

// mergeOpts copies model/provider options into body, skipping router-owned
// metadata. Agent/session tracking keys are observability inputs and must not be
// forwarded verbatim to upstream providers.
func mergeOpts(body map[string]any, options map[string]any) {
	for k, v := range options {
		if shouldForwardOption(k) {
			body[k] = v
		}
	}
}

// buildRequest returns (url, requestBody, headers) for the given provider.
func (p *HTTPProxy) buildRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	switch def.Provider {
	case domain.ProviderOpenAi, domain.ProviderMistral, domain.ProviderVLLM, domain.ProviderLocalAI,
		domain.ProviderGroq:
		return p.chatGPTRequest(def, cfg, req)
	case domain.ProviderAzureOpenAI:
		return p.azureOpenAIRequest(def, cfg, req)
	case domain.ProviderAnthropic:
		return p.anthropicRequest(def, cfg, req)
	case domain.ProviderOllama:
		return p.ollamaRequest(def, cfg, req)
	case domain.ProviderGemini:
		return p.geminiRequest(def, cfg, req)
	case domain.ProviderKling:
		return p.klingRequest(def, cfg, req)
	case domain.ProviderCohere:
		return p.cohereRequest(def, cfg, req)
	case domain.ProviderBedrock:
		return p.bedrockConverseRequest(def, cfg, req)
	default: // ProviderCustom and any future additions
		return p.customRequest(cfg, req)
	}
}

// ── ChatGPT / Mistral (OpenAI-compatible) ────────────────────────────────────

// parseHistoryField decodes the JSON-encoded conversation history stored in
// fields["_history"] by the OpenAI-compat handler. Returns nil when absent.
func parseHistoryField(fields map[string]string) []application.HistoryMessage {
	raw, ok := fields["_history"]
	if !ok || raw == "" {
		return nil
	}
	var msgs []application.HistoryMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		return nil
	}
	return msgs
}

type functionTool struct {
	Name        string
	Description string
	Parameters  any
}

type canonicalToolCall struct {
	ID        string
	Name      string
	Arguments any
}

func openAIToolsFromOptions(options map[string]any) []functionTool {
	rawTools := mapSlice(options["tools"])
	tools := make([]functionTool, 0, len(rawTools))
	for _, raw := range rawTools {
		fn, _ := raw["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		desc, _ := fn["description"].(string)
		params := fn["parameters"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, functionTool{Name: name, Description: desc, Parameters: params})
	}
	return tools
}

func mapSlice(value any) []map[string]any {
	switch v := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func parseCanonicalToolCalls(raw json.RawMessage) []canonicalToolCall {
	if len(raw) == 0 {
		return nil
	}
	var calls []map[string]any
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil
	}
	out := make([]canonicalToolCall, 0, len(calls))
	for i, call := range calls {
		fn, _ := call["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		id, _ := call["id"].(string)
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		out = append(out, canonicalToolCall{
			ID:        id,
			Name:      name,
			Arguments: toolArgumentsValue(fn["arguments"]),
		})
	}
	return out
}

func canonicalToolCallsJSON(calls []canonicalToolCall) json.RawMessage {
	if len(calls) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(calls))
	for i, call := range calls {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		out = append(out, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": toolArgumentsString(call.Arguments),
			},
		})
	}
	b, _ := json.Marshal(out)
	return b
}

func toolArgumentsValue(value any) any {
	switch v := value.(type) {
	case string:
		if v == "" {
			return map[string]any{}
		}
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return decoded
		}
		return map[string]any{}
	case nil:
		return map[string]any{}
	default:
		return v
	}
}

func toolArgumentsString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return "{}"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "{}"
		}
		return string(b)
	}
}

func stripToolOptions(body map[string]any) {
	delete(body, "tools")
	delete(body, "tool_choice")
	delete(body, "toolConfig")
}

func convertOpenAIToolChoiceForAnthropic(value any) any {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		if strings.EqualFold(v, "required") {
			return map[string]any{"type": "any"}
		}
		return map[string]any{"type": v}
	case map[string]any:
		if fn, _ := v["function"].(map[string]any); fn != nil {
			if name, _ := fn["name"].(string); name != "" {
				return map[string]any{"type": "tool", "name": name}
			}
		}
		if typ, _ := v["type"].(string); typ != "" {
			return map[string]any{"type": typ}
		}
	}
	return nil
}

func convertOpenAIToolChoiceForGemini(value any) any {
	switch v := value.(type) {
	case string:
		switch strings.ToLower(v) {
		case "none":
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}}
		case "required", "any":
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}
		case "auto":
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}
		}
	case map[string]any:
		if fn, _ := v["function"].(map[string]any); fn != nil {
			if name, _ := fn["name"].(string); name != "" {
				return map[string]any{"functionCallingConfig": map[string]any{
					"mode":                 "ANY",
					"allowedFunctionNames": []string{name},
				}}
			}
		}
	}
	return nil
}

func convertOpenAIToolChoiceForBedrock(value any) any {
	switch v := value.(type) {
	case string:
		switch strings.ToLower(v) {
		case "auto":
			return map[string]any{"auto": map[string]any{}}
		case "required", "any":
			return map[string]any{"any": map[string]any{}}
		case "none":
			return nil
		}
	case map[string]any:
		if fn, _ := v["function"].(map[string]any); fn != nil {
			if name, _ := fn["name"].(string); name != "" {
				return map[string]any{"tool": map[string]any{"name": name}}
			}
		}
	}
	return nil
}

func applyAnthropicTools(body map[string]any, options map[string]any) {
	tools := openAIToolsFromOptions(options)
	if len(tools) == 0 {
		stripToolOptions(body)
		return
	}
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.Parameters,
		})
	}
	body["tools"] = converted
	if choice := convertOpenAIToolChoiceForAnthropic(options["tool_choice"]); choice != nil {
		body["tool_choice"] = choice
	} else {
		delete(body, "tool_choice")
	}
}

func applyGeminiTools(body map[string]any, options map[string]any) {
	tools := openAIToolsFromOptions(options)
	if len(tools) == 0 {
		stripToolOptions(body)
		return
	}
	declarations := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		declarations = append(declarations, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		})
	}
	body["tools"] = []map[string]any{{"functionDeclarations": declarations}}
	delete(body, "tool_choice")
	if choice := convertOpenAIToolChoiceForGemini(options["tool_choice"]); choice != nil {
		body["toolConfig"] = choice
	}
}

func applyBedrockTools(body map[string]any, options map[string]any) {
	tools := openAIToolsFromOptions(options)
	stripToolOptions(body)
	if len(tools) == 0 {
		return
	}
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, map[string]any{
			"toolSpec": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": map[string]any{"json": tool.Parameters},
			},
		})
	}
	toolConfig := map[string]any{"tools": converted}
	if choice := convertOpenAIToolChoiceForBedrock(options["tool_choice"]); choice != nil {
		toolConfig["toolChoice"] = choice
	}
	body["toolConfig"] = toolConfig
}

// buildChatMessages constructs the messages array for OpenAI-compatible APIs.
// withImage=true enables vision content parts for the user turn.
// If history is empty, falls back to the JSON-encoded _history field so that
// the OpenAI-compat handler can pass multi-turn context without changing interfaces.
func buildChatMessages(fields map[string]string, history []application.HistoryMessage, withImage bool) []any {
	if len(history) == 0 {
		history = parseHistoryField(fields)
	}
	messages := make([]any, 0, 3+len(history))
	if sp := fields["systemPrompt"]; sp != "" {
		messages = append(messages, map[string]any{"role": "system", "content": sp})
	}
	for _, h := range history {
		msg := map[string]any{"role": h.Role}
		if len(h.ToolCalls) > 0 {
			var calls any
			if err := json.Unmarshal(h.ToolCalls, &calls); err == nil {
				msg["content"] = nil
				msg["tool_calls"] = calls
			} else {
				msg["content"] = h.Content
			}
		} else {
			msg["content"] = h.Content
		}
		if h.ToolCallID != "" {
			msg["tool_call_id"] = h.ToolCallID
		}
		messages = append(messages, msg)
	}
	prompt := fields["prompt"]
	if prompt == "" && len(history) > 0 && fields["image"] == "" {
		return messages
	}
	if withImage {
		if imageField := fields["image"]; imageField != "" {
			imageURLs := parseStringOrJSONArray(imageField)
			content := make([]any, 0, len(imageURLs)+1)
			for _, u := range imageURLs {
				content = append(content, map[string]any{
					"type":      "image_url",
					"image_url": map[string]string{"url": u},
				})
			}
			content = append(content, map[string]any{"type": "text", "text": prompt})
			return append(messages, map[string]any{"role": "user", "content": content})
		}
	}
	return append(messages, map[string]any{"role": "user", "content": prompt})
}

func buildOllamaMessages(fields map[string]string, history []application.HistoryMessage) []any {
	if len(history) == 0 {
		history = parseHistoryField(fields)
	}
	messages := make([]any, 0, 3+len(history))
	if sp := fields["systemPrompt"]; sp != "" {
		messages = append(messages, map[string]any{"role": "system", "content": sp})
	}
	toolNamesByID := map[string]string{}
	for _, h := range history {
		msg := map[string]any{"role": h.Role}
		if calls := parseCanonicalToolCalls(h.ToolCalls); len(calls) > 0 {
			outCalls := make([]any, 0, len(calls))
			for i, call := range calls {
				toolNamesByID[call.ID] = call.Name
				outCalls = append(outCalls, map[string]any{
					"type": "function",
					"function": map[string]any{
						"index":     i,
						"name":      call.Name,
						"arguments": toolArgumentsValue(call.Arguments),
					},
				})
			}
			msg["content"] = h.Content
			msg["tool_calls"] = outCalls
		} else {
			msg["content"] = h.Content
		}
		if h.Role == "tool" {
			name := h.ToolName
			if name == "" {
				name = toolNamesByID[h.ToolCallID]
			}
			if name != "" {
				msg["tool_name"] = name
			}
		}
		messages = append(messages, msg)
	}
	if prompt := fields["prompt"]; prompt == "" && len(history) > 0 {
		return messages
	}
	return append(messages, map[string]any{"role": "user", "content": fields["prompt"]})
}

func buildAnthropicMessages(fields map[string]string, history []application.HistoryMessage) []any {
	if len(history) == 0 {
		history = parseHistoryField(fields)
	}
	messages := make([]any, 0, len(history)+1)
	pendingToolResults := make([]any, 0)
	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		messages = append(messages, map[string]any{"role": "user", "content": pendingToolResults})
		pendingToolResults = nil
	}
	for _, h := range history {
		switch h.Role {
		case "system":
			continue
		case "assistant":
			flushToolResults()
			if calls := parseCanonicalToolCalls(h.ToolCalls); len(calls) > 0 {
				parts := make([]any, 0, len(calls)+1)
				if h.Content != "" {
					parts = append(parts, map[string]any{"type": "text", "text": h.Content})
				}
				for _, call := range calls {
					parts = append(parts, map[string]any{
						"type":  "tool_use",
						"id":    call.ID,
						"name":  call.Name,
						"input": toolArgumentsValue(call.Arguments),
					})
				}
				messages = append(messages, map[string]any{"role": "assistant", "content": parts})
			} else {
				messages = append(messages, map[string]any{"role": "assistant", "content": h.Content})
			}
		case "tool":
			pendingToolResults = append(pendingToolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": h.ToolCallID,
				"content":     h.Content,
			})
		default:
			flushToolResults()
			messages = append(messages, map[string]any{"role": "user", "content": h.Content})
		}
	}
	flushToolResults()

	contentParts := make([]any, 0, 2)
	if imageField := fields["image"]; imageField != "" {
		for _, u := range parseStringOrJSONArray(imageField) {
			contentParts = append(contentParts, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type": "url",
					"url":  u,
				},
			})
		}
	}
	if prompt := fields["prompt"]; prompt != "" || len(contentParts) > 0 || len(messages) == 0 {
		contentParts = append(contentParts, map[string]any{"type": "text", "text": prompt})
		messages = append(messages, map[string]any{"role": "user", "content": contentParts})
	}
	return messages
}

func buildGeminiContents(fields map[string]string, history []application.HistoryMessage) []any {
	if len(history) == 0 {
		history = parseHistoryField(fields)
	}
	contents := make([]any, 0, len(history)+1)
	toolNamesByID := map[string]string{}
	for _, h := range history {
		switch h.Role {
		case "assistant":
			if calls := parseCanonicalToolCalls(h.ToolCalls); len(calls) > 0 {
				parts := make([]any, 0, len(calls)+1)
				if h.Content != "" {
					parts = append(parts, map[string]any{"text": h.Content})
				}
				for _, call := range calls {
					toolNamesByID[call.ID] = call.Name
					parts = append(parts, map[string]any{"functionCall": map[string]any{
						"name": call.Name,
						"args": toolArgumentsValue(call.Arguments),
					}})
				}
				contents = append(contents, map[string]any{"role": "model", "parts": parts})
			} else {
				contents = append(contents, map[string]any{"role": "model", "parts": []any{map[string]any{"text": h.Content}}})
			}
		case "tool":
			name := h.ToolName
			if name == "" {
				name = toolNamesByID[h.ToolCallID]
			}
			contents = append(contents, map[string]any{"role": "user", "parts": []any{map[string]any{
				"functionResponse": map[string]any{
					"name":     name,
					"response": map[string]any{"content": h.Content},
				},
			}}})
		case "system":
			continue
		default:
			contents = append(contents, map[string]any{"role": "user", "parts": []any{map[string]any{"text": h.Content}}})
		}
	}

	parts := make([]any, 0, 2)
	if imageURL := fields["image"]; imageURL != "" {
		parts = append(parts, map[string]any{
			"fileData": map[string]any{
				"mimeType": "image/jpeg",
				"fileUri":  imageURL,
			},
		})
	}
	if prompt := fields["prompt"]; prompt != "" || len(parts) > 0 || len(contents) == 0 {
		parts = append(parts, map[string]any{"text": prompt})
		contents = append(contents, map[string]any{"role": "user", "parts": parts})
	}
	return contents
}

func buildBedrockMessages(fields map[string]string, history []application.HistoryMessage) []map[string]any {
	if len(history) == 0 {
		history = parseHistoryField(fields)
	}
	messages := make([]map[string]any, 0, len(history)+1)
	pendingToolResults := make([]any, 0)
	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		messages = append(messages, map[string]any{"role": "user", "content": pendingToolResults})
		pendingToolResults = nil
	}
	for _, h := range history {
		switch h.Role {
		case "system":
			continue
		case "assistant":
			flushToolResults()
			if calls := parseCanonicalToolCalls(h.ToolCalls); len(calls) > 0 {
				parts := make([]any, 0, len(calls)+1)
				if h.Content != "" {
					parts = append(parts, map[string]any{"text": h.Content})
				}
				for _, call := range calls {
					parts = append(parts, map[string]any{"toolUse": map[string]any{
						"toolUseId": call.ID,
						"name":      call.Name,
						"input":     toolArgumentsValue(call.Arguments),
					}})
				}
				messages = append(messages, map[string]any{"role": "assistant", "content": parts})
			} else {
				messages = append(messages, map[string]any{"role": "assistant", "content": []map[string]any{{"text": h.Content}}})
			}
		case "tool":
			pendingToolResults = append(pendingToolResults, map[string]any{"toolResult": map[string]any{
				"toolUseId": h.ToolCallID,
				"content":   []map[string]any{{"text": h.Content}},
			}})
		default:
			flushToolResults()
			messages = append(messages, map[string]any{"role": "user", "content": []map[string]any{{"text": h.Content}}})
		}
	}
	flushToolResults()
	if prompt := fields["prompt"]; prompt != "" || len(messages) == 0 {
		messages = append(messages, map[string]any{"role": "user", "content": []map[string]any{{"text": prompt}}})
	}
	return messages
}

func (p *HTTPProxy) chatGPTRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	body := map[string]any{
		"model":    def.ModelID,
		"messages": buildChatMessages(req.Fields, req.History, true),
	}
	mergeOpts(body, req.Options)
	dropOrphanToolChoice(body)
	if schema, ok := req.Options["_structured_schema"]; ok {
		name, _ := req.Options["_structured_name"].(string)
		if name == "" {
			name = "response"
		}
		strict, _ := req.Options["_structured_strict"].(bool)
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   name,
				"schema": schema,
				"strict": strict,
			},
		}
	}
	return cfg.BaseURL + "/chat/completions", body, map[string]string{"Authorization": "Bearer " + p.selectAPIKey(cfg)}
}

// ── Anthropic ────────────────────────────────────────────────────────────────

func (p *HTTPProxy) anthropicRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	// Build effective system prompt: base + optional structured-output instruction.
	systemPrompt := req.Fields["systemPrompt"]
	if schema, ok := req.Options["_structured_schema"]; ok {
		schemaJSON, _ := json.Marshal(schema)
		instruction := "You must respond with valid JSON that exactly conforms to this JSON schema:\n" +
			string(schemaJSON) + "\n\nRespond only with the JSON object, no other text."
		if systemPrompt != "" {
			systemPrompt += "\n\n" + instruction
		} else {
			systemPrompt = instruction
		}
	}

	body := map[string]any{
		"model":      def.ModelID,
		"max_tokens": 1024,
		"messages":   buildAnthropicMessages(req.Fields, req.History),
	}

	// Prompt caching: wrap system prompt in an ephemeral cache_control block so
	// Anthropic caches the (potentially large) system prompt across requests.
	_, cacheEnabled := req.Options["_prompt_cache"]
	if systemPrompt != "" {
		if cacheEnabled {
			body["system"] = []map[string]any{
				{
					"type":          "text",
					"text":          systemPrompt,
					"cache_control": map[string]any{"type": "ephemeral"},
				},
			}
		} else {
			body["system"] = systemPrompt
		}
	}

	mergeOpts(body, req.Options)
	applyAnthropicTools(body, req.Options)

	headers := map[string]string{
		"x-api-key":         p.selectAPIKey(cfg),
		"anthropic-version": "2023-06-01",
	}
	if cacheEnabled {
		headers["anthropic-beta"] = "prompt-caching-2024-07-31"
	}
	return cfg.BaseURL + "/messages", body, headers
}

// ── Ollama ───────────────────────────────────────────────────────────────────

func (p *HTTPProxy) ollamaRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	body := map[string]any{
		"model":    def.ModelID,
		"messages": buildOllamaMessages(req.Fields, req.History),
		"stream":   false,
	}
	mergeOpts(body, req.Options)
	dropOrphanToolChoice(body)
	if schema, ok := req.Options["_structured_schema"]; ok {
		body["format"] = schema
	}
	headers := map[string]string{}
	if key := p.selectAPIKey(cfg); key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	return cfg.BaseURL + "/api/chat", body, headers
}

// ── Gemini ───────────────────────────────────────────────────────────────────

func (p *HTTPProxy) geminiRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	body := map[string]any{
		"contents": buildGeminiContents(req.Fields, req.History),
	}
	if sp := req.Fields["systemPrompt"]; sp != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []any{map[string]any{"text": sp}},
		}
	}
	mergeOpts(body, req.Options)
	applyGeminiTools(body, req.Options)
	if schema, ok := req.Options["_structured_schema"]; ok {
		genCfg, _ := body["generationConfig"].(map[string]any)
		if genCfg == nil {
			genCfg = map[string]any{}
		}
		genCfg["responseMimeType"] = "application/json"
		genCfg["responseSchema"] = schema
		body["generationConfig"] = genCfg
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		cfg.BaseURL, def.ModelID, p.selectAPIKey(cfg))
	return url, body, map[string]string{}
}

// ── Kling AI ─────────────────────────────────────────────────────────────────

// klingRequest builds a Kling AI video-generation request.
// The endpoint is chosen based on which fields are present:
//   - motionVideo present → /v1/videos/motion-ctrl-video (Kling v3 motion control)
//   - referenceImage or characterImage present → /v1/videos/image2video
//   - default → /v1/videos/text2video
//
// APIKey = AccessKey, APISecret = SecretKey. When both are present a short-lived
// JWT is generated on the fly (HS256, 30-minute expiry) as required by the Kling
// API. If only APIKey is provided it is used directly as a Bearer token.
func (p *HTTPProxy) klingRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	body := map[string]any{
		"model_name": def.ModelID,
		"prompt":     req.Fields["prompt"],
		"duration":   5,
	}

	var path string
	if motionVideo := req.Fields["motionVideo"]; motionVideo != "" {
		path = "/videos/motion-ctrl-video"
		body["character_image_url"] = req.Fields["characterImage"]
		body["motion_video_url"] = motionVideo
	} else if imageURL := req.Fields["referenceImage"]; imageURL != "" {
		path = "/videos/image2video"
		body["image_url"] = imageURL
	} else if imageURL := req.Fields["characterImage"]; imageURL != "" {
		path = "/videos/image2video"
		body["image_url"] = imageURL
	} else {
		path = "/videos/text2video"
	}

	mergeOpts(body, req.Options)
	stripToolOptions(body)

	var bearer string
	if cfg.APISecret != "" {
		bearer = klingJWT(cfg.APIKey, cfg.APISecret)
	} else {
		bearer = cfg.APIKey
	}
	headers := map[string]string{
		"Authorization": "Bearer " + bearer,
	}
	return cfg.BaseURL + path, body, headers
}

// klingJWT generates a short-lived HS256 JWT for the Kling API using only stdlib.
func klingJWT(accessKey, secretKey string) string {
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	now := time.Now().Unix()
	payloadJSON, _ := json.Marshal(map[string]any{"iss": accessKey, "iat": now, "exp": now + 1800})
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	msg := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(msg))
	return msg + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ── Custom (pass-through) ────────────────────────────────────────────────────

// customRequest forwards all fields and options verbatim to the configured endpoint.
func (p *HTTPProxy) customRequest(
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	body := map[string]any{}
	for k, v := range req.Fields {
		body[k] = v
	}
	mergeOpts(body, req.Options)
	dropOrphanToolChoice(body)

	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	if cfg.APISecret != "" {
		headers["X-API-Secret"] = cfg.APISecret
	}
	return cfg.BaseURL, body, headers
}

// ── Azure OpenAI ─────────────────────────────────────────────────────────────

// azureOpenAIRequest builds a request for Azure OpenAI deployments.
// The caller sets BaseURL to the full deployment endpoint:
// https://<resource>.openai.azure.com/openai/deployments/<deployment>
// APISecret holds the optional api-version (defaults to 2024-02-01).
func (p *HTTPProxy) azureOpenAIRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	apiVersion := cfg.APISecret
	if apiVersion == "" {
		apiVersion = "2024-02-01"
	}
	body := map[string]any{
		"messages": buildChatMessages(req.Fields, req.History, true),
	}
	mergeOpts(body, req.Options)
	dropOrphanToolChoice(body)
	if schema, ok := req.Options["_structured_schema"]; ok {
		name, _ := req.Options["_structured_name"].(string)
		if name == "" {
			name = "response"
		}
		strict, _ := req.Options["_structured_strict"].(bool)
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   name,
				"schema": schema,
				"strict": strict,
			},
		}
	}
	url := cfg.BaseURL + "/chat/completions?api-version=" + apiVersion
	return url, body, map[string]string{"api-key": p.selectAPIKey(cfg)}
}

// ── Cohere ────────────────────────────────────────────────────────────────────

// cohereRequest builds a Cohere Chat v2 API request.
// Cohere's v2 API accepts the same messages array shape as OpenAI.
func (p *HTTPProxy) cohereRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	msgs := buildChatMessages(req.Fields, req.History, false)
	body := map[string]any{
		"model":    def.ModelID,
		"messages": msgs,
	}
	mergeOpts(body, req.Options)
	dropOrphanToolChoice(body)
	return cfg.BaseURL + "/v2/chat", body, map[string]string{"Authorization": "Bearer " + p.selectAPIKey(cfg)}
}

// ── AWS Bedrock ───────────────────────────────────────────────────────────────

// bedrockConverseRequest builds a Bedrock Converse API request.
// The Converse API is a unified, model-agnostic interface.
// BaseURL format: https://bedrock-runtime.<region>.amazonaws.com
// The model ID is taken from def.ModelID (e.g. "anthropic.claude-3-5-sonnet-20241022-v2:0").
func (p *HTTPProxy) bedrockConverseRequest(
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (string, any, map[string]string) {
	body := map[string]any{
		"messages": buildBedrockMessages(req.Fields, req.History),
	}
	if sp := req.Fields["systemPrompt"]; sp != "" {
		body["system"] = []map[string]any{{"text": sp}}
	}
	mergeOpts(body, req.Options)
	applyBedrockTools(body, req.Options)
	url := cfg.BaseURL + "/model/" + def.ModelID + "/converse"
	// SigV4 signing is applied in bedrockSign before the request is sent.
	headers := map[string]string{
		"x-bedrock-access-key": p.selectAPIKey(cfg),
		"x-bedrock-secret-key": cfg.APISecret,
	}
	return url, body, headers
}

// ── Response parsing ─────────────────────────────────────────────────────────

func (p *HTTPProxy) parseResponse(provider domain.Provider, body []byte) (*application.ProxyResponse, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return &application.ProxyResponse{Content: string(body)}, nil
	}

	content, toolCalls := extractContentAndToolCalls(provider, raw)
	if content == "" && len(toolCalls) == 0 {
		content = string(body)
	}

	in, out, cached := extractTokenUsage(provider, raw)
	return &application.ProxyResponse{Content: content, Raw: raw, InputTokens: in, OutputTokens: out, CachedInputTokens: cached, ToolCalls: toolCalls}, nil
}

// extractTokenUsage pulls prompt/completion/cache token counts from provider responses.
func extractTokenUsage(provider domain.Provider, raw map[string]any) (inputTokens, outputTokens, cachedInputTokens int64) {
	usage, _ := raw["usage"].(map[string]any)
	switch provider {
	case domain.ProviderOpenAi, domain.ProviderMistral, domain.ProviderVLLM, domain.ProviderLocalAI,
		domain.ProviderGroq, domain.ProviderAzureOpenAI:
		if usage == nil {
			return 0, 0, 0
		}
		// {"usage":{"prompt_tokens":N,"completion_tokens":M,"prompt_tokens_details":{"cached_tokens":K}}}
		inputTokens = int64(toInt(usage["prompt_tokens"]))
		outputTokens = int64(toInt(usage["completion_tokens"]))
		if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			cachedInputTokens = int64(toInt(details["cached_tokens"]))
		}
	case domain.ProviderAnthropic:
		if usage == nil {
			return 0, 0, 0
		}
		// {"usage":{"input_tokens":N,"output_tokens":M,"cache_read_input_tokens":K,"cache_creation_input_tokens":J}}
		inputTokens = int64(toInt(usage["input_tokens"]))
		outputTokens = int64(toInt(usage["output_tokens"]))
		cachedInputTokens = int64(toInt(usage["cache_read_input_tokens"]))
	case domain.ProviderGemini:
		// {"usageMetadata":{"promptTokenCount":N,"candidatesTokenCount":M}}
		meta, _ := raw["usageMetadata"].(map[string]any)
		if meta != nil {
			inputTokens = int64(toInt(meta["promptTokenCount"]))
			outputTokens = int64(toInt(meta["candidatesTokenCount"]))
		}
	case domain.ProviderOllama:
		// {"prompt_eval_count":N,"eval_count":M}
		inputTokens = int64(toInt(raw["prompt_eval_count"]))
		outputTokens = int64(toInt(raw["eval_count"]))
	case domain.ProviderBedrock:
		if usage == nil {
			return 0, 0, 0
		}
		// {"usage":{"inputTokens":N,"outputTokens":M,"cacheReadInputTokensCount":K}}
		inputTokens = int64(toInt(usage["inputTokens"]))
		outputTokens = int64(toInt(usage["outputTokens"]))
		cachedInputTokens = int64(toInt(usage["cacheReadInputTokensCount"]))
	}
	return inputTokens, outputTokens, cachedInputTokens
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func isOpenAICompatibleChatProvider(provider domain.Provider) bool {
	switch provider {
	case domain.ProviderOpenAi, domain.ProviderMistral, domain.ProviderVLLM, domain.ProviderLocalAI,
		domain.ProviderGroq, domain.ProviderAzureOpenAI:
		return true
	default:
		return false
	}
}

// ── Streaming ────────────────────────────────────────────────────────────────

// SendStream opens a streaming connection to the upstream provider and returns
// a channel of StreamChunk values. Providers that don't natively stream
// (Gemini, Kling, Custom) fall back to a single-chunk wrapper around Send.
func (p *HTTPProxy) SendStream(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (<-chan application.StreamChunk, error) {
	switch def.Provider {
	case domain.ProviderOpenAi, domain.ProviderMistral, domain.ProviderVLLM, domain.ProviderLocalAI,
		domain.ProviderGroq, domain.ProviderAzureOpenAI:
		return p.streamChatGPT(ctx, def, cfg, req)
	case domain.ProviderAnthropic:
		return p.streamAnthropic(ctx, def, cfg, req)
	case domain.ProviderOllama:
		return p.streamOllama(ctx, def, cfg, req)
	default:
		// Gemini, Kling, Cohere, Custom: wrap sync response.
		return p.streamWrapped(ctx, def, cfg, req)
	}
}

// doStreamRequest makes an HTTP POST and returns the open response body.
// The caller is responsible for closing resp.Body.
func (p *HTTPProxy) doStreamRequest(
	ctx context.Context,
	cfg *domain.ModelConfiguration,
	targetURL string,
	body any,
	headers map[string]string,
) (*http.Response, error) {
	for k, v := range cfg.ExtraHeaders {
		headers[k] = v
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	// Use DefaultClient (no overall timeout) — the caller's context controls lifetime.
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrProxyFailed, err.Error())
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("%w: upstream returned %d: %s", domain.ErrProxyFailed, resp.StatusCode, string(b))
	}
	return resp, nil
}

// streamChatGPT handles OpenAI-compatible SSE streams (ChatGPT, Mistral).
// Each data line is: {"choices":[{"delta":{"content":"..."},...}]}
// Tool-call deltas are accumulated across chunks and emitted on the Done chunk.
func (p *HTTPProxy) streamChatGPT(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (<-chan application.StreamChunk, error) {
	targetURL, body, headers := p.chatGPTRequest(def, cfg, req)
	if m, ok := body.(map[string]any); ok {
		m["stream"] = true
		m["stream_options"] = map[string]any{"include_usage": true}
	}

	resp, err := p.doStreamRequest(ctx, cfg, targetURL, body, headers)
	if err != nil {
		return nil, err
	}

	ch := make(chan application.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var inputTokens, outputTokens, cachedInputTokens int64
		// toolCallArgs accumulates function argument strings keyed by call index.
		type tcAcc struct {
			ID   string
			Name string
			Args strings.Builder
		}
		toolCallAccum := map[int]*tcAcc{}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				done := application.StreamChunk{Done: true, InputTokens: inputTokens, OutputTokens: outputTokens, CachedInputTokens: cachedInputTokens}
				if len(toolCallAccum) > 0 {
					calls := make([]map[string]any, 0, len(toolCallAccum))
					for i := 0; i < len(toolCallAccum); i++ {
						acc, ok := toolCallAccum[i]
						if !ok {
							continue
						}
						calls = append(calls, map[string]any{
							"id":   acc.ID,
							"type": "function",
							"function": map[string]any{
								"name":      acc.Name,
								"arguments": acc.Args.String(),
							},
						})
					}
					done.ToolCalls, _ = json.Marshal(calls)
				}
				ch <- done
				return
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Type     string `json:"type"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens        int `json:"prompt_tokens"`
					CompletionTokens    int `json:"completion_tokens"`
					PromptTokensDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if chunk.Usage != nil {
				inputTokens = int64(chunk.Usage.PromptTokens)
				outputTokens = int64(chunk.Usage.CompletionTokens)
				if chunk.Usage.PromptTokensDetails != nil {
					cachedInputTokens = int64(chunk.Usage.PromptTokensDetails.CachedTokens)
				}
			}
			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta
				if delta.Content != "" {
					select {
					case ch <- application.StreamChunk{Delta: delta.Content}:
					case <-ctx.Done():
						return
					}
				}
				for _, tc := range delta.ToolCalls {
					acc, exists := toolCallAccum[tc.Index]
					if !exists {
						acc = &tcAcc{}
						toolCallAccum[tc.Index] = acc
					}
					if tc.ID != "" {
						acc.ID = tc.ID
					}
					if tc.Function.Name != "" {
						acc.Name = tc.Function.Name
					}
					acc.Args.WriteString(tc.Function.Arguments)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case ch <- application.StreamChunk{Err: err}:
			default:
			}
		}
	}()
	return ch, nil
}

// streamAnthropic handles Anthropic SSE streams.
// Relevant data lines carry {"type":"content_block_delta","delta":{"type":"text_delta","text":"..."}}
// or {"type":"message_stop"}.
func (p *HTTPProxy) streamAnthropic(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (<-chan application.StreamChunk, error) {
	targetURL, body, headers := p.anthropicRequest(def, cfg, req)
	if m, ok := body.(map[string]any); ok {
		m["stream"] = true
	}

	resp, err := p.doStreamRequest(ctx, cfg, targetURL, body, headers)
	if err != nil {
		return nil, err
	}

	ch := make(chan application.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var inputTokens, outputTokens, cachedInputTokens int64
		type toolUseAcc struct {
			ID      string
			Name    string
			Input   strings.Builder
			Started bool
		}
		toolUses := map[int]*toolUseAcc{}
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var event struct {
				Type    string `json:"type"`
				Index   int    `json:"index"`
				Message *struct {
					Usage *struct {
						InputTokens          int `json:"input_tokens"`
						CacheReadInputTokens int `json:"cache_read_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
				ContentBlock *struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Usage *struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			switch event.Type {
			case "message_start":
				if event.Message != nil && event.Message.Usage != nil {
					inputTokens = int64(event.Message.Usage.InputTokens)
					cachedInputTokens = int64(event.Message.Usage.CacheReadInputTokens)
				}
			case "message_delta":
				if event.Usage != nil {
					outputTokens = int64(event.Usage.OutputTokens)
				}
			case "content_block_start":
				if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
					toolUses[event.Index] = &toolUseAcc{
						ID:      event.ContentBlock.ID,
						Name:    event.ContentBlock.Name,
						Started: true,
					}
				}
			case "content_block_delta":
				if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
					select {
					case ch <- application.StreamChunk{Delta: event.Delta.Text}:
					case <-ctx.Done():
						return
					}
				} else if event.Delta.Type == "input_json_delta" {
					acc := toolUses[event.Index]
					if acc == nil {
						acc = &toolUseAcc{Started: true}
						toolUses[event.Index] = acc
					}
					acc.Input.WriteString(event.Delta.PartialJSON)
				}
			case "message_stop":
				done := application.StreamChunk{Done: true, InputTokens: inputTokens, OutputTokens: outputTokens, CachedInputTokens: cachedInputTokens}
				if len(toolUses) > 0 {
					calls := make([]canonicalToolCall, 0, len(toolUses))
					for i := 0; i < len(toolUses); i++ {
						acc, ok := toolUses[i]
						if !ok || acc.Name == "" {
							continue
						}
						calls = append(calls, canonicalToolCall{ID: acc.ID, Name: acc.Name, Arguments: acc.Input.String()})
					}
					done.ToolCalls = canonicalToolCallsJSON(calls)
				}
				ch <- done
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case ch <- application.StreamChunk{Err: err}:
			default:
			}
		}
	}()
	return ch, nil
}

// streamOllama handles Ollama's newline-delimited JSON stream.
// Each line: {"message":{"role":"assistant","content":"..."},"done":false|true}
func (p *HTTPProxy) streamOllama(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (<-chan application.StreamChunk, error) {
	targetURL, body, headers := p.ollamaRequest(def, cfg, req)
	if m, ok := body.(map[string]any); ok {
		m["stream"] = true // override the false we set in ollamaRequest
	}

	resp, err := p.doStreamRequest(ctx, cfg, targetURL, body, headers)
	if err != nil {
		return nil, err
	}

	ch := make(chan application.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		dec := json.NewDecoder(resp.Body)
		for {
			var line struct {
				Message struct {
					Content   string `json:"content"`
					ToolCalls any    `json:"tool_calls"`
				} `json:"message"`
				Done            bool `json:"done"`
				PromptEvalCount int  `json:"prompt_eval_count"`
				EvalCount       int  `json:"eval_count"`
			}
			if err := dec.Decode(&line); err != nil {
				if !errors.Is(err, io.EOF) {
					select {
					case ch <- application.StreamChunk{Err: err}:
					default:
					}
				}
				return
			}
			if line.Done {
				ch <- application.StreamChunk{
					Done:         true,
					InputTokens:  int64(line.PromptEvalCount),
					OutputTokens: int64(line.EvalCount),
					ToolCalls:    canonicalToolCallsJSON(canonicalToolCallsFromOpenAIish(line.Message.ToolCalls)),
				}
				return
			}
			if line.Message.Content != "" {
				select {
				case ch <- application.StreamChunk{Delta: line.Message.Content}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

// streamWrapped falls back to a synchronous Send and emits the full response
// as a single StreamChunk followed by a Done sentinel. Used for providers that
// have no native token streaming (Gemini, Kling, Custom).
func (p *HTTPProxy) streamWrapped(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	req *application.ProxyRequest,
) (<-chan application.StreamChunk, error) {
	resp, err := p.Send(ctx, def, cfg, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan application.StreamChunk, 2)
	if resp.Content != "" {
		ch <- application.StreamChunk{Delta: resp.Content}
	}
	ch <- application.StreamChunk{
		Done:              true,
		InputTokens:       resp.InputTokens,
		OutputTokens:      resp.OutputTokens,
		CachedInputTokens: resp.CachedInputTokens,
		ToolCalls:         resp.ToolCalls,
	}
	close(ch)
	return ch, nil
}

// ── Embeddings ───────────────────────────────────────────────────────────────

// SendEmbedding calls the provider's embedding endpoint and returns a dense float vector.
// Only OpenAI-compatible providers (OpenAI, Mistral) and Ollama are supported.
// All other providers return ErrEmbeddingNotSupported, which causes semantic
// features to degrade gracefully.
func (p *HTTPProxy) SendEmbedding(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	text string,
) ([]float32, error) {
	switch def.Provider {
	case domain.ProviderOpenAi, domain.ProviderMistral, domain.ProviderVLLM, domain.ProviderLocalAI:
		return p.openAIEmbedding(ctx, def, cfg, text)
	case domain.ProviderOllama:
		return p.ollamaEmbedding(ctx, def, cfg, text)
	default:
		return nil, domain.ErrEmbeddingNotSupported
	}
}

func (p *HTTPProxy) openAIEmbedding(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	text string,
) ([]float32, error) {
	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	body, _ := json.Marshal(map[string]any{
		"model": def.ModelID,
		"input": text,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.selectAPIKey(cfg))
	for k, v := range cfg.ExtraHeaders {
		if !strings.EqualFold(k, "authorization") {
			httpReq.Header.Set(k, v)
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrProxyFailed, err.Error())
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: upstream returned %d: %s", domain.ErrProxyFailed, resp.StatusCode, string(respBytes))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parse embedding response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response from %s", def.ModelID)
	}
	return result.Data[0].Embedding, nil
}

func (p *HTTPProxy) ollamaEmbedding(
	ctx context.Context,
	def *domain.ModelDefinition,
	cfg *domain.ModelConfiguration,
	text string,
) ([]float32, error) {
	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	body, _ := json.Marshal(map[string]any{
		"model":  def.ModelID,
		"prompt": text,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrProxyFailed, err.Error())
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: upstream returned %d: %s", domain.ErrProxyFailed, resp.StatusCode, string(respBytes))
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parse embedding response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response from %s", def.ModelID)
	}
	return result.Embedding, nil
}

// parseStringOrJSONArray returns a slice containing either the parsed JSON
// string array or, if parsing fails, a single-element slice with the raw value.
func parseStringOrJSONArray(s string) []string {
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr
	}
	return []string{s}
}

// extractContentAndToolCalls extracts text content and normalises provider
// tool-use responses into the OpenAI-compatible tool_calls shape used by the
// router MCP loop.
func extractContentAndToolCalls(provider domain.Provider, raw map[string]any) (content string, toolCalls json.RawMessage) {
	if isOpenAICompatibleChatProvider(provider) {
		return extractOpenAICompatibleContentAndToolCalls(raw)
	}
	switch provider {
	case domain.ProviderAnthropic:
		// { "content": [{ "type": "text", "text": "..." }, { "type": "tool_use", ... }] }
		if contents, ok := raw["content"].([]any); ok {
			calls := make([]canonicalToolCall, 0)
			for _, c := range contents {
				block, ok := c.(map[string]any)
				if !ok {
					continue
				}
				switch block["type"] {
				case "text":
					s, _ := block["text"].(string)
					content += s
				case "tool_use":
					id, _ := block["id"].(string)
					name, _ := block["name"].(string)
					calls = append(calls, canonicalToolCall{ID: id, Name: name, Arguments: block["input"]})
				}
			}
			toolCalls = canonicalToolCallsJSON(calls)
		}

	case domain.ProviderOllama:
		// { "message": { "content": "...", "tool_calls": [...] } } or { "response": "..." }
		if msg, ok := raw["message"].(map[string]any); ok {
			content, _ = msg["content"].(string)
			if calls := canonicalToolCallsFromOpenAIish(msg["tool_calls"]); len(calls) > 0 {
				toolCalls = canonicalToolCallsJSON(calls)
			}
		} else {
			content, _ = raw["response"].(string)
		}

	case domain.ProviderGemini:
		// { "candidates": [{ "content": { "parts": [{ "text": "..." }, {"functionCall": ...}] } }] }
		if candidates, ok := raw["candidates"].([]any); ok && len(candidates) > 0 {
			if candidate, ok := candidates[0].(map[string]any); ok {
				if cnt, ok := candidate["content"].(map[string]any); ok {
					if parts, ok := cnt["parts"].([]any); ok && len(parts) > 0 {
						calls := make([]canonicalToolCall, 0)
						for i, rawPart := range parts {
							part, ok := rawPart.(map[string]any)
							if !ok {
								continue
							}
							if text, _ := part["text"].(string); text != "" {
								content += text
							}
							if fn, _ := part["functionCall"].(map[string]any); fn != nil {
								name, _ := fn["name"].(string)
								if name != "" {
									calls = append(calls, canonicalToolCall{
										ID:        fmt.Sprintf("call_%d", i),
										Name:      name,
										Arguments: fn["args"],
									})
								}
							}
						}
						toolCalls = canonicalToolCallsJSON(calls)
					}
				}
			}
		}

	case domain.ProviderCohere:
		// { "message": { "content": [{"type":"text","text":"..."}], "tool_calls": [...] } }
		if msg, ok := raw["message"].(map[string]any); ok {
			content = contentText(msg["content"])
			if calls := canonicalToolCallsFromOpenAIish(msg["tool_calls"]); len(calls) > 0 {
				toolCalls = canonicalToolCallsJSON(calls)
			}
		}

	case domain.ProviderBedrock:
		// { "output": { "message": { "content": [{"text":...}, {"toolUse":...}] } } }
		if out, ok := raw["output"].(map[string]any); ok {
			if msg, ok := out["message"].(map[string]any); ok {
				if parts, ok := msg["content"].([]any); ok {
					calls := make([]canonicalToolCall, 0)
					for _, rawPart := range parts {
						part, ok := rawPart.(map[string]any)
						if !ok {
							continue
						}
						if text, _ := part["text"].(string); text != "" {
							content += text
						}
						if toolUse, _ := part["toolUse"].(map[string]any); toolUse != nil {
							id, _ := toolUse["toolUseId"].(string)
							name, _ := toolUse["name"].(string)
							calls = append(calls, canonicalToolCall{ID: id, Name: name, Arguments: toolUse["input"]})
						}
					}
					toolCalls = canonicalToolCallsJSON(calls)
				}
			}
		}

	case domain.ProviderKling:
		// Task-creation response: { "data": { "task_id": "..." } }
		// Polling/completion response: { "data": { "task_result": { "videos": [{ "url": "..." }] } } }
		if data, ok := raw["data"].(map[string]any); ok {
			if result, ok := data["task_result"].(map[string]any); ok {
				if videos, ok := result["videos"].([]any); ok && len(videos) > 0 {
					if v, ok := videos[0].(map[string]any); ok {
						content, _ = v["url"].(string)
					}
				}
			}
			if content == "" {
				content, _ = data["task_id"].(string)
			}
		}

	default: // CUSTOM – try common field names
		for _, key := range []string{"content", "response", "result", "output", "text"} {
			if s, ok := raw[key].(string); ok && s != "" {
				content = s
				break
			}
		}
	}
	return content, toolCalls
}

func extractOpenAICompatibleContentAndToolCalls(raw map[string]any) (content string, toolCalls json.RawMessage) {
	if choices, ok := raw["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				content = contentText(msg["content"])
				if calls := canonicalToolCallsFromOpenAIish(msg["tool_calls"]); len(calls) > 0 {
					toolCalls = canonicalToolCallsJSON(calls)
				}
			}
		}
	}
	return content, toolCalls
}

func canonicalToolCallsFromOpenAIish(value any) []canonicalToolCall {
	rawCalls := mapSlice(value)
	calls := make([]canonicalToolCall, 0, len(rawCalls))
	for i, raw := range rawCalls {
		fn, _ := raw["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		id, _ := raw["id"].(string)
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		calls = append(calls, canonicalToolCall{
			ID:        id,
			Name:      name,
			Arguments: toolArgumentsValue(fn["arguments"]),
		})
	}
	return calls
}

func contentText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, _ := part["text"].(string); text != "" {
				b.WriteString(text)
			}
		}
		return b.String()
	default:
		return ""
	}
}
