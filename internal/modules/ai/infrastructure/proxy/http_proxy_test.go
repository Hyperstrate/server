package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/domain"
)

// ── buildChatMessages ─────────────────────────────────────────────────────────

func TestBuildChatMessages_promptOnly(t *testing.T) {
	msgs := buildChatMessages(
		map[string]string{"prompt": "hello"},
		nil,
		false,
	)
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	assertMessage(t, msgs[0], "user", "hello")
}

func TestBuildChatMessages_withSystemPrompt(t *testing.T) {
	msgs := buildChatMessages(
		map[string]string{"prompt": "hi", "systemPrompt": "be helpful"},
		nil,
		false,
	)
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	assertMessage(t, msgs[0], "system", "be helpful")
	assertMessage(t, msgs[1], "user", "hi")
}

func TestBuildChatMessages_withHistory(t *testing.T) {
	history := []application.HistoryMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "response"},
	}
	msgs := buildChatMessages(map[string]string{"prompt": "second"}, history, false)
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	assertMessage(t, msgs[0], "user", "first")
	assertMessage(t, msgs[1], "assistant", "response")
	assertMessage(t, msgs[2], "user", "second")
}

func TestBuildChatMessages_withToolHistory(t *testing.T) {
	history := []application.HistoryMessage{
		{
			Role:      "assistant",
			ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_repo_status","arguments":"{}"}}]`),
		},
		{Role: "tool", ToolCallID: "call_1", Content: "status: clean"},
	}

	msgs := buildChatMessages(map[string]string{"prompt": ""}, history, false)
	if len(msgs) != 2 {
		t.Fatalf("want only tool history messages, got %d", len(msgs))
	}

	assistantMsg, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("want assistant message map, got %T", msgs[0])
	}
	if assistantMsg["role"] != "assistant" {
		t.Fatalf("want assistant role, got %v", assistantMsg["role"])
	}
	if assistantMsg["content"] != nil {
		t.Fatalf("want null assistant content for tool_calls, got %v", assistantMsg["content"])
	}
	if calls, ok := assistantMsg["tool_calls"].([]any); !ok || len(calls) != 1 {
		t.Fatalf("want one assistant tool call, got %v", assistantMsg["tool_calls"])
	}

	toolMsg, ok := msgs[1].(map[string]any)
	if !ok {
		t.Fatalf("want tool message map, got %T", msgs[1])
	}
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" || toolMsg["content"] != "status: clean" {
		t.Fatalf("unexpected tool message: %+v", toolMsg)
	}
}

func TestBuildChatMessages_withSystemPromptAndHistory(t *testing.T) {
	msgs := buildChatMessages(
		map[string]string{"prompt": "p", "systemPrompt": "sys"},
		[]application.HistoryMessage{{Role: "user", Content: "prev"}},
		false,
	)
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	assertMessage(t, msgs[0], "system", "sys")
	assertMessage(t, msgs[1], "user", "prev")
	assertMessage(t, msgs[2], "user", "p")
}

func TestBuildChatMessages_withImageEnabled(t *testing.T) {
	msgs := buildChatMessages(
		map[string]string{"prompt": "what is this?", "image": "https://example.com/img.jpg"},
		nil,
		true,
	)
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	m, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatal("want map[string]any message")
	}
	if m["role"] != "user" {
		t.Errorf("want role 'user', got %v", m["role"])
	}
	// content should be an array (image + text parts)
	parts, ok := m["content"].([]any)
	if !ok {
		t.Fatalf("want content as []any, got %T", m["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("want 2 content parts (image + text), got %d", len(parts))
	}
	imgPart, _ := parts[0].(map[string]any)
	if imgPart["type"] != "image_url" {
		t.Errorf("want first part type 'image_url', got %v", imgPart["type"])
	}
	textPart, _ := parts[1].(map[string]any)
	if textPart["type"] != "text" {
		t.Errorf("want second part type 'text', got %v", textPart["type"])
	}
}

func TestBuildChatMessages_imageDisabled_imageFieldIgnored(t *testing.T) {
	// withImage=false: image field in fields must be ignored.
	msgs := buildChatMessages(
		map[string]string{"prompt": "hi", "image": "https://example.com/img.jpg"},
		nil,
		false,
	)
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	assertMessage(t, msgs[0], "user", "hi")
}

func TestBuildChatMessages_imageAsJSONArray(t *testing.T) {
	msgs := buildChatMessages(
		map[string]string{
			"prompt": "compare",
			"image":  `["https://a.com/1.jpg","https://b.com/2.jpg"]`,
		},
		nil,
		true,
	)
	m, _ := msgs[0].(map[string]any)
	parts, _ := m["content"].([]any)
	// 2 image parts + 1 text part
	if len(parts) != 3 {
		t.Errorf("want 3 content parts (2 images + text), got %d", len(parts))
	}
}

// ── mergeOpts ────────────────────────────────────────────────────────────────

func TestMergeOpts_keepsProviderOptions(t *testing.T) {
	body := map[string]any{"model": "gpt-test"}
	tools := []any{map[string]any{"type": "function"}}

	mergeOpts(body, map[string]any{
		"temperature": 0.2,
		"top_p":       0.9,
		"tools":       tools,
		"tool_choice": "auto",
	})

	if body["temperature"] != 0.2 {
		t.Errorf("want temperature forwarded, got %v", body["temperature"])
	}
	if body["top_p"] != 0.9 {
		t.Errorf("want top_p forwarded, got %v", body["top_p"])
	}
	if gotTools, ok := body["tools"].([]any); !ok || !reflect.DeepEqual(gotTools, tools) {
		t.Errorf("want tools forwarded, got %v", body["tools"])
	}
	if body["tool_choice"] != "auto" {
		t.Errorf("want tool_choice forwarded, got %v", body["tool_choice"])
	}
}

func TestMergeOpts_skipsInternalAgentMetadata(t *testing.T) {
	body := map[string]any{}

	mergeOpts(body, map[string]any{
		"_structured_schema":      map[string]any{"type": "object"},
		"agent":                   "codex",
		"agent_role":              "worker",
		"agent_session_id":        "local-session-1",
		"agent_user_id":           "dev-1",
		"conversation_id":         "conversation-1",
		"parent_agent":            "claude_code",
		"parent_agent_session_id": "parent-session-1",
		"parent_agent_user_id":    "parent-dev-1",
		"parent_session_id":       "parent-session-1",
		"parent_subject_user_id":  "parent-subject-1",
		"parent_user_id":          "parent-user-1",
		"session_id":              "session-1",
		"subject_user_id":         "subject-1",
		"turn_index":              3,
		"user_id":                 "user-1",
		"temperature":             0.3,
	})

	if body["temperature"] != 0.3 {
		t.Errorf("want provider option forwarded, got %v", body["temperature"])
	}
	for _, key := range []string{
		"_structured_schema",
		"agent",
		"agent_role",
		"agent_session_id",
		"agent_user_id",
		"conversation_id",
		"parent_agent",
		"parent_agent_session_id",
		"parent_agent_user_id",
		"parent_session_id",
		"parent_subject_user_id",
		"parent_user_id",
		"session_id",
		"subject_user_id",
		"turn_index",
		"user_id",
	} {
		if _, ok := body[key]; ok {
			t.Errorf("want %q stripped from upstream body", key)
		}
	}
}

// ── provider tool translation ────────────────────────────────────────────────

func TestAnthropicRequest_translatesToolsAndToolHistory(t *testing.T) {
	p := &HTTPProxy{}
	_, bodyAny, _ := p.anthropicRequest(
		&domain.ModelDefinition{ModelID: "claude-test"},
		&domain.ModelConfiguration{BaseURL: "https://anthropic.test", APIKey: "key"},
		&application.ProxyRequest{
			Fields: map[string]string{},
			History: []application.HistoryMessage{
				{Role: "user", Content: "inspect repo"},
				{Role: "assistant", ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_repo_status","arguments":"{\"path\":\"/tmp/demo\"}"}}]`)},
				{Role: "tool", ToolCallID: "call_1", ToolName: "get_repo_status", Content: "status: clean"},
			},
			Options: sampleToolOptions(),
		},
	)
	body := bodyAny.(map[string]any)

	tools := body["tools"].([]map[string]any)
	if tools[0]["name"] != "get_repo_status" || tools[0]["input_schema"] == nil {
		t.Fatalf("unexpected anthropic tools: %+v", tools)
	}
	messages := body["messages"].([]any)
	assistant := messages[1].(map[string]any)
	assistantParts := assistant["content"].([]any)
	toolUse := assistantParts[0].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "call_1" || toolUse["name"] != "get_repo_status" {
		t.Fatalf("unexpected anthropic tool_use: %+v", toolUse)
	}
	toolResultMsg := messages[2].(map[string]any)
	toolResult := toolResultMsg["content"].([]any)[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call_1" {
		t.Fatalf("unexpected anthropic tool_result: %+v", toolResult)
	}
}

func TestGeminiRequest_translatesToolsAndToolHistory(t *testing.T) {
	p := &HTTPProxy{}
	_, bodyAny, _ := p.geminiRequest(
		&domain.ModelDefinition{ModelID: "gemini-test"},
		&domain.ModelConfiguration{BaseURL: "https://gemini.test", APIKey: "key"},
		&application.ProxyRequest{
			Fields: map[string]string{},
			History: []application.HistoryMessage{
				{Role: "user", Content: "inspect repo"},
				{Role: "assistant", ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_repo_status","arguments":"{\"path\":\"/tmp/demo\"}"}}]`)},
				{Role: "tool", ToolCallID: "call_1", ToolName: "get_repo_status", Content: "status: clean"},
			},
			Options: sampleToolOptions(),
		},
	)
	body := bodyAny.(map[string]any)

	tools := body["tools"].([]map[string]any)
	declarations := tools[0]["functionDeclarations"].([]map[string]any)
	if declarations[0]["name"] != "get_repo_status" {
		t.Fatalf("unexpected gemini declarations: %+v", declarations)
	}
	contents := body["contents"].([]any)
	modelTurn := contents[1].(map[string]any)
	functionCall := modelTurn["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if functionCall["name"] != "get_repo_status" {
		t.Fatalf("unexpected gemini functionCall: %+v", functionCall)
	}
	toolTurn := contents[2].(map[string]any)
	functionResponse := toolTurn["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if functionResponse["name"] != "get_repo_status" {
		t.Fatalf("unexpected gemini functionResponse: %+v", functionResponse)
	}
}

func TestBedrockRequest_translatesToolsAndToolHistory(t *testing.T) {
	p := &HTTPProxy{}
	_, bodyAny, _ := p.bedrockConverseRequest(
		&domain.ModelDefinition{ModelID: "anthropic.claude-test"},
		&domain.ModelConfiguration{BaseURL: "https://bedrock.test", APIKey: "key", APISecret: "secret"},
		&application.ProxyRequest{
			Fields: map[string]string{},
			History: []application.HistoryMessage{
				{Role: "user", Content: "inspect repo"},
				{Role: "assistant", ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_repo_status","arguments":"{\"path\":\"/tmp/demo\"}"}}]`)},
				{Role: "tool", ToolCallID: "call_1", ToolName: "get_repo_status", Content: "status: clean"},
			},
			Options: sampleToolOptions(),
		},
	)
	body := bodyAny.(map[string]any)

	toolConfig := body["toolConfig"].(map[string]any)
	tools := toolConfig["tools"].([]map[string]any)
	spec := tools[0]["toolSpec"].(map[string]any)
	if spec["name"] != "get_repo_status" || spec["inputSchema"] == nil {
		t.Fatalf("unexpected bedrock toolSpec: %+v", spec)
	}
	messages := body["messages"].([]map[string]any)
	toolUse := messages[1]["content"].([]any)[0].(map[string]any)["toolUse"].(map[string]any)
	if toolUse["toolUseId"] != "call_1" || toolUse["name"] != "get_repo_status" {
		t.Fatalf("unexpected bedrock toolUse: %+v", toolUse)
	}
	toolResult := messages[2]["content"].([]any)[0].(map[string]any)["toolResult"].(map[string]any)
	if toolResult["toolUseId"] != "call_1" {
		t.Fatalf("unexpected bedrock toolResult: %+v", toolResult)
	}
}

func TestExtractContentAndToolCalls_normalizesProviderToolCalls(t *testing.T) {
	tests := []struct {
		name     string
		provider domain.Provider
		raw      map[string]any
		wantName string
		wantID   string
	}{
		{
			name:     "openai compatible groq",
			provider: domain.ProviderGroq,
			raw: map[string]any{"choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{
				"id": "call_1", "type": "function", "function": map[string]any{"name": "get_repo_status", "arguments": `{"path":"/tmp/demo"}`},
			}}}}}},
			wantName: "get_repo_status",
			wantID:   "call_1",
		},
		{
			name:     "anthropic",
			provider: domain.ProviderAnthropic,
			raw: map[string]any{"content": []any{map[string]any{
				"type": "tool_use", "id": "toolu_1", "name": "get_repo_status", "input": map[string]any{"path": "/tmp/demo"},
			}}},
			wantName: "get_repo_status",
			wantID:   "toolu_1",
		},
		{
			name:     "gemini",
			provider: domain.ProviderGemini,
			raw: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{
				"functionCall": map[string]any{"name": "get_repo_status", "args": map[string]any{"path": "/tmp/demo"}},
			}}}}}},
			wantName: "get_repo_status",
			wantID:   "call_0",
		},
		{
			name:     "ollama",
			provider: domain.ProviderOllama,
			raw: map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{
				"type": "function", "function": map[string]any{"name": "get_repo_status", "arguments": map[string]any{"path": "/tmp/demo"}},
			}}}},
			wantName: "get_repo_status",
			wantID:   "call_0",
		},
		{
			name:     "cohere",
			provider: domain.ProviderCohere,
			raw: map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{
				"id": "co_1", "type": "function", "function": map[string]any{"name": "get_repo_status", "arguments": `{"path":"/tmp/demo"}`},
			}}}},
			wantName: "get_repo_status",
			wantID:   "co_1",
		},
		{
			name:     "bedrock",
			provider: domain.ProviderBedrock,
			raw: map[string]any{"output": map[string]any{"message": map[string]any{"content": []any{map[string]any{
				"toolUse": map[string]any{"toolUseId": "bed_1", "name": "get_repo_status", "input": map[string]any{"path": "/tmp/demo"}},
			}}}}},
			wantName: "get_repo_status",
			wantID:   "bed_1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, rawCalls := extractContentAndToolCalls(tt.provider, tt.raw)
			calls := parseCanonicalToolCalls(rawCalls)
			if len(calls) != 1 {
				t.Fatalf("calls len = %d, want 1, raw=%s", len(calls), string(rawCalls))
			}
			if calls[0].Name != tt.wantName || calls[0].ID != tt.wantID {
				t.Fatalf("unexpected call: %+v", calls[0])
			}
		})
	}
}

// ── klingJWT ─────────────────────────────────────────────────────────────────

func TestKlingJWT_producesThreePartToken(t *testing.T) {
	token := klingJWT("access_key", "secret_key")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("want 3 JWT parts, got %d: %s", len(parts), token)
	}
}

func TestKlingJWT_headerIsHS256(t *testing.T) {
	token := klingJWT("acc", "sec")
	parts := strings.Split(token, ".")

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("cannot base64-decode header: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("cannot JSON-decode header: %v", err)
	}
	if header["alg"] != "HS256" {
		t.Errorf("want alg 'HS256', got %q", header["alg"])
	}
	if header["typ"] != "JWT" {
		t.Errorf("want typ 'JWT', got %q", header["typ"])
	}
}

func TestKlingJWT_payloadContainsIssuer(t *testing.T) {
	token := klingJWT("my_access_key", "my_secret")
	parts := strings.Split(token, ".")

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("cannot base64-decode payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("cannot JSON-decode payload: %v", err)
	}
	if payload["iss"] != "my_access_key" {
		t.Errorf("want iss 'my_access_key', got %v", payload["iss"])
	}
	if _, ok := payload["iat"]; !ok {
		t.Error("want iat in payload")
	}
	if _, ok := payload["exp"]; !ok {
		t.Error("want exp in payload")
	}
}

func TestKlingJWT_signatureIsValidHMACSHA256(t *testing.T) {
	accessKey, secretKey := "acc", "sec"
	token := klingJWT(accessKey, secretKey)
	parts := strings.Split(token, ".")

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("cannot base64-decode signature: %v", err)
	}

	msg := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(msg))
	expected := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expected) {
		t.Error("signature does not match expected HMAC-SHA256")
	}
}

func TestExtractTokenUsage_anthropicIncludesCacheWriteTokens(t *testing.T) {
	raw := map[string]any{
		"usage": map[string]any{
			"input_tokens":                float64(50),
			"output_tokens":               float64(20),
			"cache_read_input_tokens":     float64(1_800),
			"cache_creation_input_tokens": float64(248),
		},
	}

	input, output, cached, cacheWrite, cacheWrite1h := extractTokenUsage(domain.ProviderAnthropic, raw)
	if input != 2_098 || output != 20 || cached != 1_800 || cacheWrite != 248 || cacheWrite1h != 0 {
		t.Fatalf("usage = input:%d output:%d cached:%d cacheWrite:%d cacheWrite1h:%d", input, output, cached, cacheWrite, cacheWrite1h)
	}
}

func TestExtractTokenUsage_anthropicSplitsCacheCreationTTL(t *testing.T) {
	raw := map[string]any{
		"usage": map[string]any{
			"input_tokens":                float64(8),
			"output_tokens":               float64(0),
			"cache_read_input_tokens":     float64(0),
			"cache_creation_input_tokens": float64(5_120),
			"cache_creation": map[string]any{
				"ephemeral_5m_input_tokens": float64(4_096),
				"ephemeral_1h_input_tokens": float64(1_024),
			},
		},
	}

	input, output, cached, cacheWrite, cacheWrite1h := extractTokenUsage(domain.ProviderAnthropic, raw)
	if input != 5_128 || output != 0 || cached != 0 || cacheWrite != 4_096 || cacheWrite1h != 1_024 {
		t.Fatalf("usage = input:%d output:%d cached:%d cacheWrite:%d cacheWrite1h:%d", input, output, cached, cacheWrite, cacheWrite1h)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func assertMessage(t *testing.T, msg any, wantRole, wantContent string) {
	t.Helper()
	m, ok := msg.(map[string]any)
	if !ok {
		t.Fatalf("want map[string]any, got %T", msg)
	}
	if m["role"] != wantRole {
		t.Errorf("want role %q, got %v", wantRole, m["role"])
	}
	if m["content"] != wantContent {
		t.Errorf("want content %q, got %v", wantContent, m["content"])
	}
}

func sampleToolOptions() map[string]any {
	return map[string]any{
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_repo_status",
				"description": "Inspect a repository status.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
				},
			},
		}},
		"tool_choice": "auto",
	}
}
