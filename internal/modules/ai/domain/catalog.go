package domain

import (
	"sort"
	"strings"
)

// InputFieldDef describes a single user-facing input field.
// The frontend uses this to render the correct control (text area, file picker, etc.)
// before sending a request. Field keys are also used as keys in the Fields map
// of inference requests and jobs.
type InputFieldDef struct {
	Key         string    `json:"key"      validate:"required"`
	Label       string    `json:"label"    validate:"required"`
	Type        InputType `json:"type"     validate:"required"`
	Required    bool      `json:"required"`
	Description string    `json:"description,omitempty"`
	MaxLength   int       `json:"maxLength,omitempty"`
}

// CredentialFieldDef describes a single credential input for configuring a model registration.
// The frontend renders only the fields defined here instead of showing all possible credential inputs.
type CredentialFieldDef struct {
	Key         string `json:"key"                  validate:"required"`
	Label       string `json:"label"                validate:"required"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// ModelDefinition is the static, immutable specification of one AI model.
// Definitions live in the catalog and are never stored in the database.
type ModelDefinition struct {
	Key         string          `json:"key"         validate:"required"`
	Provider    Provider        `json:"provider"    validate:"required"`
	ModelID     string          `json:"modelId"     validate:"required"` // identifier sent to the upstream API (e.g. "gpt-4o")
	DisplayName string          `json:"displayName" validate:"required"`
	Description string          `json:"description,omitempty"`
	InputFields []InputFieldDef `json:"inputFields" validate:"required"`

	// Pricing in USD per 1M tokens (0 = unknown/free).
	InputPricePer1MTokens  float64 `json:"inputPricePer1MTokens"`
	OutputPricePer1MTokens float64 `json:"outputPricePer1MTokens"`
	// ContextWindowTokens is the maximum token count accepted by the model.
	ContextWindowTokens int `json:"contextWindowTokens"`
	// Capabilities is a set of feature flags (e.g. "vision", "reasoning", "function_calling").
	Capabilities []string `json:"capabilities,omitempty"`

	// DefaultBaseURL is the provider's canonical API endpoint, pre-filled in the configuration UI.
	DefaultBaseURL string `json:"defaultBaseUrl,omitempty"`
	// CredentialFields lists the credential inputs shown when configuring this model.
	// Only the fields defined here are rendered — e.g. OpenAI shows only "API Key",
	// Kling shows "Access Key ID" + "Secret Access Key", Ollama shows nothing.
	CredentialFields []CredentialFieldDef `json:"credentialFields"`
}

// Per-provider credential field sets — shared across all models from the same provider.
var (
	credsOpenAI = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: true},
	}
	credsAnthropic = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: true},
	}
	credsGemini = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: true},
	}
	credsMistral = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: true},
	}

	credsKling = []CredentialFieldDef{
		{Key: "apiKey", Label: "Access Key ID", Required: true},
		{Key: "apiSecret", Label: "Secret Access Key", Required: true},
	}
	credsOllama = []CredentialFieldDef{} // self-hosted — no credentials needed

	// vLLM and LocalAI optionally require an API key depending on deployment config.
	credsVLLM = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: false,
			Description: "Optional — set to any non-empty value if your vLLM server requires authentication"},
	}
	credsLocalAI = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: false,
			Description: "Optional — set if your LocalAI server requires authentication"},
	}

	credsCustom = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: false},
		{Key: "apiSecret", Label: "API Secret", Required: false},
	}

	credsAzureOpenAI = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: true,
			Description: "Azure OpenAI key (from Azure portal, Keys and Endpoint)"},
		{Key: "apiSecret", Label: "API Version", Required: false,
			Description: "Defaults to 2024-02-01 when left blank"},
	}
	credsGroq = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: true},
	}
	credsCohere = []CredentialFieldDef{
		{Key: "apiKey", Label: "API Key", Required: true},
	}
	credsBedrock = []CredentialFieldDef{
		{Key: "apiKey", Label: "AWS Access Key ID", Required: false,
			Description: "Leave blank to use the instance IAM role or environment credentials"},
		{Key: "apiSecret", Label: "AWS Secret Access Key", Required: false},
	}
)

// standard OpenAI chat input fields
var oaiFields = []InputFieldDef{
	{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false, MaxLength: 32768},
	{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
}
var oaiFieldsVision = []InputFieldDef{
	{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false, MaxLength: 32768},
	{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
	{Key: "image", Label: "Image", Type: InputTypeImage, Required: false},
}
var anthropicFields = []InputFieldDef{
	{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false},
	{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
	{Key: "image", Label: "Image", Type: InputTypeImage, Required: false},
}
var geminiFields = []InputFieldDef{
	{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false},
	{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
	{Key: "image", Label: "Image", Type: InputTypeImage, Required: false},
}
var mistralFields = []InputFieldDef{
	{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false},
	{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
}

// catalog is the authoritative registry of all supported model definitions.
var catalog = map[string]ModelDefinition{

	// ── OpenAI — GPT-5.5 family ─────────────────────────────────────────────────

	"chatgpt/gpt-5.5": {
		Key: "chatgpt/gpt-5.5", Provider: ProviderOpenAi, ModelID: "gpt-5.5",
		DisplayName:           "GPT-5.5",
		Description:           "OpenAI's most capable model — long context with vision",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-5.5-pro": {
		Key: "chatgpt/gpt-5.5-pro", Provider: ProviderOpenAi, ModelID: "gpt-5.5-pro",
		DisplayName:           "GPT-5.5 Pro",
		Description:           "Most capable GPT-5.5 variant for the most demanding tasks",
		InputPricePer1MTokens: 30.00, OutputPricePer1MTokens: 180.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — GPT-5.4 family ─────────────────────────────────────────────────

	"chatgpt/gpt-5.4": {
		Key: "chatgpt/gpt-5.4", Provider: ProviderOpenAi, ModelID: "gpt-5.4",
		DisplayName:           "GPT-5.4",
		Description:           "Best intelligence at scale for agentic, coding, and professional workflows — 1M context with vision",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-5.4-mini": {
		Key: "chatgpt/gpt-5.4-mini", Provider: ProviderOpenAi, ModelID: "gpt-5.4-mini",
		DisplayName:           "GPT-5.4 mini",
		Description:           "Strongest mini model for coding, computer use, and subagents — 400K context with vision",
		InputPricePer1MTokens: 0.75, OutputPricePer1MTokens: 4.50,
		ContextWindowTokens: 400_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-5.4-nano": {
		Key: "chatgpt/gpt-5.4-nano", Provider: ProviderOpenAi, ModelID: "gpt-5.4-nano",
		DisplayName:           "GPT-5.4 nano",
		Description:           "Cheapest GPT-5.4-class model for simple high-volume tasks — 400K context with vision",
		InputPricePer1MTokens: 0.20, OutputPricePer1MTokens: 1.25,
		ContextWindowTokens: 400_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-5.4-pro": {
		Key: "chatgpt/gpt-5.4-pro", Provider: ProviderOpenAi, ModelID: "gpt-5.4-pro",
		DisplayName:           "GPT-5.4 Pro",
		Description:           "Highest-capability GPT-5.4 variant for advanced professional tasks",
		InputPricePer1MTokens: 30.00, OutputPricePer1MTokens: 180.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — GPT-5.2 family ─────────────────────────────────────────────────

	"chatgpt/gpt-5.2": {
		Key: "chatgpt/gpt-5.2", Provider: ProviderOpenAi, ModelID: "gpt-5.2",
		DisplayName:           "GPT-5.2",
		InputPricePer1MTokens: 1.75, OutputPricePer1MTokens: 14.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-5.2-pro": {
		Key: "chatgpt/gpt-5.2-pro", Provider: ProviderOpenAi, ModelID: "gpt-5.2-pro",
		DisplayName:           "GPT-5.2 Pro",
		InputPricePer1MTokens: 21.00, OutputPricePer1MTokens: 168.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — GPT-5 family ───────────────────────────────────────────────────

	"chatgpt/gpt-5.1": {
		Key: "chatgpt/gpt-5.1", Provider: ProviderOpenAi, ModelID: "gpt-5.1",
		DisplayName:           "GPT-5.1",
		InputPricePer1MTokens: 1.25, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-5": {
		Key: "chatgpt/gpt-5", Provider: ProviderOpenAi, ModelID: "gpt-5",
		DisplayName:           "GPT-5",
		InputPricePer1MTokens: 1.25, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-5-mini": {
		Key: "chatgpt/gpt-5-mini", Provider: ProviderOpenAi, ModelID: "gpt-5-mini",
		DisplayName:           "GPT-5 mini",
		InputPricePer1MTokens: 0.25, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-5-nano": {
		Key: "chatgpt/gpt-5-nano", Provider: ProviderOpenAi, ModelID: "gpt-5-nano",
		DisplayName:           "GPT-5 nano",
		InputPricePer1MTokens: 0.05, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-5-pro": {
		Key: "chatgpt/gpt-5-pro", Provider: ProviderOpenAi, ModelID: "gpt-5-pro",
		DisplayName:           "GPT-5 Pro",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 120.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — GPT-4.1 family ─────────────────────────────────────────────────

	"chatgpt/gpt-4.1": {
		Key: "chatgpt/gpt-4.1", Provider: ProviderOpenAi, ModelID: "gpt-4.1",
		DisplayName:           "GPT-4.1",
		Description:           "Strong instruction-following with vision support — 1M context",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 8.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-4.1-mini": {
		Key: "chatgpt/gpt-4.1-mini", Provider: ProviderOpenAi, ModelID: "gpt-4.1-mini",
		DisplayName:           "GPT-4.1 mini",
		Description:           "Compact and fast variant of GPT-4.1",
		InputPricePer1MTokens: 0.40, OutputPricePer1MTokens: 1.60,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-4.1-nano": {
		Key: "chatgpt/gpt-4.1-nano", Provider: ProviderOpenAi, ModelID: "gpt-4.1-nano",
		DisplayName:           "GPT-4.1 nano",
		Description:           "Smallest and fastest GPT-4.1 variant for high-volume, low-latency tasks",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},

	// ── OpenAI — GPT-4o family ──────────────────────────────────────────────────

	"chatgpt/gpt-4o": {
		Key: "chatgpt/gpt-4o", Provider: ProviderOpenAi, ModelID: "gpt-4o",
		DisplayName:           "GPT-4o",
		Description:           "OpenAI's flagship multimodal model — text and vision",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-4o-mini": {
		Key: "chatgpt/gpt-4o-mini", Provider: ProviderOpenAi, ModelID: "gpt-4o-mini",
		DisplayName:           "GPT-4o mini",
		Description:           "Fast and affordable model for lightweight tasks",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-4o-2024-05-13": {
		Key: "chatgpt/gpt-4o-2024-05-13", Provider: ProviderOpenAi, ModelID: "gpt-4o-2024-05-13",
		DisplayName:           "GPT-4o (2024-05-13)",
		Description:           "Original GPT-4o snapshot — pinned version",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — Reasoning (o-series) ───────────────────────────────────────────

	"chatgpt/o4-mini": {
		Key: "chatgpt/o4-mini", Provider: ProviderOpenAi, ModelID: "o4-mini",
		DisplayName:           "o4-mini",
		Description:           "Fast, cost-efficient reasoning model with vision support",
		InputPricePer1MTokens: 1.10, OutputPricePer1MTokens: 4.40,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/o3": {
		Key: "chatgpt/o3", Provider: ProviderOpenAi, ModelID: "o3",
		DisplayName:           "o3",
		Description:           "OpenAI's most capable reasoning model",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 8.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/o3-mini": {
		Key: "chatgpt/o3-mini", Provider: ProviderOpenAi, ModelID: "o3-mini",
		DisplayName:           "o3-mini",
		Description:           "Cost-efficient reasoning model balancing speed and intelligence",
		InputPricePer1MTokens: 1.10, OutputPricePer1MTokens: 4.40,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/o3-pro": {
		Key: "chatgpt/o3-pro", Provider: ProviderOpenAi, ModelID: "o3-pro",
		DisplayName:           "o3 Pro",
		Description:           "Maximum reasoning capability — highest accuracy on complex tasks",
		InputPricePer1MTokens: 20.00, OutputPricePer1MTokens: 80.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/o1": {
		Key: "chatgpt/o1", Provider: ProviderOpenAi, ModelID: "o1",
		DisplayName:           "o1",
		Description:           "OpenAI's first reasoning model — excels at complex math, science, and coding",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 60.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/o1-mini": {
		Key: "chatgpt/o1-mini", Provider: ProviderOpenAi, ModelID: "o1-mini",
		DisplayName:           "o1-mini",
		Description:           "Fast and affordable reasoning model optimised for STEM tasks",
		InputPricePer1MTokens: 1.10, OutputPricePer1MTokens: 4.40,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/o1-pro": {
		Key: "chatgpt/o1-pro", Provider: ProviderOpenAi, ModelID: "o1-pro",
		DisplayName:           "o1 Pro",
		Description:           "Most powerful o1 variant — highest accuracy at premium cost",
		InputPricePer1MTokens: 150.00, OutputPricePer1MTokens: 600.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — GPT-4 Turbo / legacy ───────────────────────────────────────────

	"chatgpt/gpt-4-turbo-2024-04-09": {
		Key: "chatgpt/gpt-4-turbo-2024-04-09", Provider: ProviderOpenAi, ModelID: "gpt-4-turbo-2024-04-09",
		DisplayName:           "GPT-4 Turbo (2024-04-09)",
		Description:           "GPT-4 Turbo with vision — 128K context",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-4-0125-preview": {
		Key: "chatgpt/gpt-4-0125-preview", Provider: ProviderOpenAi, ModelID: "gpt-4-0125-preview",
		DisplayName:           "GPT-4 Turbo Preview (0125)",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-4-1106-preview": {
		Key: "chatgpt/gpt-4-1106-preview", Provider: ProviderOpenAi, ModelID: "gpt-4-1106-preview",
		DisplayName:           "GPT-4 Turbo Preview (1106)",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-4-1106-vision-preview": {
		Key: "chatgpt/gpt-4-1106-vision-preview", Provider: ProviderOpenAi, ModelID: "gpt-4-1106-vision-preview",
		DisplayName:           "GPT-4 Turbo Vision Preview (1106)",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"chatgpt/gpt-4-0613": {
		Key: "chatgpt/gpt-4-0613", Provider: ProviderOpenAi, ModelID: "gpt-4-0613",
		DisplayName:           "GPT-4 (0613)",
		InputPricePer1MTokens: 30.00, OutputPricePer1MTokens: 60.00,
		ContextWindowTokens: 8_192,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-4-0314": {
		Key: "chatgpt/gpt-4-0314", Provider: ProviderOpenAi, ModelID: "gpt-4-0314",
		DisplayName:           "GPT-4 (0314)",
		InputPricePer1MTokens: 30.00, OutputPricePer1MTokens: 60.00,
		ContextWindowTokens: 8_192,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-4-32k": {
		Key: "chatgpt/gpt-4-32k", Provider: ProviderOpenAi, ModelID: "gpt-4-32k",
		DisplayName:           "GPT-4 32K",
		InputPricePer1MTokens: 60.00, OutputPricePer1MTokens: 120.00,
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},

	// ── OpenAI — GPT-3.5 / legacy ───────────────────────────────────────────────

	"chatgpt/gpt-3.5-turbo": {
		Key: "chatgpt/gpt-3.5-turbo", Provider: ProviderOpenAi, ModelID: "gpt-3.5-turbo",
		DisplayName:           "GPT-3.5 Turbo",
		InputPricePer1MTokens: 0.50, OutputPricePer1MTokens: 1.50,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-3.5-turbo-0125": {
		Key: "chatgpt/gpt-3.5-turbo-0125", Provider: ProviderOpenAi, ModelID: "gpt-3.5-turbo-0125",
		DisplayName:           "GPT-3.5 Turbo (0125)",
		InputPricePer1MTokens: 0.50, OutputPricePer1MTokens: 1.50,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-3.5-turbo-1106": {
		Key: "chatgpt/gpt-3.5-turbo-1106", Provider: ProviderOpenAi, ModelID: "gpt-3.5-turbo-1106",
		DisplayName:           "GPT-3.5 Turbo (1106)",
		InputPricePer1MTokens: 1.00, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-3.5-turbo-0613": {
		Key: "chatgpt/gpt-3.5-turbo-0613", Provider: ProviderOpenAi, ModelID: "gpt-3.5-turbo-0613",
		DisplayName:           "GPT-3.5 Turbo (0613)",
		InputPricePer1MTokens: 1.50, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 4_096,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-3.5-turbo-0301": {
		Key: "chatgpt/gpt-3.5-turbo-0301", Provider: ProviderOpenAi, ModelID: "gpt-3.5-turbo-0301",
		DisplayName:           "GPT-3.5 Turbo (0301)",
		InputPricePer1MTokens: 1.50, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 4_096,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-3.5-turbo-instruct": {
		Key: "chatgpt/gpt-3.5-turbo-instruct", Provider: ProviderOpenAi, ModelID: "gpt-3.5-turbo-instruct",
		DisplayName:           "GPT-3.5 Turbo Instruct",
		InputPricePer1MTokens: 1.50, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 4_096,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/gpt-3.5-turbo-16k-0613": {
		Key: "chatgpt/gpt-3.5-turbo-16k-0613", Provider: ProviderOpenAi, ModelID: "gpt-3.5-turbo-16k-0613",
		DisplayName:           "GPT-3.5 Turbo 16K (0613)",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 4.00,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/davinci-002": {
		Key: "chatgpt/davinci-002", Provider: ProviderOpenAi, ModelID: "davinci-002",
		DisplayName:           "davinci-002",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 16_384,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	"chatgpt/babbage-002": {
		Key: "chatgpt/babbage-002", Provider: ProviderOpenAi, ModelID: "babbage-002",
		DisplayName:           "babbage-002",
		InputPricePer1MTokens: 0.40, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 16_384,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},

	// ── Anthropic — Claude Opus 4.x ─────────────────────────────────────────────

	"anthropic/claude-opus-4-7": {
		Key: "anthropic/claude-opus-4-7", Provider: ProviderAnthropic, ModelID: "claude-opus-4-7",
		DisplayName:           "Claude Opus 4.7",
		Description:           "Anthropic's most capable model — best for complex reasoning and agentic coding",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 25.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-opus-4-6": {
		Key: "anthropic/claude-opus-4-6", Provider: ProviderAnthropic, ModelID: "claude-opus-4-6",
		DisplayName:           "Claude Opus 4.6",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 25.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-opus-4-5": {
		Key: "anthropic/claude-opus-4-5", Provider: ProviderAnthropic, ModelID: "claude-opus-4-5",
		DisplayName:           "Claude Opus 4.5",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 25.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-opus-4-1": {
		Key: "anthropic/claude-opus-4-1", Provider: ProviderAnthropic, ModelID: "claude-opus-4-1",
		DisplayName:           "Claude Opus 4.1",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-opus-4": {
		Key: "anthropic/claude-opus-4", Provider: ProviderAnthropic, ModelID: "claude-opus-4-20250514",
		DisplayName:           "Claude Opus 4",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Anthropic — Claude Sonnet 4.x ───────────────────────────────────────────

	"anthropic/claude-sonnet-4-6": {
		Key: "anthropic/claude-sonnet-4-6", Provider: ProviderAnthropic, ModelID: "claude-sonnet-4-6",
		DisplayName:           "Claude Sonnet 4.6",
		Description:           "Best combination of speed and intelligence — extended thinking, 1M context",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-sonnet-4-5": {
		Key: "anthropic/claude-sonnet-4-5", Provider: ProviderAnthropic, ModelID: "claude-sonnet-4-5",
		DisplayName:           "Claude Sonnet 4.5",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-sonnet-4": {
		Key: "anthropic/claude-sonnet-4", Provider: ProviderAnthropic, ModelID: "claude-sonnet-4-20250514",
		DisplayName:           "Claude Sonnet 4",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-sonnet-3-7": {
		Key: "anthropic/claude-sonnet-3-7", Provider: ProviderAnthropic, ModelID: "claude-sonnet-3-7-20250219",
		DisplayName:           "Claude Sonnet 3.7 (deprecated)",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Anthropic — Claude Haiku 4.x / 3.x ─────────────────────────────────────

	"anthropic/claude-haiku-4-5": {
		Key: "anthropic/claude-haiku-4-5", Provider: ProviderAnthropic, ModelID: "claude-haiku-4-5-20251001",
		DisplayName:           "Claude Haiku 4.5",
		Description:           "Fastest Claude model with near-frontier intelligence for lightweight tasks",
		InputPricePer1MTokens: 1.00, OutputPricePer1MTokens: 5.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-haiku-3-5": {
		Key: "anthropic/claude-haiku-3-5", Provider: ProviderAnthropic, ModelID: "claude-haiku-3-5-20241022",
		DisplayName:           "Claude Haiku 3.5",
		InputPricePer1MTokens: 0.80, OutputPricePer1MTokens: 4.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	"anthropic/claude-haiku-3": {
		Key: "anthropic/claude-haiku-3", Provider: ProviderAnthropic, ModelID: "claude-3-haiku-20240307",
		DisplayName:           "Claude Haiku 3",
		InputPricePer1MTokens: 0.25, OutputPricePer1MTokens: 1.25,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Anthropic — Claude Opus 3 (deprecated) ──────────────────────────────────

	"anthropic/claude-opus-3": {
		Key: "anthropic/claude-opus-3", Provider: ProviderAnthropic, ModelID: "claude-3-opus-20240229",
		DisplayName:           "Claude Opus 3 (deprecated)",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Gemini ──────────────────────────────────────────────────────────────────

	"gemini/gemini-2.5-pro": {
		Key: "gemini/gemini-2.5-pro", Provider: ProviderGemini, ModelID: "gemini-2.5-pro",
		DisplayName:           "Gemini 2.5 Pro",
		Description:           "Google's most capable Gemini model with deep reasoning — 1M context",
		InputPricePer1MTokens: 1.25, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		CredentialFields:    credsGemini,
		InputFields:         geminiFields,
	},
	"gemini/gemini-2.5-flash": {
		Key: "gemini/gemini-2.5-flash", Provider: ProviderGemini, ModelID: "gemini-2.5-flash",
		DisplayName:           "Gemini 2.5 Flash",
		Description:           "Google's fast and affordable reasoning model — 1M context with vision",
		InputPricePer1MTokens: 0.30, OutputPricePer1MTokens: 2.50,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		CredentialFields:    credsGemini,
		InputFields:         geminiFields,
	},
	"gemini/gemini-2.5-flash-lite": {
		Key: "gemini/gemini-2.5-flash-lite", Provider: ProviderGemini, ModelID: "gemini-2.5-flash-lite",
		DisplayName:           "Gemini 2.5 Flash-Lite",
		Description:           "Most cost-efficient Gemini model — optimised for high-volume tasks",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		CredentialFields:    credsGemini,
		InputFields:         geminiFields,
	},
	"gemini/gemini-2.0-flash": {
		Key: "gemini/gemini-2.0-flash", Provider: ProviderGemini, ModelID: "gemini-2.0-flash",
		DisplayName:           "Gemini 2.0 Flash",
		Description:           "Google's fast Gemini model — multimodal, low latency, 1M context",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		CredentialFields:    credsGemini,
		InputFields:         geminiFields,
	},

	// ── Mistral AI ──────────────────────────────────────────────────────────────

	"mistral/mistral-large": {
		Key: "mistral/mistral-large", Provider: ProviderMistral, ModelID: "mistral-large-latest",
		DisplayName:           "Mistral Large",
		Description:           "Mistral's flagship model for complex reasoning and tasks",
		InputPricePer1MTokens: 0.50, OutputPricePer1MTokens: 1.50,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/mistral-medium-3": {
		Key: "mistral/mistral-medium-3", Provider: ProviderMistral, ModelID: "mistral-medium-3",
		DisplayName:           "Mistral Medium 3",
		Description:           "Strong mid-tier model balancing capability and cost",
		InputPricePer1MTokens: 0.40, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/mistral-small": {
		Key: "mistral/mistral-small", Provider: ProviderMistral, ModelID: "mistral-small-latest",
		DisplayName:           "Mistral Small",
		Description:           "Fast and affordable Mistral model for everyday tasks",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 32_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/mistral-small-3-1": {
		Key: "mistral/mistral-small-3-1", Provider: ProviderMistral, ModelID: "mistral-small-3.1-latest",
		DisplayName:           "Mistral Small 3.1",
		Description:           "Upgraded small model with improved instruction-following",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.30,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/codestral": {
		Key: "mistral/codestral", Provider: ProviderMistral, ModelID: "codestral-latest",
		DisplayName:           "Codestral",
		Description:           "Mistral's code-specialised model — fill-in-the-middle and completion",
		InputPricePer1MTokens: 0.30, OutputPricePer1MTokens: 0.90,
		ContextWindowTokens: 256_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/pixtral-large": {
		Key: "mistral/pixtral-large", Provider: ProviderMistral, ModelID: "pixtral-large-latest",
		DisplayName:           "Pixtral Large",
		Description:           "Mistral's multimodal model — text and vision",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 6.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields: []InputFieldDef{
			{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false},
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
			{Key: "image", Label: "Image", Type: InputTypeImage, Required: false},
		},
	},
	"mistral/magistral-medium": {
		Key: "mistral/magistral-medium", Provider: ProviderMistral, ModelID: "magistral-medium-latest",
		DisplayName:           "Magistral Medium",
		Description:           "Mistral's reasoning-focused model",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 5.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/ministral-8b": {
		Key: "mistral/ministral-8b", Provider: ProviderMistral, ModelID: "ministral-8b-latest",
		DisplayName:           "Ministral 8B",
		Description:           "Compact 8B model optimised for edge and on-device use",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.10,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/ministral-3b": {
		Key: "mistral/ministral-3b", Provider: ProviderMistral, ModelID: "ministral-3b-latest",
		DisplayName:           "Ministral 3B",
		Description:           "Smallest Mistral model — ultra-low-cost inference",
		InputPricePer1MTokens: 0.04, OutputPricePer1MTokens: 0.04,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/open-mistral-nemo": {
		Key: "mistral/open-mistral-nemo", Provider: ProviderMistral, ModelID: "open-mistral-nemo",
		DisplayName:           "Mistral Nemo",
		Description:           "Open 12B model — best-in-class for its size",
		InputPricePer1MTokens: 0.01, OutputPricePer1MTokens: 0.03,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/open-mixtral-8x22b": {
		Key: "mistral/open-mixtral-8x22b", Provider: ProviderMistral, ModelID: "open-mixtral-8x22b",
		DisplayName:           "Mixtral 8x22B",
		Description:           "Open mixture-of-experts model — strong multilingual and reasoning performance",
		InputPricePer1MTokens: 1.20, OutputPricePer1MTokens: 1.20,
		ContextWindowTokens: 65_536,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	"mistral/open-mixtral-8x7b": {
		Key: "mistral/open-mixtral-8x7b", Provider: ProviderMistral, ModelID: "open-mixtral-8x7b",
		DisplayName:           "Mixtral 8x7B",
		Description:           "Open mixture-of-experts model — fast and capable for most tasks",
		InputPricePer1MTokens: 0.14, OutputPricePer1MTokens: 0.42,
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},

	// ── Ollama (self-hosted) ────────────────────────────────────────────────────

	"ollama/llama-3.3-70b": {
		Key: "ollama/llama-3.3-70b", Provider: ProviderOllama, ModelID: "llama3.3:70b",
		DisplayName:      "Llama 3.3 70B",
		Description:      "Meta's Llama 3.3 70B — self-hosted via Ollama",
		Capabilities:     []string{"function_calling"},
		DefaultBaseURL:   "http://localhost:11434",
		CredentialFields: credsOllama,
		InputFields: []InputFieldDef{
			{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false},
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
		},
	},
	"ollama/mistral-nemo": {
		Key: "ollama/mistral-nemo", Provider: ProviderOllama, ModelID: "mistral-nemo",
		DisplayName:      "Mistral Nemo (Ollama)",
		Description:      "Mistral Nemo — efficient self-hosted model via Ollama",
		Capabilities:     []string{"function_calling"},
		DefaultBaseURL:   "http://localhost:11434",
		CredentialFields: credsOllama,
		InputFields: []InputFieldDef{
			{Key: "systemPrompt", Label: "System Prompt", Type: InputTypeText, Required: false},
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true},
		},
	},

	// ── vLLM (self-hosted, OpenAI-compatible) ───────────────────────────────────

	"vllm/llama-3.1-8b-instruct": {
		Key: "vllm/llama-3.1-8b-instruct", Provider: ProviderVLLM, ModelID: "meta-llama/Meta-Llama-3.1-8B-Instruct",
		DisplayName:         "Llama 3.1 8B Instruct (vLLM)",
		Description:         "Meta's Llama 3.1 8B instruction-tuned model — fast self-hosted via vLLM",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFieldsVision,
	},
	"vllm/llama-3.1-70b-instruct": {
		Key: "vllm/llama-3.1-70b-instruct", Provider: ProviderVLLM, ModelID: "meta-llama/Meta-Llama-3.1-70B-Instruct",
		DisplayName:         "Llama 3.1 70B Instruct (vLLM)",
		Description:         "Meta's Llama 3.1 70B instruction-tuned model — self-hosted via vLLM",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},
	"vllm/mistral-7b-instruct": {
		Key: "vllm/mistral-7b-instruct", Provider: ProviderVLLM, ModelID: "mistralai/Mistral-7B-Instruct-v0.3",
		DisplayName:         "Mistral 7B Instruct (vLLM)",
		Description:         "Mistral 7B instruction-tuned — lightweight and fast on commodity GPUs",
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},
	"vllm/qwen2.5-72b-instruct": {
		Key: "vllm/qwen2.5-72b-instruct", Provider: ProviderVLLM, ModelID: "Qwen/Qwen2.5-72B-Instruct",
		DisplayName:         "Qwen 2.5 72B Instruct (vLLM)",
		Description:         "Alibaba's Qwen 2.5 72B — strong multilingual and coding capabilities",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},
	"vllm/deepseek-coder-v2-lite": {
		Key: "vllm/deepseek-coder-v2-lite", Provider: ProviderVLLM, ModelID: "deepseek-ai/DeepSeek-Coder-V2-Lite-Instruct",
		DisplayName:         "DeepSeek Coder V2 Lite (vLLM)",
		Description:         "Efficient self-hosted coding model — 16B MoE via vLLM",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 163_840,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},

	// ── LocalAI (self-hosted, OpenAI-compatible) ─────────────────────────────

	"localai/llama-3.1-8b-instruct": {
		Key: "localai/llama-3.1-8b-instruct", Provider: ProviderLocalAI, ModelID: "llama-3.1-8b-instruct",
		DisplayName:         "Llama 3.1 8B Instruct (LocalAI)",
		Description:         "Meta's Llama 3.1 8B via LocalAI — runs on CPU or GPU without CUDA required",
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},
	"localai/mistral-7b": {
		Key: "localai/mistral-7b", Provider: ProviderLocalAI, ModelID: "mistral-7b-instruct",
		DisplayName:         "Mistral 7B (LocalAI)",
		Description:         "Mistral 7B instruction model via LocalAI",
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},
	"localai/codellama-13b": {
		Key: "localai/codellama-13b", Provider: ProviderLocalAI, ModelID: "codellama-13b-instruct",
		DisplayName:         "CodeLlama 13B (LocalAI)",
		Description:         "Meta's CodeLlama 13B — code generation and completion via LocalAI",
		ContextWindowTokens: 16_384,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},
	"localai/phi-3-medium": {
		Key: "localai/phi-3-medium", Provider: ProviderLocalAI, ModelID: "phi-3-medium-128k-instruct",
		DisplayName:         "Phi-3 Medium (LocalAI)",
		Description:         "Microsoft's Phi-3 Medium — efficient reasoning model with 128K context via LocalAI",
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},

	// ── Kling ───────────────────────────────────────────────────────────────────

	"kling/kling-v1": {
		Key: "kling/kling-v1", Provider: ProviderKling, ModelID: "kling-v1",
		DisplayName:      "Kling v1",
		Description:      "Text-to-video generation",
		Capabilities:     []string{"video_generation"},
		DefaultBaseURL:   "https://api.klingai.com/v1",
		CredentialFields: credsKling,
		InputFields: []InputFieldDef{
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true, MaxLength: 2500,
				Description: "Describe the video you want to generate"},
		},
	},
	"kling/kling-v1-image": {
		Key: "kling/kling-v1-image", Provider: ProviderKling, ModelID: "kling-v1",
		DisplayName:      "Kling v1 Image-to-Video",
		Description:      "Animate a reference image into a video",
		Capabilities:     []string{"video_generation", "image_to_video"},
		DefaultBaseURL:   "https://api.klingai.com/v1",
		CredentialFields: credsKling,
		InputFields: []InputFieldDef{
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true, MaxLength: 2500},
			{Key: "referenceImage", Label: "Reference Image", Type: InputTypeImage, Required: true,
				Description: "The base image to animate"},
		},
	},
	"kling/kling-v2": {
		Key: "kling/kling-v2", Provider: ProviderKling, ModelID: "kling-v2",
		DisplayName:      "Kling v2",
		Description:      "Enhanced text-to-video generation",
		Capabilities:     []string{"video_generation"},
		DefaultBaseURL:   "https://api.klingai.com/v1",
		CredentialFields: credsKling,
		InputFields: []InputFieldDef{
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true, MaxLength: 2500},
		},
	},
	"kling/kling-v2.1": {
		Key: "kling/kling-v2.1", Provider: ProviderKling, ModelID: "kling-v2.1",
		DisplayName:      "Kling v2.1",
		Description:      "Improved fidelity and motion quality over v2",
		Capabilities:     []string{"video_generation", "image_to_video"},
		DefaultBaseURL:   "https://api.klingai.com/v1",
		CredentialFields: credsKling,
		InputFields: []InputFieldDef{
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true, MaxLength: 2500},
			{Key: "referenceImage", Label: "Reference Image", Type: InputTypeImage, Required: false},
		},
	},
	"kling/kling-v3-motion-control": {
		Key: "kling/kling-v3-motion-control", Provider: ProviderKling, ModelID: "kling-v3",
		DisplayName:      "Kling v3 Motion Control",
		Description:      "Generate videos with precise motion control: provide a character image and a motion reference video",
		Capabilities:     []string{"video_generation", "image_to_video", "motion_control"},
		DefaultBaseURL:   "https://api.klingai.com/v1",
		CredentialFields: credsKling,
		InputFields: []InputFieldDef{
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true, MaxLength: 2500,
				Description: "Describe the scene and action"},
			{Key: "characterImage", Label: "Character Image", Type: InputTypeImage, Required: true,
				Description: "The character to animate"},
			{Key: "motionVideo", Label: "Motion Video", Type: InputTypeVideo, Required: true,
				Description: "Reference video that provides the motion pattern"},
		},
	},

	// ── Custom ──────────────────────────────────────────────────────────────────

	"custom": {
		Key: "custom", Provider: ProviderCustom, ModelID: "",
		DisplayName:      "Custom",
		Description:      "Generic passthrough — all fields and options are forwarded verbatim to the configured endpoint",
		CredentialFields: credsCustom,
		InputFields: []InputFieldDef{
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: false,
				Description: "Sent as-is; add any additional fields via the options map"},
		},
	},

	// ── Embedding models ──────────────────────────────────────────────────────
	// These have no InputFields — they are called programmatically by semantic
	// features (semantic_cache, semantic_classifier), not through the inference UI.

	"openai/text-embedding-3-small": {
		Key: "openai/text-embedding-3-small", Provider: ProviderOpenAi, ModelID: "text-embedding-3-small",
		DisplayName:           "OpenAI Text Embedding 3 Small",
		Description:           "Fast, cost-effective embedding model for semantic search and routing",
		InputPricePer1MTokens: 0.02,
		ContextWindowTokens:   8_191,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.openai.com/v1",
		CredentialFields:      credsOpenAI,
		InputFields:           []InputFieldDef{},
	},
	"openai/text-embedding-3-large": {
		Key: "openai/text-embedding-3-large", Provider: ProviderOpenAi, ModelID: "text-embedding-3-large",
		DisplayName:           "OpenAI Text Embedding 3 Large",
		Description:           "High-accuracy embedding model for demanding semantic applications",
		InputPricePer1MTokens: 0.13,
		ContextWindowTokens:   8_191,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.openai.com/v1",
		CredentialFields:      credsOpenAI,
		InputFields:           []InputFieldDef{},
	},
	"openai/text-embedding-ada-002": {
		Key: "openai/text-embedding-ada-002", Provider: ProviderOpenAi, ModelID: "text-embedding-ada-002",
		DisplayName:           "OpenAI Text Embedding Ada 002",
		Description:           "Legacy OpenAI embedding model — use text-embedding-3-small for new projects",
		InputPricePer1MTokens: 0.10,
		ContextWindowTokens:   8_191,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.openai.com/v1",
		CredentialFields:      credsOpenAI,
		InputFields:           []InputFieldDef{},
	},
	"mistral/mistral-embed": {
		Key: "mistral/mistral-embed", Provider: ProviderMistral, ModelID: "mistral-embed",
		DisplayName:           "Mistral Embed",
		Description:           "Mistral's embedding model for semantic search and classification",
		InputPricePer1MTokens: 0.10,
		ContextWindowTokens:   8_192,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.mistral.ai/v1",
		CredentialFields:      credsMistral,
		InputFields:           []InputFieldDef{},
	},
	"ollama/nomic-embed-text": {
		Key: "ollama/nomic-embed-text", Provider: ProviderOllama, ModelID: "nomic-embed-text",
		DisplayName:      "Nomic Embed Text (Ollama)",
		Description:      "Local open-source embedding model for semantic search and routing",
		Capabilities:     []string{"embedding"},
		CredentialFields: credsOllama,
		InputFields:      []InputFieldDef{},
	},
	"ollama/mxbai-embed-large": {
		Key: "ollama/mxbai-embed-large", Provider: ProviderOllama, ModelID: "mxbai-embed-large",
		DisplayName:      "MixedBread Embed Large (Ollama)",
		Description:      "High-performance local embedding model via Ollama",
		Capabilities:     []string{"embedding"},
		CredentialFields: credsOllama,
		InputFields:      []InputFieldDef{},
	},
	// ── Azure OpenAI ──────────────────────────────────────────────────────────────

	"azure/gpt-4o": {
		Key: "azure/gpt-4o", Provider: ProviderAzureOpenAI, ModelID: "gpt-4o",
		DisplayName:           "GPT-4o (Azure)",
		Description:           "OpenAI GPT-4o hosted on Azure OpenAI Service",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://<resource>.openai.azure.com/openai/deployments/<deployment>",
		CredentialFields:    credsAzureOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"azure/gpt-4o-mini": {
		Key: "azure/gpt-4o-mini", Provider: ProviderAzureOpenAI, ModelID: "gpt-4o-mini",
		DisplayName:           "GPT-4o Mini (Azure)",
		Description:           "Fast and affordable GPT-4o Mini on Azure",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://<resource>.openai.azure.com/openai/deployments/<deployment>",
		CredentialFields:    credsAzureOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"azure/gpt-4.1": {
		Key: "azure/gpt-4.1", Provider: ProviderAzureOpenAI, ModelID: "gpt-4.1",
		DisplayName:           "GPT-4.1 (Azure)",
		Description:           "OpenAI GPT-4.1 hosted on Azure OpenAI Service",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 8.00,
		ContextWindowTokens: 1_047_576,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://<resource>.openai.azure.com/openai/deployments/<deployment>",
		CredentialFields:    credsAzureOpenAI,
		InputFields:         oaiFieldsVision,
	},
	"azure/o3-mini": {
		Key: "azure/o3-mini", Provider: ProviderAzureOpenAI, ModelID: "o3-mini",
		DisplayName:           "o3-mini (Azure)",
		Description:           "OpenAI o3-mini reasoning model on Azure",
		InputPricePer1MTokens: 1.10, OutputPricePer1MTokens: 4.40,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"reasoning", "function_calling"},
		DefaultBaseURL:      "https://<resource>.openai.azure.com/openai/deployments/<deployment>",
		CredentialFields:    credsAzureOpenAI,
		InputFields:         oaiFields,
	},

	// ── Groq ─────────────────────────────────────────────────────────────────────

	"groq/llama-3.3-70b": {
		Key: "groq/llama-3.3-70b", Provider: ProviderGroq, ModelID: "llama-3.3-70b-versatile",
		DisplayName:           "Llama 3.3 70B (Groq)",
		Description:           "Meta Llama 3.3 70B via Groq — ultra-fast inference",
		InputPricePer1MTokens: 0.59, OutputPricePer1MTokens: 0.79,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	"groq/llama-3.1-8b": {
		Key: "groq/llama-3.1-8b", Provider: ProviderGroq, ModelID: "llama-3.1-8b-instant",
		DisplayName:           "Llama 3.1 8B Instant (Groq)",
		Description:           "Fastest small model on Groq — ideal for high-throughput tasks",
		InputPricePer1MTokens: 0.05, OutputPricePer1MTokens: 0.08,
		ContextWindowTokens: 131_072,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	"groq/mixtral-8x7b": {
		Key: "groq/mixtral-8x7b", Provider: ProviderGroq, ModelID: "mixtral-8x7b-32768",
		DisplayName:           "Mixtral 8x7B (Groq)",
		Description:           "Mistral Mixtral MoE model served by Groq",
		InputPricePer1MTokens: 0.24, OutputPricePer1MTokens: 0.24,
		ContextWindowTokens: 32_768,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	"groq/deepseek-r1-70b": {
		Key: "groq/deepseek-r1-70b", Provider: ProviderGroq, ModelID: "deepseek-r1-distill-llama-70b",
		DisplayName:           "DeepSeek R1 70B (Groq)",
		Description:           "DeepSeek R1 distilled reasoning model — fast on Groq",
		InputPricePer1MTokens: 0.75, OutputPricePer1MTokens: 0.99,
		ContextWindowTokens: 131_072,
		Capabilities:        []string{"reasoning"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	"groq/llama-4-scout": {
		Key: "groq/llama-4-scout", Provider: ProviderGroq, ModelID: "meta-llama/llama-4-scout-17b-16e-instruct",
		DisplayName:           "Llama 4 Scout 17B (Groq)",
		Description:           "Meta Llama 4 Scout multimodal model on Groq",
		InputPricePer1MTokens: 0.11, OutputPricePer1MTokens: 0.34,
		ContextWindowTokens: 131_072,
		Capabilities:        []string{"vision"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFieldsVision,
	},

	// ── Cohere ────────────────────────────────────────────────────────────────────

	"cohere/command-r-plus": {
		Key: "cohere/command-r-plus", Provider: ProviderCohere, ModelID: "command-r-plus",
		DisplayName:           "Command R+ (Cohere)",
		Description:           "Cohere's most capable model — RAG and complex reasoning",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.cohere.com",
		CredentialFields:    credsCohere,
		InputFields:         oaiFields,
	},
	"cohere/command-r": {
		Key: "cohere/command-r", Provider: ProviderCohere, ModelID: "command-r",
		DisplayName:           "Command R (Cohere)",
		Description:           "Balanced performance and cost for RAG workflows",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.cohere.com",
		CredentialFields:    credsCohere,
		InputFields:         oaiFields,
	},
	"cohere/command-r7b": {
		Key: "cohere/command-r7b", Provider: ProviderCohere, ModelID: "command-r7b-12-2024",
		DisplayName:           "Command R7B (Cohere)",
		Description:           "Compact, efficient model for low-latency tasks",
		InputPricePer1MTokens: 0.0375, OutputPricePer1MTokens: 0.15,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.cohere.com",
		CredentialFields:    credsCohere,
		InputFields:         oaiFields,
	},

	// ── AWS Bedrock ───────────────────────────────────────────────────────────────

	"bedrock/claude-3-5-sonnet": {
		Key: "bedrock/claude-3-5-sonnet", Provider: ProviderBedrock, ModelID: "anthropic.claude-3-5-sonnet-20241022-v2:0",
		DisplayName:           "Claude 3.5 Sonnet (Bedrock)",
		Description:           "Anthropic Claude 3.5 Sonnet via AWS Bedrock",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         anthropicFields,
	},
	"bedrock/claude-3-5-haiku": {
		Key: "bedrock/claude-3-5-haiku", Provider: ProviderBedrock, ModelID: "anthropic.claude-3-5-haiku-20241022-v1:0",
		DisplayName:           "Claude 3.5 Haiku (Bedrock)",
		Description:           "Fast, affordable Claude 3.5 Haiku via AWS Bedrock",
		InputPricePer1MTokens: 0.80, OutputPricePer1MTokens: 4.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         anthropicFields,
	},
	"bedrock/claude-3-opus": {
		Key: "bedrock/claude-3-opus", Provider: ProviderBedrock, ModelID: "anthropic.claude-3-opus-20240229-v1:0",
		DisplayName:           "Claude 3 Opus (Bedrock)",
		Description:           "Most capable Claude 3 via AWS Bedrock",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         anthropicFields,
	},
	"bedrock/llama-3-3-70b": {
		Key: "bedrock/llama-3-3-70b", Provider: ProviderBedrock, ModelID: "us.meta.llama3-3-70b-instruct-v1:0",
		DisplayName:           "Llama 3.3 70B (Bedrock)",
		Description:           "Meta Llama 3.3 70B Instruct via AWS Bedrock",
		InputPricePer1MTokens: 0.72, OutputPricePer1MTokens: 0.72,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         oaiFields,
	},
	"bedrock/nova-pro": {
		Key: "bedrock/nova-pro", Provider: ProviderBedrock, ModelID: "amazon.nova-pro-v1:0",
		DisplayName:           "Amazon Nova Pro (Bedrock)",
		Description:           "Amazon's most capable Nova model — multimodal",
		InputPricePer1MTokens: 0.80, OutputPricePer1MTokens: 3.20,
		ContextWindowTokens: 300_000,
		Capabilities:        []string{"vision"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         oaiFieldsVision,
	},
	"bedrock/nova-lite": {
		Key: "bedrock/nova-lite", Provider: ProviderBedrock, ModelID: "amazon.nova-lite-v1:0",
		DisplayName:           "Amazon Nova Lite (Bedrock)",
		Description:           "Fast, low-cost Amazon Nova model",
		InputPricePer1MTokens: 0.06, OutputPricePer1MTokens: 0.24,
		ContextWindowTokens: 300_000,
		Capabilities:        []string{"vision"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         oaiFieldsVision,
	},
}

// MatchesQuery reports whether this definition matches a lower-cased search term
// across key, displayName, modelId, provider, description, and capabilities.
func (d *ModelDefinition) MatchesQuery(q string) bool {
	fields := []string{d.Key, d.DisplayName, d.ModelID, string(d.Provider), d.Description}
	fields = append(fields, d.Capabilities...)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// DefinitionKeysMatchingQuery returns catalog keys whose definition matches the
// lower-cased query by any searchable field (displayName, modelId, key, etc.).
func DefinitionKeysMatchingQuery(query string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}
	var keys []string
	for key, def := range catalog {
		if def.MatchesQuery(query) {
			keys = append(keys, key)
		}
	}
	return keys
}

// FindModelDefinition returns the ModelDefinition for key, or (zero, false) if not found.
func FindModelDefinition(key string) (ModelDefinition, bool) {
	def, ok := catalog[key]
	return def, ok
}

// HasCapability reports whether the model definition includes the given capability string.
func (d *ModelDefinition) HasCapability(cap string) bool {
	for _, c := range d.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// ComputeCostUSD returns the estimated cost in USD for the given token counts.
// Returns 0 when pricing is unknown (either price field is 0).
func (d *ModelDefinition) ComputeCostUSD(inputTokens, outputTokens int64) float64 {
	if d.InputPricePer1MTokens == 0 && d.OutputPricePer1MTokens == 0 {
		return 0
	}
	return (float64(inputTokens)*d.InputPricePer1MTokens +
		float64(outputTokens)*d.OutputPricePer1MTokens) / 1_000_000
}

// AllDefinitions returns every catalog entry sorted alphabetically by key.
func AllDefinitions() []ModelDefinition {
	out := make([]ModelDefinition, 0, len(catalog))
	for _, d := range catalog {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// DefinitionKeysWithCapabilities returns the catalog keys of all definitions
// that include every capability in the requested set.
func DefinitionKeysWithCapabilities(capabilities []string) []string {
	var keys []string
	for key, def := range catalog {
		if def.hasAllCapabilities(capabilities) {
			keys = append(keys, key)
		}
	}
	return keys
}

func (d *ModelDefinition) hasAllCapabilities(caps []string) bool {
	for _, need := range caps {
		if !d.HasCapability(need) {
			return false
		}
	}
	return true
}
