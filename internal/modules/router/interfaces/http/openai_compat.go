package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/router/application"

	"github.com/gin-gonic/gin"
)

// ── Request types ─────────────────────────────────────────────────────────────

// openAIContentPart is one element of a multimodal content array.
type openAIContentPart struct {
	Type     string          `json:"type"` // "text" | "image_url"
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

// openAIChatMessage mirrors the OpenAI chat message schema.
// Content may be a plain JSON string or an array of content parts (multimodal).
type openAIChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// text returns the concatenated plain-text content of the message.
func (m *openAIChatMessage) text() string {
	if len(m.Content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var parts []openAIContentPart
	if json.Unmarshal(m.Content, &parts) != nil {
		return string(m.Content)
	}
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// imageURLs extracts image URLs from an array-form content message.
func (m *openAIChatMessage) imageURLs() []string {
	var parts []openAIContentPart
	if json.Unmarshal(m.Content, &parts) != nil {
		return nil
	}
	var urls []string
	for _, p := range parts {
		if p.Type == "image_url" && p.ImageURL != nil {
			urls = append(urls, p.ImageURL.URL)
		}
	}
	return urls
}

// openAIChatRequest mirrors the OpenAI /v1/chat/completions request body.
type openAIChatRequest struct {
	Model            string              `json:"model"`
	Messages         []openAIChatMessage `json:"messages" binding:"required,min=1"`
	Temperature      *float64            `json:"temperature"`
	MaxTokens        *int                `json:"max_tokens"`
	TopP             *float64            `json:"top_p"`
	FrequencyPenalty *float64            `json:"frequency_penalty"`
	PresencePenalty  *float64            `json:"presence_penalty"`
	Stop             json.RawMessage     `json:"stop"` // string | []string | null
	Seed             *int                `json:"seed"`
	N                *int                `json:"n"`
	Tools            json.RawMessage     `json:"tools"`       // []tool object | null
	ToolChoice       json.RawMessage     `json:"tool_choice"` // "auto" | "none" | object | null
	Stream           bool                `json:"stream"`
	Metadata         map[string]any      `json:"metadata,omitempty"`
}

// parsedInput is the result of normalising the messages array.
type parsedInput struct {
	SystemPrompt string
	// History holds all turns before the final user turn, in order.
	History []parsedMessage
	// UserContent is the text of the last user message.
	UserContent string
	// ImageURLs are extracted from multimodal content in the last user message.
	ImageURLs []string
}

type parsedMessage struct {
	Role    string
	Content string
}

// parseMessages extracts system prompt, history, and the final user turn from
// the messages array in the order the OpenAI spec defines.
func (r *openAIChatRequest) parseMessages() parsedInput {
	// Locate the last user message — everything before it is history.
	lastUserIdx := -1
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	var result parsedInput
	for i, msg := range r.Messages {
		switch msg.Role {
		case "system":
			result.SystemPrompt = msg.text()
		case "user":
			if i == lastUserIdx {
				result.UserContent = msg.text()
				result.ImageURLs = msg.imageURLs()
			} else {
				result.History = append(result.History, parsedMessage{Role: "user", Content: msg.text()})
			}
		case "assistant":
			// Include assistant turns that precede the last user message.
			if lastUserIdx == -1 || i < lastUserIdx {
				result.History = append(result.History, parsedMessage{Role: "assistant", Content: msg.text()})
			}
		}
	}
	return result
}

// toOptions converts the request's generation parameters into an options map
// that the router pipeline passes through to the upstream provider.
func (r *openAIChatRequest) toOptions() map[string]any {
	opts := map[string]any{}
	if r.Temperature != nil {
		opts["temperature"] = *r.Temperature
	}
	if r.MaxTokens != nil {
		opts["max_tokens"] = *r.MaxTokens
	}
	if r.TopP != nil {
		opts["top_p"] = *r.TopP
	}
	if r.FrequencyPenalty != nil {
		opts["frequency_penalty"] = *r.FrequencyPenalty
	}
	if r.PresencePenalty != nil {
		opts["presence_penalty"] = *r.PresencePenalty
	}
	if len(r.Stop) > 0 && string(r.Stop) != "null" {
		var stop any
		if json.Unmarshal(r.Stop, &stop) == nil {
			opts["stop"] = stop
		}
	}
	if r.Seed != nil {
		opts["seed"] = *r.Seed
	}
	if r.N != nil && *r.N > 1 {
		opts["n"] = *r.N
	}
	for _, key := range []string{
		"agent_session_id",
		"session_id",
		"conversation_id",
		"agent",
		"agent_role",
		"parent_session_id",
		"parent_agent_session_id",
		"parent_agent",
		"agent_user_id",
		"user_id",
		"subject_user_id",
		"parent_agent_user_id",
		"parent_user_id",
		"parent_subject_user_id",
		"turn_index",
	} {
		if value, ok := r.Metadata[key]; ok {
			opts[key] = value
		}
	}
	if len(r.Tools) > 0 && string(r.Tools) != "null" {
		var tools any
		if json.Unmarshal(r.Tools, &tools) == nil {
			opts["tools"] = tools
		}
	}
	if len(r.ToolChoice) > 0 && string(r.ToolChoice) != "null" {
		var tc any
		if json.Unmarshal(r.ToolChoice, &tc) == nil {
			opts["tool_choice"] = tc
		}
	}
	return opts
}

// ── Tool-call response types ──────────────────────────────────────────────────

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"` // "function"
	Function openAIToolCallFunction `json:"function"`
}

// ── Non-streaming response types ──────────────────────────────────────────────

// openAIResponseMessage is the assistant turn in a non-streaming choice.
// Content is a pointer so it serialises as null when tool_calls are present.
type openAIResponseMessage struct {
	Role      string           `json:"role"`
	Content   *string          `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIChoice struct {
	Index        int                   `json:"index"`
	Message      openAIResponseMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

// buildOpenAIResponse converts a RouteInferResult into an OpenAI-compatible
// response, using the provider-reported token counts when available.
func buildOpenAIResponse(result *application.RouteInferResult, requestedModel string) openAIChatResponse {
	model := result.ModelDefKey
	if model == "" {
		model = requestedModel
	}

	inputTok := int(result.InputTokens)
	outputTok := int(result.OutputTokens)

	finishReason := "stop"
	msg := openAIResponseMessage{Role: "assistant"}

	if len(result.ToolCalls) > 0 {
		var calls []openAIToolCall
		if json.Unmarshal(result.ToolCalls, &calls) == nil && len(calls) > 0 {
			msg.ToolCalls = calls
			finishReason = "tool_calls"
		}
	}
	if msg.ToolCalls == nil {
		c := result.Content
		msg.Content = &c
	}

	return openAIChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openAIChoice{
			{Index: 0, Message: msg, FinishReason: finishReason},
		},
		Usage: openAIUsage{
			PromptTokens:     inputTok,
			CompletionTokens: outputTok,
			TotalTokens:      inputTok + outputTok,
		},
	}
}

// ── Streaming response types ──────────────────────────────────────────────────

type openAIStreamToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIStreamToolCall struct {
	Index    int                          `json:"index"`
	ID       string                       `json:"id,omitempty"`
	Type     string                       `json:"type,omitempty"`
	Function openAIStreamToolCallFunction `json:"function"`
}

type openAIStreamDelta struct {
	Role      string                 `json:"role,omitempty"`
	Content   *string                `json:"content,omitempty"`
	ToolCalls []openAIStreamToolCall `json:"tool_calls,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openAIStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
	Usage   *openAIStreamUsage   `json:"usage,omitempty"`
}

func buildOpenAIStreamChunk(id, model, delta string) openAIStreamChunk {
	c := delta
	return openAIStreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openAIStreamChoice{
			{Index: 0, Delta: openAIStreamDelta{Content: &c}},
		},
	}
}

func buildOpenAIStreamDone(id, model string, inputTok, outputTok int) openAIStreamChunk {
	reason := "stop"
	return openAIStreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openAIStreamChoice{
			{Index: 0, Delta: openAIStreamDelta{}, FinishReason: &reason},
		},
		Usage: &openAIStreamUsage{
			PromptTokens:     inputTok,
			CompletionTokens: outputTok,
			TotalTokens:      inputTok + outputTok,
		},
	}
}

// buildOpenAIStreamToolCallsDone emits a final chunk with finish_reason=tool_calls
// and the complete accumulated tool_calls array.
func buildOpenAIStreamToolCallsDone(id, model string, toolCalls []openAIToolCall, inputTok, outputTok int) openAIStreamChunk {
	reason := "tool_calls"
	streamCalls := make([]openAIStreamToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		streamCalls[i] = openAIStreamToolCall{
			Index: i,
			ID:    tc.ID,
			Type:  "function",
			Function: openAIStreamToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return openAIStreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openAIStreamChoice{
			{Index: 0, Delta: openAIStreamDelta{ToolCalls: streamCalls}, FinishReason: &reason},
		},
		Usage: &openAIStreamUsage{
			PromptTokens:     inputTok,
			CompletionTokens: outputTok,
			TotalTokens:      inputTok + outputTok,
		},
	}
}

// ── Embeddings API ────────────────────────────────────────────────────────────

type openAIEmbeddingRequest struct {
	Input          any    `json:"input"` // string or []string
	Model          string `json:"model"`
	EncodingFormat string `json:"encoding_format"` // "float" (default) | "base64"
}

type openAIEmbeddingObject struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIEmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type openAIEmbeddingResponse struct {
	Object string                  `json:"object"`
	Data   []openAIEmbeddingObject `json:"data"`
	Model  string                  `json:"model"`
	Usage  openAIEmbeddingUsage    `json:"usage"`
}

// OpenAIEmbeddings godoc
// @Summary     OpenAI-compatible embeddings
// @Description Accepts an OpenAI /v1/embeddings request and routes it through the router's embedding model.
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string  true  "Router ID"
// @Param       body  body      object  true  "OpenAI embeddings request"
// @Success     200   {object}  object
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/v1/embeddings [post]
func (h *Handler) OpenAIEmbeddings(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var req openAIEmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err, &req)
		return
	}

	var texts []string
	switch v := req.Input.(type) {
	case string:
		texts = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				texts = append(texts, s)
			}
		}
	default:
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "input must be a string or array of strings"})
		return
	}
	if len(texts) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "input must not be empty"})
		return
	}

	vecs, modelDefKey, err := h.svc.RouteEmbed(c.Request.Context(), id, texts)
	if err != nil {
		respondError(c, err)
		return
	}

	data := make([]openAIEmbeddingObject, len(vecs))
	totalTokens := 0
	for i, v := range vecs {
		data[i] = openAIEmbeddingObject{Object: "embedding", Embedding: v, Index: i}
		totalTokens += len(texts[i]) / 4
	}

	model := req.Model
	if model == "" {
		model = modelDefKey
	}
	c.JSON(http.StatusOK, openAIEmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  model,
		Usage:  openAIEmbeddingUsage{PromptTokens: totalTokens, TotalTokens: totalTokens},
	})
}

// ── Anthropic Messages API ────────────────────────────────────────────────────

type anthropicContentBlock struct {
	Type string `json:"type"` // "text" | "image"
	Text string `json:"text,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"  binding:"required"`
	System      string             `json:"system"`
	MaxTokens   int                `json:"max_tokens"`
	Stream      bool               `json:"stream"`
	TopP        *float64           `json:"top_p"`
	Temperature *float64           `json:"temperature"`
}

type anthropicUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type anthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []anthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        anthropicUsage          `json:"usage"`
}

// AnthropicMessages handles POST /v1/messages in Anthropic SDK format.
// It translates the request into the router pipeline and returns an Anthropic-shaped response.
func (h *Handler) AnthropicMessages(c *gin.Context) {
	id := c.Param("id")
	if id == "" || len(id) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": "invalid router id"}})
		return
	}

	var req anthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return
	}

	// Build fields from Anthropic message format
	fields := map[string]string{}

	if req.System != "" {
		fields["systemPrompt"] = req.System
	}

	// Last user message becomes the prompt; prior turns become history
	var history []map[string]string
	var userContent string
	for i, msg := range req.Messages {
		text := extractAnthropicText(msg.Content)
		if i == len(req.Messages)-1 && msg.Role == "user" {
			userContent = text
		} else {
			history = append(history, map[string]string{"role": msg.Role, "content": text})
		}
	}
	fields["prompt"] = userContent
	if len(history) > 0 {
		if b, err := json.Marshal(history); err == nil {
			fields["_history"] = string(b)
		}
	}

	options := map[string]any{}
	if req.MaxTokens > 0 {
		options["max_tokens"] = req.MaxTokens
	}
	if req.TopP != nil {
		options["top_p"] = *req.TopP
	}
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	options = injectAgentSessionHeaders(c, options)

	input := application.RouteInferInput{Fields: fields, Options: options}

	if req.Stream {
		h.writeAnthropicSSEStream(c, id, req.Model, input)
		return
	}

	result, err := h.svc.RouteInfer(c.Request.Context(), id, input)
	if err != nil {
		slog.Error("anthropic route infer", "routerID", id, "err", err)
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, anthropicResponse{
		ID:         "msg_" + result.SelectedModelID,
		Type:       "message",
		Role:       "assistant",
		Content:    []anthropicContentBlock{{Type: "text", Text: result.Content}},
		Model:      result.ModelDefKey,
		StopReason: "end_turn",
		Usage:      anthropicUsage{InputTokens: result.InputTokens, OutputTokens: result.OutputTokens},
	})
}

func extractAnthropicText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	}
	return fmt.Sprintf("%v", content)
}

func (h *Handler) writeAnthropicSSEStream(c *gin.Context, routerID, modelHint string, input application.RouteInferInput) {
	ch, err := h.svc.RouteInferStream(c.Request.Context(), routerID, input)
	if err != nil {
		slog.Error("anthropic route infer stream", "routerID", routerID, "err", err)
		respondError(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	msgID := "msg_stream_" + routerID
	// message_start
	writeAnthropicEvent(c, "message_start", map[string]any{
		"type":    "message_start",
		"message": map[string]any{"id": msgID, "type": "message", "role": "assistant", "content": []any{}, "model": modelHint, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}},
	})
	writeAnthropicEvent(c, "content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	writeAnthropicEvent(c, "ping", map[string]any{"type": "ping"})

	for chunk := range ch {
		if chunk.Err != nil {
			writeAnthropicEvent(c, "error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": chunk.Err.Error()}})
			return
		}
		if chunk.Delta != "" {
			writeAnthropicEvent(c, "content_block_delta", map[string]any{
				"type": "content_block_delta", "index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Delta},
			})
		}
		if chunk.Done {
			writeAnthropicEvent(c, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
			writeAnthropicEvent(c, "message_delta", map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
				"usage": map[string]any{"output_tokens": chunk.OutputTokens},
			})
			writeAnthropicEvent(c, "message_stop", map[string]any{"type": "message_stop"})
		}
	}
	c.Writer.Flush()
}

func writeAnthropicEvent(c *gin.Context, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, b)
	c.Writer.Flush()
}
