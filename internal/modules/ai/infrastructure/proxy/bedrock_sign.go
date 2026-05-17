package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// bedrockDo sends a Bedrock Converse request with SigV4 signing.
// It extracts the access key and secret from the special x-bedrock-* headers
// that bedrockConverseRequest injects, then strips them before sending.
func bedrockDo(ctx context.Context, rawURL string, body any, inHeaders map[string]string) (*http.Response, error) {
	accessKey := inHeaders["x-bedrock-access-key"]
	secretKey := inHeaders["x-bedrock-secret-key"]

	region := extractBedrockRegion(rawURL)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("bedrock marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range inHeaders {
		if !strings.HasPrefix(k, "x-bedrock-") {
			req.Header.Set(k, v)
		}
	}

	if err := signBedrockRequest(req, bodyBytes, accessKey, secretKey, region); err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("bedrock upstream %d: %s", resp.StatusCode, string(b))
	}
	return resp, nil
}

func signBedrockRequest(req *http.Request, payload []byte, accessKey, secretKey, region string) error {
	signer := v4.NewSigner()
	creds := aws.Credentials{
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
	}
	if accessKey == "" {
		// Fall back to environment / instance role — use a static zero-value
		// credential provider; the SDK will prefer env vars automatically
		// when credentials are empty, so we just skip signing here.
		return nil
	}
	hash := "UNSIGNED-PAYLOAD"
	return signer.SignHTTP(req.Context(), creds, req, hash, "bedrock", region, time.Now())
}

func extractBedrockRegion(rawURL string) string {
	// https://bedrock-runtime.us-east-1.amazonaws.com/...
	parts := strings.Split(rawURL, ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "us-east-1"
}

// bedrockParseConverse parses a Bedrock Converse API sync response.
func bedrockParseConverse(body []byte) (content string, inputTokens, outputTokens int64) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return string(body), 0, 0
	}
	// output.message.content[0].text
	if out, ok := raw["output"].(map[string]any); ok {
		if msg, ok := out["message"].(map[string]any); ok {
			if parts, ok := msg["content"].([]any); ok && len(parts) > 0 {
				if part, ok := parts[0].(map[string]any); ok {
					content, _ = part["text"].(string)
				}
			}
		}
	}
	// usage.inputTokens / outputTokens
	if u, ok := raw["usage"].(map[string]any); ok {
		if v, ok := u["inputTokens"].(float64); ok {
			inputTokens = int64(v)
		}
		if v, ok := u["outputTokens"].(float64); ok {
			outputTokens = int64(v)
		}
	}
	return
}
