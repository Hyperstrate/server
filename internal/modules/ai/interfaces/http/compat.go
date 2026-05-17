package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/ai/application"

	"github.com/gin-gonic/gin"
)

// ── Shared request types ──────────────────────────────────────────────────────

type compatContentPart struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *compatImageURL     `json:"image_url,omitempty"`
}

type compatImageURL struct {
	URL string `json:"url"`
}

// ── OpenAI ────────────────────────────────────────────────────────────────────

type compatChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type compatChatRequest struct {
	Messages    []compatChatMessage `json:"messages"   binding:"required"`
	Stream      bool                `json:"stream"`
	MaxTokens   *int                `json:"max_tokens"`
	Temperature *float64            `json:"temperature"`
	TopP        *float64            `json:"top_p"`
	Stop        any                 `json:"stop"`
}

// ModelOpenAIChatCompletions handles POST /v1/chat/completions for a direct model.
func (h *Handler) ModelOpenAIChatCompletions(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" || len(modelID) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model id"})
		return
	}

	var req compatChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fields, options := compatParseOpenAI(req)

	input := application.InferRequest{
		ModelID: modelID,
		Fields:  fields,
		Options: options,
	}

	if req.Stream {
		compatWriteOpenAIStream(c, modelID, input, func(i application.InferRequest) (<-chan application.StreamChunk, error) {
			return h.svc.InferStream(c.Request.Context(), i)
		})
		return
	}

	result, err := h.svc.Infer(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, compatBuildOpenAIResponse(modelID, *result))
}

// ── Anthropic ─────────────────────────────────────────────────────────────────

type compatAnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type compatAnthropicRequest struct {
	Messages    []compatAnthropicMessage `json:"messages"   binding:"required"`
	System      string                   `json:"system"`
	MaxTokens   int                      `json:"max_tokens"`
	Stream      bool                     `json:"stream"`
	Temperature *float64                 `json:"temperature"`
	TopP        *float64                 `json:"top_p"`
}

// ModelAnthropicMessages handles POST /v1/messages for a direct model.
func (h *Handler) ModelAnthropicMessages(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" || len(modelID) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": "invalid model id"}})
		return
	}

	var req compatAnthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return
	}

	fields := map[string]string{}
	if req.System != "" {
		fields["systemPrompt"] = req.System
	}

	var history []map[string]string
	var userContent string
	for i, msg := range req.Messages {
		text := compatExtractText(msg.Content)
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
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		options["top_p"] = *req.TopP
	}

	input := application.InferRequest{ModelID: modelID, Fields: fields, Options: options}

	if req.Stream {
		compatWriteAnthropicStream(c, modelID, input, func(i application.InferRequest) (<-chan application.StreamChunk, error) {
			return h.svc.InferStream(c.Request.Context(), i)
		})
		return
	}

	result, err := h.svc.Infer(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, map[string]any{
		"id":           "msg_" + modelID,
		"type":         "message",
		"role":         "assistant",
		"content":      []map[string]any{{"type": "text", "text": result.Content}},
		"model":        result.ModelDefKey,
		"stop_reason":  "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  result.InputTokens,
			"output_tokens": result.OutputTokens,
		},
	})
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func compatParseOpenAI(req compatChatRequest) (fields map[string]string, options map[string]any) {
	fields = map[string]string{}
	var history []map[string]string

	for i, msg := range req.Messages {
		text := compatExtractMessageText(msg)
		switch msg.Role {
		case "system":
			fields["systemPrompt"] = text
		case "user":
			if i == len(req.Messages)-1 {
				fields["prompt"] = text
			} else {
				history = append(history, map[string]string{"role": "user", "content": text})
			}
		default:
			history = append(history, map[string]string{"role": msg.Role, "content": text})
		}
	}

	if len(history) > 0 {
		if b, err := json.Marshal(history); err == nil {
			fields["_history"] = string(b)
		}
	}

	options = map[string]any{}
	if req.MaxTokens != nil {
		options["max_tokens"] = *req.MaxTokens
	}
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		options["top_p"] = *req.TopP
	}
	if req.Stop != nil {
		options["stop"] = req.Stop
	}
	return
}

func compatExtractMessageText(msg compatChatMessage) string {
	// Try plain string first
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		return s
	}
	// Try content part array
	var parts []compatContentPart
	if json.Unmarshal(msg.Content, &parts) == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return string(msg.Content)
}

func compatExtractText(content any) string {
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

func compatBuildOpenAIResponse(modelID string, result application.InferenceResult) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-" + modelID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   result.ModelDefKey,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": result.Content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     result.InputTokens,
			"completion_tokens": result.OutputTokens,
			"total_tokens":      result.InputTokens + result.OutputTokens,
		},
	}
}

func compatWriteOpenAIStream(
	c *gin.Context,
	modelID string,
	input application.InferRequest,
	infer func(application.InferRequest) (<-chan application.StreamChunk, error),
) {
	ch, err := infer(input)
	if err != nil {
		respondError(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	chunkID := "chatcmpl-" + modelID
	for chunk := range ch {
		if chunk.Err != nil {
			data, _ := json.Marshal(map[string]any{"error": chunk.Err.Error()})
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()
			return
		}
		if chunk.Done {
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			c.Writer.Flush()
			return
		}
		event := map[string]any{
			"id":      chunkID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   modelID,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"role": "assistant", "content": chunk.Delta},
			}},
		}
		data, _ := json.Marshal(event)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}
}

func compatWriteAnthropicStream(
	c *gin.Context,
	modelID string,
	input application.InferRequest,
	infer func(application.InferRequest) (<-chan application.StreamChunk, error),
) {
	ch, err := infer(input)
	if err != nil {
		respondError(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	msgID := "msg_" + modelID
	writeEvent := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, b)
		c.Writer.Flush()
	}

	writeEvent("message_start", map[string]any{
		"type":    "message_start",
		"message": map[string]any{"id": msgID, "type": "message", "role": "assistant", "content": []any{}, "model": modelID},
	})
	writeEvent("content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	writeEvent("ping", map[string]any{"type": "ping"})

	for chunk := range ch {
		if chunk.Err != nil {
			writeEvent("error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": chunk.Err.Error()}})
			return
		}
		if chunk.Delta != "" {
			writeEvent("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Delta},
			})
		}
		if chunk.Done {
			writeEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
			writeEvent("message_delta", map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": "end_turn"},
				"usage": map[string]any{"output_tokens": chunk.OutputTokens},
			})
			writeEvent("message_stop", map[string]any{"type": "message_stop"})
		}
	}
}
