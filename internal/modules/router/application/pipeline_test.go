package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/router/domain"
)

// ── Stubs ─────────────────────────────────────────────────────────────────────

type stubInferencer struct {
	streamFn func(ctx context.Context, modelID string, fields map[string]string, opts map[string]any) (<-chan StreamChunk, error)
	inferFn  func(ctx context.Context, modelID string, fields map[string]string, opts map[string]any) (*ModelInferResult, error)
}

func (s *stubInferencer) InferModel(ctx context.Context, modelID string, fields map[string]string, opts map[string]any) (*ModelInferResult, error) {
	if s.inferFn != nil {
		return s.inferFn(ctx, modelID, fields, opts)
	}
	return &ModelInferResult{Content: "ok"}, nil
}

func (s *stubInferencer) InferModelStream(ctx context.Context, modelID string, fields map[string]string, opts map[string]any) (<-chan StreamChunk, error) {
	if s.streamFn != nil {
		return s.streamFn(ctx, modelID, fields, opts)
	}
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func singleTargetRouter() (*domain.Router, []domain.RouterTarget) {
	router := &domain.Router{ID: "rtr_1", Strategy: domain.RoutingStrategyFailover}
	targets := []domain.RouterTarget{
		{ID: "tgt_1", ModelID: "mdl_1", IsEnabled: true, Weight: 1},
	}
	return router, targets
}

func TestMarkCachedResultEstimatesCachedInputTokens(t *testing.T) {
	hit := markCachedResult(&RouteInferResult{Content: "cached"}, map[string]string{
		"systemPrompt": "You are concise.",
		"prompt":       strings.Repeat("x", 400),
	}, "exact")

	if !hit.CacheHit || hit.CacheHitType != "exact" {
		t.Fatalf("expected exact cache hit metadata, got hit=%v type=%q", hit.CacheHit, hit.CacheHitType)
	}
	if hit.CachedInputTokens <= 0 {
		t.Fatalf("expected estimated cached input tokens, got %d", hit.CachedInputTokens)
	}
}

func TestMCPToolCallNameArgs_acceptsStringAndObjectArguments(t *testing.T) {
	name, args, err := mcpToolCallNameArgs(map[string]any{
		"function": map[string]any{
			"name":      "lookup",
			"arguments": `{"city":"Paris"}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "lookup" || args["city"] != "Paris" {
		t.Fatalf("unexpected parsed call: name=%q args=%v", name, args)
	}

	name, args, err = mcpToolCallNameArgs(map[string]any{
		"function": map[string]any{
			"name":      "lookup",
			"arguments": map[string]any{"city": "Bucharest"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "lookup" || args["city"] != "Bucharest" {
		t.Fatalf("unexpected parsed call: name=%q args=%v", name, args)
	}
}

func TestMCPToolCallDetail_tracksPayloadAndResponse(t *testing.T) {
	detail := mcpToolCallDetail("lookup", "call_1", map[string]any{"city": "Paris"}, "sunny", nil)
	for _, want := range []string{
		"lookup (call_1)",
		`payload={"city":"Paris"}`,
		"response=sunny",
		"response_chars=5",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}
}

func TestRun_prefetchesMCPToolsBeforeInference(t *testing.T) {
	var listCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode MCP request: %v", err)
		}
		if req.Method != "tools/list" {
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
		listCalls.Add(1)
		writeMCPToolsList(t, w)
	}))
	defer server.Close()

	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router, targets := singleTargetRouter()
	features := []domain.RouterFeature{mcpFeature(server.URL)}
	var sawTools bool

	inferencer := &stubInferencer{
		inferFn: func(_ context.Context, _ string, _ map[string]string, opts map[string]any) (*ModelInferResult, error) {
			sawTools = hasToolNamed(opts["tools"], "get_repo_status")
			return &ModelInferResult{Content: "ok"}, nil
		},
	}

	_, steps, err := p.run(context.Background(), router, targets, features, nil, map[string]string{"prompt": "inspect repo"}, nil, inferencer, false)
	if err != nil {
		t.Fatal(err)
	}
	if !sawTools {
		t.Fatal("expected MCP tools to be available on the first inference call")
	}
	if listCalls.Load() != 1 {
		t.Fatalf("tools/list calls = %d, want 1", listCalls.Load())
	}
	if !hasPipelineStep(steps, "mcp_tools_list", "success") {
		t.Fatalf("expected successful MCP tool discovery step, got %+v", steps)
	}
}

func TestRun_executesMCPToolCallWithPrefetchedTools(t *testing.T) {
	var listCalls atomic.Int64
	var toolCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode MCP request: %v", err)
		}
		switch req.Method {
		case "tools/list":
			listCalls.Add(1)
			writeMCPToolsList(t, w)
		case "tools/call":
			toolCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"branch: main\nstatus: clean"}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer server.Close()

	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router, targets := singleTargetRouter()
	features := []domain.RouterFeature{mcpFeature(server.URL)}
	var inferenceCalls int
	var sawToolHistory bool

	inferencer := &stubInferencer{
		inferFn: func(_ context.Context, _ string, fields map[string]string, opts map[string]any) (*ModelInferResult, error) {
			inferenceCalls++
			if inferenceCalls == 1 {
				if !hasToolNamed(opts["tools"], "get_repo_status") {
					t.Fatal("first inference did not receive MCP tools")
				}
				return &ModelInferResult{
					ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_repo_status","arguments":"{\"path\":\"/tmp/demo\"}"}}]`),
				}, nil
			}

			var history []struct {
				Role       string          `json:"role"`
				ToolCallID string          `json:"tool_call_id"`
				ToolCalls  json.RawMessage `json:"tool_calls"`
			}
			if err := json.Unmarshal([]byte(fields["_history"]), &history); err != nil {
				t.Fatalf("decode follow-up history: %v", err)
			}
			sawAssistantToolCalls := false
			sawToolResult := false
			for _, msg := range history {
				if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
					sawAssistantToolCalls = true
				}
				if msg.Role == "tool" && msg.ToolCallID == "call_1" {
					sawToolResult = true
				}
			}
			sawToolHistory = sawAssistantToolCalls && sawToolResult
			return &ModelInferResult{Content: "Repo is clean."}, nil
		},
	}

	result, _, err := p.run(context.Background(), router, targets, features, nil, map[string]string{"prompt": "inspect repo"}, nil, inferencer, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Repo is clean." {
		t.Fatalf("content = %q, want final tool-informed response", result.Content)
	}
	if inferenceCalls != 2 {
		t.Fatalf("inference calls = %d, want 2", inferenceCalls)
	}
	if listCalls.Load() != 1 {
		t.Fatalf("tools/list calls = %d, want 1", listCalls.Load())
	}
	if toolCalls.Load() != 1 {
		t.Fatalf("tools/call calls = %d, want 1", toolCalls.Load())
	}
	if !sawToolHistory {
		t.Fatal("follow-up inference did not receive assistant tool_calls plus tool result history")
	}
	if len(result.ToolCallCaptures) != 1 || !strings.Contains(result.ToolCallCaptures[0].ResponsePayload, "status: clean") {
		t.Fatalf("unexpected tool captures: %+v", result.ToolCallCaptures)
	}
}

func TestRun_relaxesForcedToolChoiceAfterMCPToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode MCP request: %v", err)
		}
		switch req.Method {
		case "tools/list":
			writeMCPToolsList(t, w)
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"status: clean"}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer server.Close()

	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router, targets := singleTargetRouter()
	features := []domain.RouterFeature{mcpFeature(server.URL)}
	inferenceCalls := 0

	inferencer := &stubInferencer{
		inferFn: func(_ context.Context, _ string, _ map[string]string, opts map[string]any) (*ModelInferResult, error) {
			inferenceCalls++
			if inferenceCalls == 1 {
				return &ModelInferResult{
					ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_repo_status","arguments":"{}"}}]`),
				}, nil
			}
			if opts["tool_choice"] != "auto" {
				t.Fatalf("follow-up tool_choice = %#v, want auto", opts["tool_choice"])
			}
			return &ModelInferResult{Content: "Repo status is clean."}, nil
		},
	}

	forcedChoice := map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "get_repo_status"},
	}
	result, _, err := p.run(
		context.Background(),
		router,
		targets,
		features,
		nil,
		map[string]string{"prompt": "inspect repo"},
		map[string]any{"tool_choice": forcedChoice},
		inferencer,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Repo status is clean." {
		t.Fatalf("content = %q, want final answer", result.Content)
	}
	if len(result.ToolCalls) > 0 {
		t.Fatalf("tool calls leaked to final response: %s", string(result.ToolCalls))
	}
}

func mcpFeature(url string) domain.RouterFeature {
	return domain.RouterFeature{
		FeatureType: domain.FeatureMCPTools,
		IsEnabled:   true,
		Config: map[string]any{
			"servers": []any{map[string]any{"url": url}},
		},
	}
}

func writeMCPToolsList(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"tools": []map[string]any{
				{
					"name":        "get_repo_status",
					"description": "Return fake repository status.",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"path": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode tools/list response: %v", err)
	}
}

func hasToolNamed(value any, name string) bool {
	for _, tool := range toolOptionsFromAny(value) {
		fn, _ := tool["function"].(map[string]any)
		if fnName, _ := fn["name"].(string); fnName == name {
			return true
		}
	}
	return false
}

func hasPipelineStep(steps []PipelineStep, kind, outcome string) bool {
	for _, step := range steps {
		if step.Kind == kind && step.Outcome == outcome {
			return true
		}
	}
	return false
}

// drainWithTimeout drains ch until it closes or the timeout fires.
// Returns true if the channel closed before the timeout.
func drainWithTimeout(ch <-chan StreamChunk, d time.Duration) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
		case <-deadline.C:
			return false
		}
	}
}

// ── runStream tests ───────────────────────────────────────────────────────────

func TestRunStream_forwardsChunksToOutputChannel(t *testing.T) {
	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router, targets := singleTargetRouter()

	upstream := make(chan StreamChunk, 3)
	upstream <- StreamChunk{Delta: "foo"}
	upstream <- StreamChunk{Delta: "bar"}
	upstream <- StreamChunk{Done: true}
	close(upstream)

	inferencer := &stubInferencer{
		streamFn: func(_ context.Context, _ string, _ map[string]string, _ map[string]any) (<-chan StreamChunk, error) {
			return upstream, nil
		},
	}

	out, _, err := p.runStream(context.Background(), router, targets, nil, nil,
		map[string]string{"prompt": "hi"}, nil, inferencer, false)
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	for chunk := range out {
		if chunk.Delta != "" {
			deltas = append(deltas, chunk.Delta)
		}
	}
	if len(deltas) != 2 || deltas[0] != "foo" || deltas[1] != "bar" {
		t.Errorf("want [foo bar], got %v", deltas)
	}
}

func TestRunStream_outputChannelClosesOnContextCancel(t *testing.T) {
	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router, targets := singleTargetRouter()

	// Inferencer returns a channel that closes when ctx is cancelled,
	// simulating a real upstream that respects request cancellation.
	inferencer := &stubInferencer{
		streamFn: func(ctx context.Context, _ string, _ map[string]string, _ map[string]any) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk)
			go func() {
				defer close(ch)
				<-ctx.Done()
			}()
			return ch, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	out, _, err := p.runStream(ctx, router, targets, nil, nil,
		map[string]string{"prompt": "hi"}, nil, inferencer, false)
	if err != nil {
		t.Fatal(err)
	}

	cancel()

	if !drainWithTimeout(out, 2*time.Second) {
		t.Fatal("out channel did not close after ctx cancel")
	}
}

func TestRunStream_ctxCancelMidStream_goroutineExits(t *testing.T) {
	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router, targets := singleTargetRouter()

	// out buffer is 32; send 33 chunks so the goroutine blocks on chunk 33,
	// which forces the ctx.Done() branch of the select to fire.
	const nChunks = 33
	upstream := make(chan StreamChunk, nChunks)
	for range nChunks {
		upstream <- StreamChunk{Delta: "x"}
	}
	close(upstream)

	inferencer := &stubInferencer{
		streamFn: func(_ context.Context, _ string, _ map[string]string, _ map[string]any) (<-chan StreamChunk, error) {
			return upstream, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled so goroutine hits ctx.Done() at first blocked send

	out, _, err := p.runStream(ctx, router, targets, nil, nil,
		map[string]string{"prompt": "hi"}, nil, inferencer, false)
	if err != nil {
		t.Fatal(err)
	}

	// Drain without blocking — goroutine must exit even if we consume slowly.
	if !drainWithTimeout(out, 2*time.Second) {
		t.Fatal("goroutine leaked: out channel never closed after ctx cancel")
	}
}

func TestRunStream_inferenceError_returnsError(t *testing.T) {
	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router, targets := singleTargetRouter()

	inferErr := errors.New("upstream down")
	inferencer := &stubInferencer{
		streamFn: func(_ context.Context, _ string, _ map[string]string, _ map[string]any) (<-chan StreamChunk, error) {
			return nil, inferErr
		},
	}

	_, _, err := p.runStream(context.Background(), router, targets, nil, nil,
		map[string]string{"prompt": "hi"}, nil, inferencer, false)
	if !errors.Is(err, inferErr) {
		t.Errorf("want inferErr, got %v", err)
	}
}

func TestRunStream_noEnabledTargets_returnsError(t *testing.T) {
	p := newFeaturePipeline(nil, nil, nil, nil, nil, nil)
	router := &domain.Router{ID: "rtr_1", Strategy: domain.RoutingStrategyFailover}
	// All targets disabled
	targets := []domain.RouterTarget{{ID: "tgt_1", ModelID: "mdl_1", IsEnabled: false}}

	_, _, err := p.runStream(context.Background(), router, targets, nil, nil,
		map[string]string{"prompt": "hi"}, nil, &stubInferencer{}, false)
	if err == nil {
		t.Fatal("want error for no enabled targets, got nil")
	}
}
