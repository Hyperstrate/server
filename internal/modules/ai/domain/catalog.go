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
	InputPricePer1MTokens             float64 `json:"inputPricePer1MTokens"`
	CachedInputPricePer1MTokens       float64 `json:"cachedInputPricePer1MTokens,omitempty"`
	CacheWriteInputPricePer1MTokens   float64 `json:"cacheWriteInputPricePer1MTokens,omitempty"`
	CacheWrite1hInputPricePer1MTokens float64 `json:"cacheWrite1hInputPricePer1MTokens,omitempty"`
	OutputPricePer1MTokens            float64 `json:"outputPricePer1MTokens"`
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

// TokenUsage is the normalized token breakdown used for billing estimates.
// InputTokens is total input tokens across regular, cache-read, and cache-write
// buckets. CachedInputTokens are provider-side cache reads. CacheWriteInputTokens
// are default/5-minute cache writes, while CacheWrite1hInputTokens are Anthropic
// one-hour cache writes when the provider reports them separately.
type TokenUsage struct {
	InputTokens             int64
	CachedInputTokens       int64
	CacheWriteInputTokens   int64
	CacheWrite1hInputTokens int64
	OutputTokens            int64
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

const (
	openAIChatCatalogPrefix      = "chatgpt/"
	openAIEmbeddingCatalogPrefix = "openai/"
	azureOpenAICatalogPrefix     = "azure/"
	anthropicCatalogPrefix       = "anthropic/"
	geminiCatalogPrefix          = "gemini/"
	mistralCatalogPrefix         = "mistral/"
	ollamaCatalogPrefix          = "ollama/"
	vllmCatalogPrefix            = "vllm/"
	localAICatalogPrefix         = "localai/"
	klingCatalogPrefix           = "kling/"
	groqCatalogPrefix            = "groq/"
	cohereCatalogPrefix          = "cohere/"
	bedrockCatalogPrefix         = "bedrock/"
	customCatalogKey             = "custom"
)

const (
	openAIModelGPT55                 = "gpt-5.5"
	openAIModelGPT55Pro              = "gpt-5.5-pro"
	openAIModelGPT54                 = "gpt-5.4"
	openAIModelGPT54Mini             = "gpt-5.4-mini"
	openAIModelGPT54Nano             = "gpt-5.4-nano"
	openAIModelGPT54Pro              = "gpt-5.4-pro"
	openAIModelGPT52                 = "gpt-5.2"
	openAIModelGPT52Pro              = "gpt-5.2-pro"
	openAIModelGPT51                 = "gpt-5.1"
	openAIModelGPT5                  = "gpt-5"
	openAIModelGPT5Mini              = "gpt-5-mini"
	openAIModelGPT5Nano              = "gpt-5-nano"
	openAIModelGPT5Pro               = "gpt-5-pro"
	openAIModelGPT41                 = "gpt-4.1"
	openAIModelGPT41Mini             = "gpt-4.1-mini"
	openAIModelGPT41Nano             = "gpt-4.1-nano"
	openAIModelGPT4o                 = "gpt-4o"
	openAIModelGPT4oMini             = "gpt-4o-mini"
	openAIModelGPT4o20240513         = "gpt-4o-2024-05-13"
	openAIModelO4Mini                = "o4-mini"
	openAIModelO3                    = "o3"
	openAIModelO3Mini                = "o3-mini"
	openAIModelO3Pro                 = "o3-pro"
	openAIModelO1                    = "o1"
	openAIModelO1Mini                = "o1-mini"
	openAIModelO1Pro                 = "o1-pro"
	openAIModelGPT4Turbo20240409     = "gpt-4-turbo-2024-04-09"
	openAIModelGPT40125Preview       = "gpt-4-0125-preview"
	openAIModelGPT41106Preview       = "gpt-4-1106-preview"
	openAIModelGPT41106VisionPreview = "gpt-4-1106-vision-preview"
	openAIModelGPT40613              = "gpt-4-0613"
	openAIModelGPT40314              = "gpt-4-0314"
	openAIModelGPT432K               = "gpt-4-32k"
	openAIModelGPT35Turbo            = "gpt-3.5-turbo"
	openAIModelGPT35Turbo0125        = "gpt-3.5-turbo-0125"
	openAIModelGPT35Turbo1106        = "gpt-3.5-turbo-1106"
	openAIModelGPT35Turbo0613        = "gpt-3.5-turbo-0613"
	openAIModelGPT35Turbo0301        = "gpt-3.5-turbo-0301"
	openAIModelGPT35TurboInstruct    = "gpt-3.5-turbo-instruct"
	openAIModelGPT35Turbo16K0613     = "gpt-3.5-turbo-16k-0613"
	openAIModelDavinci002            = "davinci-002"
	openAIModelBabbage002            = "babbage-002"
	openAIModelTextEmbedding3Small   = "text-embedding-3-small"
	openAIModelTextEmbedding3Large   = "text-embedding-3-large"
	openAIModelTextEmbeddingAda002   = "text-embedding-ada-002"
)

func openAIChatCatalogKey(modelID string) string {
	return openAIChatCatalogPrefix + modelID
}

func openAIEmbeddingCatalogKey(modelID string) string {
	return openAIEmbeddingCatalogPrefix + modelID
}

func azureOpenAICatalogKey(modelID string) string {
	return azureOpenAICatalogPrefix + modelID
}

func anthropicCatalogKey(slug string) string { return anthropicCatalogPrefix + slug }
func geminiCatalogKey(modelID string) string { return geminiCatalogPrefix + modelID }
func mistralCatalogKey(slug string) string   { return mistralCatalogPrefix + slug }
func ollamaCatalogKey(slug string) string    { return ollamaCatalogPrefix + slug }
func vllmCatalogKey(slug string) string      { return vllmCatalogPrefix + slug }
func localAICatalogKey(slug string) string   { return localAICatalogPrefix + slug }
func klingCatalogKey(slug string) string     { return klingCatalogPrefix + slug }
func groqCatalogKey(slug string) string      { return groqCatalogPrefix + slug }
func cohereCatalogKey(modelID string) string { return cohereCatalogPrefix + modelID }
func bedrockCatalogKey(slug string) string   { return bedrockCatalogPrefix + slug }

var knownOpenAIChatModelIDs = map[string]struct{}{
	openAIModelGPT55:                 {},
	openAIModelGPT55Pro:              {},
	openAIModelGPT54:                 {},
	openAIModelGPT54Mini:             {},
	openAIModelGPT54Nano:             {},
	openAIModelGPT54Pro:              {},
	openAIModelGPT52:                 {},
	openAIModelGPT52Pro:              {},
	openAIModelGPT51:                 {},
	openAIModelGPT5:                  {},
	openAIModelGPT5Mini:              {},
	openAIModelGPT5Nano:              {},
	openAIModelGPT5Pro:               {},
	openAIModelGPT41:                 {},
	openAIModelGPT41Mini:             {},
	openAIModelGPT41Nano:             {},
	openAIModelGPT4o:                 {},
	openAIModelGPT4oMini:             {},
	openAIModelGPT4o20240513:         {},
	openAIModelO4Mini:                {},
	openAIModelO3:                    {},
	openAIModelO3Mini:                {},
	openAIModelO3Pro:                 {},
	openAIModelO1:                    {},
	openAIModelO1Mini:                {},
	openAIModelO1Pro:                 {},
	openAIModelGPT4Turbo20240409:     {},
	openAIModelGPT40125Preview:       {},
	openAIModelGPT41106Preview:       {},
	openAIModelGPT41106VisionPreview: {},
	openAIModelGPT40613:              {},
	openAIModelGPT40314:              {},
	openAIModelGPT432K:               {},
	openAIModelGPT35Turbo:            {},
	openAIModelGPT35Turbo0125:        {},
	openAIModelGPT35Turbo1106:        {},
	openAIModelGPT35Turbo0613:        {},
	openAIModelGPT35Turbo0301:        {},
	openAIModelGPT35TurboInstruct:    {},
	openAIModelGPT35Turbo16K0613:     {},
	openAIModelDavinci002:            {},
	openAIModelBabbage002:            {},
}

var knownOpenAIEmbeddingModelIDs = map[string]struct{}{
	openAIModelTextEmbedding3Small: {},
	openAIModelTextEmbedding3Large: {},
	openAIModelTextEmbeddingAda002: {},
}

const (
	anthropicModelClaudeOpus47   = "claude-opus-4-7"
	anthropicModelClaudeOpus46   = "claude-opus-4-6"
	anthropicModelClaudeOpus45   = "claude-opus-4-5"
	anthropicModelClaudeOpus41   = "claude-opus-4-1"
	anthropicModelClaudeOpus4    = "claude-opus-4-20250514"
	anthropicModelClaudeSonnet46 = "claude-sonnet-4-6"
	anthropicModelClaudeSonnet45 = "claude-sonnet-4-5"
	anthropicModelClaudeSonnet4  = "claude-sonnet-4-20250514"
	anthropicModelClaudeSonnet37 = "claude-sonnet-3-7-20250219"
	anthropicModelClaudeHaiku45  = "claude-haiku-4-5-20251001"
	anthropicModelClaudeHaiku35  = "claude-haiku-3-5-20241022"
	anthropicModelClaudeHaiku3   = "claude-3-haiku-20240307"
	anthropicModelClaudeOpus3    = "claude-3-opus-20240229"
	anthropicSlugClaudeOpus4     = "claude-opus-4"
	anthropicSlugClaudeSonnet4   = "claude-sonnet-4"
	anthropicSlugClaudeSonnet37  = "claude-sonnet-3-7"
	anthropicSlugClaudeHaiku45   = "claude-haiku-4-5"
	anthropicSlugClaudeHaiku35   = "claude-haiku-3-5"
	anthropicSlugClaudeHaiku3    = "claude-haiku-3"
	anthropicSlugClaudeOpus3     = "claude-opus-3"
)

const (
	geminiModel25Pro       = "gemini-2.5-pro"
	geminiModel25Flash     = "gemini-2.5-flash"
	geminiModel25FlashLite = "gemini-2.5-flash-lite"
	geminiModel20Flash     = "gemini-2.0-flash"
)

const (
	mistralModelLargeLatest      = "mistral-large-latest"
	mistralModelMedium3          = "mistral-medium-3"
	mistralModelSmallLatest      = "mistral-small-latest"
	mistralModelSmall31Latest    = "mistral-small-3.1-latest"
	mistralModelCodestralLatest  = "codestral-latest"
	mistralModelPixtralLarge     = "pixtral-large-latest"
	mistralModelMagistralMedium  = "magistral-medium-latest"
	mistralModelMinistral8B      = "ministral-8b-latest"
	mistralModelMinistral3B      = "ministral-3b-latest"
	mistralModelOpenMistralNemo  = "open-mistral-nemo"
	mistralModelOpenMixtral8x22B = "open-mixtral-8x22b"
	mistralModelOpenMixtral8x7B  = "open-mixtral-8x7b"
	mistralModelEmbed            = "mistral-embed"
	mistralSlugLarge             = "mistral-large"
	mistralSlugMedium3           = "mistral-medium-3"
	mistralSlugSmall             = "mistral-small"
	mistralSlugSmall31           = "mistral-small-3-1"
	mistralSlugCodestral         = "codestral"
	mistralSlugPixtralLarge      = "pixtral-large"
	mistralSlugMagistralMedium   = "magistral-medium"
	mistralSlugMinistral8B       = "ministral-8b"
	mistralSlugMinistral3B       = "ministral-3b"
	mistralSlugEmbed             = "mistral-embed"
)

const (
	ollamaModelLlama3370B  = "llama3.3:70b"
	ollamaModelMistralNemo = "mistral-nemo"
	ollamaModelNomicEmbed  = "nomic-embed-text"
	ollamaModelMxbaiEmbed  = "mxbai-embed-large"
	ollamaSlugLlama3370B   = "llama-3.3-70b"
	ollamaSlugMistralNemo  = "mistral-nemo"
	ollamaSlugNomicEmbed   = "nomic-embed-text"
	ollamaSlugMxbaiEmbed   = "mxbai-embed-large"
)

const (
	vllmModelLlama318B       = "meta-llama/Meta-Llama-3.1-8B-Instruct"
	vllmModelLlama3170B      = "meta-llama/Meta-Llama-3.1-70B-Instruct"
	vllmModelMistral7B       = "mistralai/Mistral-7B-Instruct-v0.3"
	vllmModelQwen2572B       = "Qwen/Qwen2.5-72B-Instruct"
	vllmModelDeepSeekCoderV2 = "deepseek-ai/DeepSeek-Coder-V2-Lite-Instruct"
	vllmSlugLlama318B        = "llama-3.1-8b-instruct"
	vllmSlugLlama3170B       = "llama-3.1-70b-instruct"
	vllmSlugMistral7B        = "mistral-7b-instruct"
	vllmSlugQwen2572B        = "qwen2.5-72b-instruct"
	vllmSlugDeepSeekCoderV2  = "deepseek-coder-v2-lite"
)

const (
	localAIModelLlama318B    = "llama-3.1-8b-instruct"
	localAIModelMistral7B    = "mistral-7b-instruct"
	localAIModelCodeLlama13B = "codellama-13b-instruct"
	localAIModelPhi3Medium   = "phi-3-medium-128k-instruct"
	localAISlugLlama318B     = "llama-3.1-8b-instruct"
	localAISlugMistral7B     = "mistral-7b"
	localAISlugCodeLlama13B  = "codellama-13b"
	localAISlugPhi3Medium    = "phi-3-medium"
)

const (
	klingModelV1             = "kling-v1"
	klingModelV2             = "kling-v2"
	klingModelV21            = "kling-v2.1"
	klingModelV3             = "kling-v3"
	klingSlugV1              = "kling-v1"
	klingSlugV1Image         = "kling-v1-image"
	klingSlugV2              = "kling-v2"
	klingSlugV21             = "kling-v2.1"
	klingSlugV3MotionControl = "kling-v3-motion-control"
)

const (
	groqModelLlama3370B    = "llama-3.3-70b-versatile"
	groqModelLlama318B     = "llama-3.1-8b-instant"
	groqModelMixtral8x7B   = "mixtral-8x7b-32768"
	groqModelDeepSeekR170B = "deepseek-r1-distill-llama-70b"
	groqModelLlama4Scout   = "meta-llama/llama-4-scout-17b-16e-instruct"
	groqSlugLlama3370B     = "llama-3.3-70b"
	groqSlugLlama318B      = "llama-3.1-8b"
	groqSlugMixtral8x7B    = "mixtral-8x7b"
	groqSlugDeepSeekR170B  = "deepseek-r1-70b"
	groqSlugLlama4Scout    = "llama-4-scout"
)

const (
	cohereModelCommandRPlus = "command-r-plus"
	cohereModelCommandR     = "command-r"
	cohereModelCommandR7B   = "command-r7b-12-2024"
	cohereSlugCommandR7B    = "command-r7b"
)

const (
	bedrockModelClaude35Sonnet = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	bedrockModelClaude35Haiku  = "anthropic.claude-3-5-haiku-20241022-v1:0"
	bedrockModelClaude3Opus    = "anthropic.claude-3-opus-20240229-v1:0"
	bedrockModelLlama3370B     = "us.meta.llama3-3-70b-instruct-v1:0"
	bedrockModelNovaPro        = "amazon.nova-pro-v1:0"
	bedrockModelNovaLite       = "amazon.nova-lite-v1:0"
	bedrockSlugClaude35Sonnet  = "claude-3-5-sonnet"
	bedrockSlugClaude35Haiku   = "claude-3-5-haiku"
	bedrockSlugClaude3Opus     = "claude-3-opus"
	bedrockSlugLlama3370B      = "llama-3-3-70b"
	bedrockSlugNovaPro         = "nova-pro"
	bedrockSlugNovaLite        = "nova-lite"
)

var knownCatalogModelIDsByProvider = map[Provider]map[string]struct{}{
	ProviderOpenAi:      mergeStringSets(knownOpenAIChatModelIDs, knownOpenAIEmbeddingModelIDs),
	ProviderAzureOpenAI: knownOpenAIChatModelIDs,
	ProviderAnthropic: {
		anthropicModelClaudeOpus47:   {},
		anthropicModelClaudeOpus46:   {},
		anthropicModelClaudeOpus45:   {},
		anthropicModelClaudeOpus41:   {},
		anthropicModelClaudeOpus4:    {},
		anthropicModelClaudeSonnet46: {},
		anthropicModelClaudeSonnet45: {},
		anthropicModelClaudeSonnet4:  {},
		anthropicModelClaudeSonnet37: {},
		anthropicModelClaudeHaiku45:  {},
		anthropicModelClaudeHaiku35:  {},
		anthropicModelClaudeHaiku3:   {},
		anthropicModelClaudeOpus3:    {},
	},
	ProviderGemini: {
		geminiModel25Pro:       {},
		geminiModel25Flash:     {},
		geminiModel25FlashLite: {},
		geminiModel20Flash:     {},
	},
	ProviderMistral: {
		mistralModelLargeLatest:      {},
		mistralModelMedium3:          {},
		mistralModelSmallLatest:      {},
		mistralModelSmall31Latest:    {},
		mistralModelCodestralLatest:  {},
		mistralModelPixtralLarge:     {},
		mistralModelMagistralMedium:  {},
		mistralModelMinistral8B:      {},
		mistralModelMinistral3B:      {},
		mistralModelOpenMistralNemo:  {},
		mistralModelOpenMixtral8x22B: {},
		mistralModelOpenMixtral8x7B:  {},
		mistralModelEmbed:            {},
	},
	ProviderOllama: {
		ollamaModelLlama3370B:  {},
		ollamaModelMistralNemo: {},
		ollamaModelNomicEmbed:  {},
		ollamaModelMxbaiEmbed:  {},
	},
	ProviderVLLM: {
		vllmModelLlama318B:       {},
		vllmModelLlama3170B:      {},
		vllmModelMistral7B:       {},
		vllmModelQwen2572B:       {},
		vllmModelDeepSeekCoderV2: {},
	},
	ProviderLocalAI: {
		localAIModelLlama318B:    {},
		localAIModelMistral7B:    {},
		localAIModelCodeLlama13B: {},
		localAIModelPhi3Medium:   {},
	},
	ProviderKling: {
		klingModelV1:  {},
		klingModelV2:  {},
		klingModelV21: {},
		klingModelV3:  {},
	},
	ProviderGroq: {
		groqModelLlama3370B:    {},
		groqModelLlama318B:     {},
		groqModelMixtral8x7B:   {},
		groqModelDeepSeekR170B: {},
		groqModelLlama4Scout:   {},
	},
	ProviderCohere: {
		cohereModelCommandRPlus: {},
		cohereModelCommandR:     {},
		cohereModelCommandR7B:   {},
	},
	ProviderBedrock: {
		bedrockModelClaude35Sonnet: {},
		bedrockModelClaude35Haiku:  {},
		bedrockModelClaude3Opus:    {},
		bedrockModelLlama3370B:     {},
		bedrockModelNovaPro:        {},
		bedrockModelNovaLite:       {},
	},
}

func mergeStringSets(sets ...map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for _, set := range sets {
		for key := range set {
			out[key] = struct{}{}
		}
	}
	return out
}

var openAICachedInputPricesPer1M = map[string]float64{
	openAIModelGPT55:     0.50,
	openAIModelGPT54:     0.25,
	openAIModelGPT54Mini: 0.075,
	openAIModelGPT54Nano: 0.02,
	openAIModelGPT52:     0.175,
	openAIModelGPT51:     0.125,
	openAIModelGPT5:      0.125,
	openAIModelGPT5Mini:  0.025,
	openAIModelGPT5Nano:  0.005,
	openAIModelGPT41:     0.50,
	openAIModelGPT41Mini: 0.10,
	openAIModelGPT41Nano: 0.025,
	openAIModelGPT4o:     1.25,
	openAIModelGPT4oMini: 0.075,
	openAIModelO4Mini:    0.275,
	openAIModelO3:        0.50,
	openAIModelO3Mini:    0.55,
	openAIModelO1:        7.50,
	openAIModelO1Mini:    0.55,
}

// catalog is the authoritative registry of all supported model definitions.
var catalog = map[string]ModelDefinition{

	// ── OpenAI — GPT-5.5 family ─────────────────────────────────────────────────

	openAIChatCatalogKey(openAIModelGPT55): {
		Key: openAIChatCatalogKey(openAIModelGPT55), Provider: ProviderOpenAi, ModelID: openAIModelGPT55,
		DisplayName:           "GPT-5.5",
		Description:           "OpenAI's most capable model — long context with vision",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT55Pro): {
		Key: openAIChatCatalogKey(openAIModelGPT55Pro), Provider: ProviderOpenAi, ModelID: openAIModelGPT55Pro,
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

	openAIChatCatalogKey(openAIModelGPT54): {
		Key: openAIChatCatalogKey(openAIModelGPT54), Provider: ProviderOpenAi, ModelID: openAIModelGPT54,
		DisplayName:           "GPT-5.4",
		Description:           "Best intelligence at scale for agentic, coding, and professional workflows — 1M context with vision",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT54Mini): {
		Key: openAIChatCatalogKey(openAIModelGPT54Mini), Provider: ProviderOpenAi, ModelID: openAIModelGPT54Mini,
		DisplayName:           "GPT-5.4 mini",
		Description:           "Strongest mini model for coding, computer use, and subagents — 400K context with vision",
		InputPricePer1MTokens: 0.75, OutputPricePer1MTokens: 4.50,
		ContextWindowTokens: 400_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT54Nano): {
		Key: openAIChatCatalogKey(openAIModelGPT54Nano), Provider: ProviderOpenAi, ModelID: openAIModelGPT54Nano,
		DisplayName:           "GPT-5.4 nano",
		Description:           "Cheapest GPT-5.4-class model for simple high-volume tasks — 400K context with vision",
		InputPricePer1MTokens: 0.20, OutputPricePer1MTokens: 1.25,
		ContextWindowTokens: 400_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT54Pro): {
		Key: openAIChatCatalogKey(openAIModelGPT54Pro), Provider: ProviderOpenAi, ModelID: openAIModelGPT54Pro,
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

	openAIChatCatalogKey(openAIModelGPT52): {
		Key: openAIChatCatalogKey(openAIModelGPT52), Provider: ProviderOpenAi, ModelID: openAIModelGPT52,
		DisplayName:           "GPT-5.2",
		InputPricePer1MTokens: 1.75, OutputPricePer1MTokens: 14.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT52Pro): {
		Key: openAIChatCatalogKey(openAIModelGPT52Pro), Provider: ProviderOpenAi, ModelID: openAIModelGPT52Pro,
		DisplayName:           "GPT-5.2 Pro",
		InputPricePer1MTokens: 21.00, OutputPricePer1MTokens: 168.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — GPT-5 family ───────────────────────────────────────────────────

	openAIChatCatalogKey(openAIModelGPT51): {
		Key: openAIChatCatalogKey(openAIModelGPT51), Provider: ProviderOpenAi, ModelID: openAIModelGPT51,
		DisplayName:           "GPT-5.1",
		InputPricePer1MTokens: 1.25, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT5): {
		Key: openAIChatCatalogKey(openAIModelGPT5), Provider: ProviderOpenAi, ModelID: openAIModelGPT5,
		DisplayName:           "GPT-5",
		InputPricePer1MTokens: 1.25, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT5Mini): {
		Key: openAIChatCatalogKey(openAIModelGPT5Mini), Provider: ProviderOpenAi, ModelID: openAIModelGPT5Mini,
		DisplayName:           "GPT-5 mini",
		InputPricePer1MTokens: 0.25, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT5Nano): {
		Key: openAIChatCatalogKey(openAIModelGPT5Nano), Provider: ProviderOpenAi, ModelID: openAIModelGPT5Nano,
		DisplayName:           "GPT-5 nano",
		InputPricePer1MTokens: 0.05, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT5Pro): {
		Key: openAIChatCatalogKey(openAIModelGPT5Pro), Provider: ProviderOpenAi, ModelID: openAIModelGPT5Pro,
		DisplayName:           "GPT-5 Pro",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 120.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},

	// ── OpenAI — GPT-4.1 family ─────────────────────────────────────────────────

	openAIChatCatalogKey(openAIModelGPT41): {
		Key: openAIChatCatalogKey(openAIModelGPT41), Provider: ProviderOpenAi, ModelID: openAIModelGPT41,
		DisplayName:           "GPT-4.1",
		Description:           "Strong instruction-following with vision support — 1M context",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 8.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT41Mini): {
		Key: openAIChatCatalogKey(openAIModelGPT41Mini), Provider: ProviderOpenAi, ModelID: openAIModelGPT41Mini,
		DisplayName:           "GPT-4.1 mini",
		Description:           "Compact and fast variant of GPT-4.1",
		InputPricePer1MTokens: 0.40, OutputPricePer1MTokens: 1.60,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT41Nano): {
		Key: openAIChatCatalogKey(openAIModelGPT41Nano), Provider: ProviderOpenAi, ModelID: openAIModelGPT41Nano,
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

	openAIChatCatalogKey(openAIModelGPT4o): {
		Key: openAIChatCatalogKey(openAIModelGPT4o), Provider: ProviderOpenAi, ModelID: openAIModelGPT4o,
		DisplayName:           "GPT-4o",
		Description:           "OpenAI's flagship multimodal model — text and vision",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT4oMini): {
		Key: openAIChatCatalogKey(openAIModelGPT4oMini), Provider: ProviderOpenAi, ModelID: openAIModelGPT4oMini,
		DisplayName:           "GPT-4o mini",
		Description:           "Fast and affordable model for lightweight tasks",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT4o20240513): {
		Key: openAIChatCatalogKey(openAIModelGPT4o20240513), Provider: ProviderOpenAi, ModelID: openAIModelGPT4o20240513,
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

	openAIChatCatalogKey(openAIModelO4Mini): {
		Key: openAIChatCatalogKey(openAIModelO4Mini), Provider: ProviderOpenAi, ModelID: openAIModelO4Mini,
		DisplayName:           "o4-mini",
		Description:           "Fast, cost-efficient reasoning model with vision support",
		InputPricePer1MTokens: 1.10, OutputPricePer1MTokens: 4.40,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelO3): {
		Key: openAIChatCatalogKey(openAIModelO3), Provider: ProviderOpenAi, ModelID: openAIModelO3,
		DisplayName:           "o3",
		Description:           "OpenAI's most capable reasoning model",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 8.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelO3Mini): {
		Key: openAIChatCatalogKey(openAIModelO3Mini), Provider: ProviderOpenAi, ModelID: openAIModelO3Mini,
		DisplayName:           "o3-mini",
		Description:           "Cost-efficient reasoning model balancing speed and intelligence",
		InputPricePer1MTokens: 1.10, OutputPricePer1MTokens: 4.40,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelO3Pro): {
		Key: openAIChatCatalogKey(openAIModelO3Pro), Provider: ProviderOpenAi, ModelID: openAIModelO3Pro,
		DisplayName:           "o3 Pro",
		Description:           "Maximum reasoning capability — highest accuracy on complex tasks",
		InputPricePer1MTokens: 20.00, OutputPricePer1MTokens: 80.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelO1): {
		Key: openAIChatCatalogKey(openAIModelO1), Provider: ProviderOpenAi, ModelID: openAIModelO1,
		DisplayName:           "o1",
		Description:           "OpenAI's first reasoning model — excels at complex math, science, and coding",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 60.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelO1Mini): {
		Key: openAIChatCatalogKey(openAIModelO1Mini), Provider: ProviderOpenAi, ModelID: openAIModelO1Mini,
		DisplayName:           "o1-mini",
		Description:           "Fast and affordable reasoning model optimised for STEM tasks",
		InputPricePer1MTokens: 1.10, OutputPricePer1MTokens: 4.40,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"reasoning"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelO1Pro): {
		Key: openAIChatCatalogKey(openAIModelO1Pro), Provider: ProviderOpenAi, ModelID: openAIModelO1Pro,
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

	openAIChatCatalogKey(openAIModelGPT4Turbo20240409): {
		Key: openAIChatCatalogKey(openAIModelGPT4Turbo20240409), Provider: ProviderOpenAi, ModelID: openAIModelGPT4Turbo20240409,
		DisplayName:           "GPT-4 Turbo (2024-04-09)",
		Description:           "GPT-4 Turbo with vision — 128K context",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT40125Preview): {
		Key: openAIChatCatalogKey(openAIModelGPT40125Preview), Provider: ProviderOpenAi, ModelID: openAIModelGPT40125Preview,
		DisplayName:           "GPT-4 Turbo Preview (0125)",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT41106Preview): {
		Key: openAIChatCatalogKey(openAIModelGPT41106Preview), Provider: ProviderOpenAi, ModelID: openAIModelGPT41106Preview,
		DisplayName:           "GPT-4 Turbo Preview (1106)",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT41106VisionPreview): {
		Key: openAIChatCatalogKey(openAIModelGPT41106VisionPreview), Provider: ProviderOpenAi, ModelID: openAIModelGPT41106VisionPreview,
		DisplayName:           "GPT-4 Turbo Vision Preview (1106)",
		InputPricePer1MTokens: 10.00, OutputPricePer1MTokens: 30.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFieldsVision,
	},
	openAIChatCatalogKey(openAIModelGPT40613): {
		Key: openAIChatCatalogKey(openAIModelGPT40613), Provider: ProviderOpenAi, ModelID: openAIModelGPT40613,
		DisplayName:           "GPT-4 (0613)",
		InputPricePer1MTokens: 30.00, OutputPricePer1MTokens: 60.00,
		ContextWindowTokens: 8_192,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT40314): {
		Key: openAIChatCatalogKey(openAIModelGPT40314), Provider: ProviderOpenAi, ModelID: openAIModelGPT40314,
		DisplayName:           "GPT-4 (0314)",
		InputPricePer1MTokens: 30.00, OutputPricePer1MTokens: 60.00,
		ContextWindowTokens: 8_192,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT432K): {
		Key: openAIChatCatalogKey(openAIModelGPT432K), Provider: ProviderOpenAi, ModelID: openAIModelGPT432K,
		DisplayName:           "GPT-4 32K",
		InputPricePer1MTokens: 60.00, OutputPricePer1MTokens: 120.00,
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},

	// ── OpenAI — GPT-3.5 / legacy ───────────────────────────────────────────────

	openAIChatCatalogKey(openAIModelGPT35Turbo): {
		Key: openAIChatCatalogKey(openAIModelGPT35Turbo), Provider: ProviderOpenAi, ModelID: openAIModelGPT35Turbo,
		DisplayName:           "GPT-3.5 Turbo",
		InputPricePer1MTokens: 0.50, OutputPricePer1MTokens: 1.50,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT35Turbo0125): {
		Key: openAIChatCatalogKey(openAIModelGPT35Turbo0125), Provider: ProviderOpenAi, ModelID: openAIModelGPT35Turbo0125,
		DisplayName:           "GPT-3.5 Turbo (0125)",
		InputPricePer1MTokens: 0.50, OutputPricePer1MTokens: 1.50,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT35Turbo1106): {
		Key: openAIChatCatalogKey(openAIModelGPT35Turbo1106), Provider: ProviderOpenAi, ModelID: openAIModelGPT35Turbo1106,
		DisplayName:           "GPT-3.5 Turbo (1106)",
		InputPricePer1MTokens: 1.00, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT35Turbo0613): {
		Key: openAIChatCatalogKey(openAIModelGPT35Turbo0613), Provider: ProviderOpenAi, ModelID: openAIModelGPT35Turbo0613,
		DisplayName:           "GPT-3.5 Turbo (0613)",
		InputPricePer1MTokens: 1.50, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 4_096,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT35Turbo0301): {
		Key: openAIChatCatalogKey(openAIModelGPT35Turbo0301), Provider: ProviderOpenAi, ModelID: openAIModelGPT35Turbo0301,
		DisplayName:           "GPT-3.5 Turbo (0301)",
		InputPricePer1MTokens: 1.50, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 4_096,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT35TurboInstruct): {
		Key: openAIChatCatalogKey(openAIModelGPT35TurboInstruct), Provider: ProviderOpenAi, ModelID: openAIModelGPT35TurboInstruct,
		DisplayName:           "GPT-3.5 Turbo Instruct",
		InputPricePer1MTokens: 1.50, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 4_096,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelGPT35Turbo16K0613): {
		Key: openAIChatCatalogKey(openAIModelGPT35Turbo16K0613), Provider: ProviderOpenAi, ModelID: openAIModelGPT35Turbo16K0613,
		DisplayName:           "GPT-3.5 Turbo 16K (0613)",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 4.00,
		ContextWindowTokens: 16_385,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelDavinci002): {
		Key: openAIChatCatalogKey(openAIModelDavinci002), Provider: ProviderOpenAi, ModelID: openAIModelDavinci002,
		DisplayName:           "davinci-002",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 16_384,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},
	openAIChatCatalogKey(openAIModelBabbage002): {
		Key: openAIChatCatalogKey(openAIModelBabbage002), Provider: ProviderOpenAi, ModelID: openAIModelBabbage002,
		DisplayName:           "babbage-002",
		InputPricePer1MTokens: 0.40, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 16_384,
		DefaultBaseURL:      "https://api.openai.com/v1",
		CredentialFields:    credsOpenAI,
		InputFields:         oaiFields,
	},

	// ── Anthropic — Claude Opus 4.x ─────────────────────────────────────────────

	anthropicCatalogKey(anthropicModelClaudeOpus47): {
		Key: anthropicCatalogKey(anthropicModelClaudeOpus47), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeOpus47,
		DisplayName:           "Claude Opus 4.7",
		Description:           "Anthropic's most capable model — best for complex reasoning and agentic coding",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 25.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicModelClaudeOpus46): {
		Key: anthropicCatalogKey(anthropicModelClaudeOpus46), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeOpus46,
		DisplayName:           "Claude Opus 4.6",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 25.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicModelClaudeOpus45): {
		Key: anthropicCatalogKey(anthropicModelClaudeOpus45), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeOpus45,
		DisplayName:           "Claude Opus 4.5",
		InputPricePer1MTokens: 5.00, OutputPricePer1MTokens: 25.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicModelClaudeOpus41): {
		Key: anthropicCatalogKey(anthropicModelClaudeOpus41), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeOpus41,
		DisplayName:           "Claude Opus 4.1",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicSlugClaudeOpus4): {
		Key: anthropicCatalogKey(anthropicSlugClaudeOpus4), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeOpus4,
		DisplayName:           "Claude Opus 4",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Anthropic — Claude Sonnet 4.x ───────────────────────────────────────────

	anthropicCatalogKey(anthropicModelClaudeSonnet46): {
		Key: anthropicCatalogKey(anthropicModelClaudeSonnet46), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeSonnet46,
		DisplayName:           "Claude Sonnet 4.6",
		Description:           "Best combination of speed and intelligence — extended thinking, 1M context",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicModelClaudeSonnet45): {
		Key: anthropicCatalogKey(anthropicModelClaudeSonnet45), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeSonnet45,
		DisplayName:           "Claude Sonnet 4.5",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicSlugClaudeSonnet4): {
		Key: anthropicCatalogKey(anthropicSlugClaudeSonnet4), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeSonnet4,
		DisplayName:           "Claude Sonnet 4",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicSlugClaudeSonnet37): {
		Key: anthropicCatalogKey(anthropicSlugClaudeSonnet37), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeSonnet37,
		DisplayName:           "Claude Sonnet 3.7 (deprecated)",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Anthropic — Claude Haiku 4.x / 3.x ─────────────────────────────────────

	anthropicCatalogKey(anthropicSlugClaudeHaiku45): {
		Key: anthropicCatalogKey(anthropicSlugClaudeHaiku45), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeHaiku45,
		DisplayName:           "Claude Haiku 4.5",
		Description:           "Fastest Claude model with near-frontier intelligence for lightweight tasks",
		InputPricePer1MTokens: 1.00, OutputPricePer1MTokens: 5.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicSlugClaudeHaiku35): {
		Key: anthropicCatalogKey(anthropicSlugClaudeHaiku35), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeHaiku35,
		DisplayName:           "Claude Haiku 3.5",
		InputPricePer1MTokens: 0.80, OutputPricePer1MTokens: 4.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},
	anthropicCatalogKey(anthropicSlugClaudeHaiku3): {
		Key: anthropicCatalogKey(anthropicSlugClaudeHaiku3), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeHaiku3,
		DisplayName:           "Claude Haiku 3",
		InputPricePer1MTokens: 0.25, OutputPricePer1MTokens: 1.25,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Anthropic — Claude Opus 3 (deprecated) ──────────────────────────────────

	anthropicCatalogKey(anthropicSlugClaudeOpus3): {
		Key: anthropicCatalogKey(anthropicSlugClaudeOpus3), Provider: ProviderAnthropic, ModelID: anthropicModelClaudeOpus3,
		DisplayName:           "Claude Opus 3 (deprecated)",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://api.anthropic.com/v1",
		CredentialFields:    credsAnthropic,
		InputFields:         anthropicFields,
	},

	// ── Gemini ──────────────────────────────────────────────────────────────────

	geminiCatalogKey(geminiModel25Pro): {
		Key: geminiCatalogKey(geminiModel25Pro), Provider: ProviderGemini, ModelID: geminiModel25Pro,
		DisplayName:           "Gemini 2.5 Pro",
		Description:           "Google's most capable Gemini model with deep reasoning — 1M context",
		InputPricePer1MTokens: 1.25, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		CredentialFields:    credsGemini,
		InputFields:         geminiFields,
	},
	geminiCatalogKey(geminiModel25Flash): {
		Key: geminiCatalogKey(geminiModel25Flash), Provider: ProviderGemini, ModelID: geminiModel25Flash,
		DisplayName:           "Gemini 2.5 Flash",
		Description:           "Google's fast and affordable reasoning model — 1M context with vision",
		InputPricePer1MTokens: 0.30, OutputPricePer1MTokens: 2.50,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling", "reasoning"},
		DefaultBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		CredentialFields:    credsGemini,
		InputFields:         geminiFields,
	},
	geminiCatalogKey(geminiModel25FlashLite): {
		Key: geminiCatalogKey(geminiModel25FlashLite), Provider: ProviderGemini, ModelID: geminiModel25FlashLite,
		DisplayName:           "Gemini 2.5 Flash-Lite",
		Description:           "Most cost-efficient Gemini model — optimised for high-volume tasks",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.40,
		ContextWindowTokens: 1_000_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		CredentialFields:    credsGemini,
		InputFields:         geminiFields,
	},
	geminiCatalogKey(geminiModel20Flash): {
		Key: geminiCatalogKey(geminiModel20Flash), Provider: ProviderGemini, ModelID: geminiModel20Flash,
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

	mistralCatalogKey(mistralSlugLarge): {
		Key: mistralCatalogKey(mistralSlugLarge), Provider: ProviderMistral, ModelID: mistralModelLargeLatest,
		DisplayName:           "Mistral Large",
		Description:           "Mistral's flagship model for complex reasoning and tasks",
		InputPricePer1MTokens: 0.50, OutputPricePer1MTokens: 1.50,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralSlugMedium3): {
		Key: mistralCatalogKey(mistralSlugMedium3), Provider: ProviderMistral, ModelID: mistralModelMedium3,
		DisplayName:           "Mistral Medium 3",
		Description:           "Strong mid-tier model balancing capability and cost",
		InputPricePer1MTokens: 0.40, OutputPricePer1MTokens: 2.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralSlugSmall): {
		Key: mistralCatalogKey(mistralSlugSmall), Provider: ProviderMistral, ModelID: mistralModelSmallLatest,
		DisplayName:           "Mistral Small",
		Description:           "Fast and affordable Mistral model for everyday tasks",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 32_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralSlugSmall31): {
		Key: mistralCatalogKey(mistralSlugSmall31), Provider: ProviderMistral, ModelID: mistralModelSmall31Latest,
		DisplayName:           "Mistral Small 3.1",
		Description:           "Upgraded small model with improved instruction-following",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.30,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralSlugCodestral): {
		Key: mistralCatalogKey(mistralSlugCodestral), Provider: ProviderMistral, ModelID: mistralModelCodestralLatest,
		DisplayName:           "Codestral",
		Description:           "Mistral's code-specialised model — fill-in-the-middle and completion",
		InputPricePer1MTokens: 0.30, OutputPricePer1MTokens: 0.90,
		ContextWindowTokens: 256_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralSlugPixtralLarge): {
		Key: mistralCatalogKey(mistralSlugPixtralLarge), Provider: ProviderMistral, ModelID: mistralModelPixtralLarge,
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
	mistralCatalogKey(mistralSlugMagistralMedium): {
		Key: mistralCatalogKey(mistralSlugMagistralMedium), Provider: ProviderMistral, ModelID: mistralModelMagistralMedium,
		DisplayName:           "Magistral Medium",
		Description:           "Mistral's reasoning-focused model",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 5.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"reasoning", "function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralSlugMinistral8B): {
		Key: mistralCatalogKey(mistralSlugMinistral8B), Provider: ProviderMistral, ModelID: mistralModelMinistral8B,
		DisplayName:           "Ministral 8B",
		Description:           "Compact 8B model optimised for edge and on-device use",
		InputPricePer1MTokens: 0.10, OutputPricePer1MTokens: 0.10,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralSlugMinistral3B): {
		Key: mistralCatalogKey(mistralSlugMinistral3B), Provider: ProviderMistral, ModelID: mistralModelMinistral3B,
		DisplayName:           "Ministral 3B",
		Description:           "Smallest Mistral model — ultra-low-cost inference",
		InputPricePer1MTokens: 0.04, OutputPricePer1MTokens: 0.04,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralModelOpenMistralNemo): {
		Key: mistralCatalogKey(mistralModelOpenMistralNemo), Provider: ProviderMistral, ModelID: mistralModelOpenMistralNemo,
		DisplayName:           "Mistral Nemo",
		Description:           "Open 12B model — best-in-class for its size",
		InputPricePer1MTokens: 0.01, OutputPricePer1MTokens: 0.03,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralModelOpenMixtral8x22B): {
		Key: mistralCatalogKey(mistralModelOpenMixtral8x22B), Provider: ProviderMistral, ModelID: mistralModelOpenMixtral8x22B,
		DisplayName:           "Mixtral 8x22B",
		Description:           "Open mixture-of-experts model — strong multilingual and reasoning performance",
		InputPricePer1MTokens: 1.20, OutputPricePer1MTokens: 1.20,
		ContextWindowTokens: 65_536,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},
	mistralCatalogKey(mistralModelOpenMixtral8x7B): {
		Key: mistralCatalogKey(mistralModelOpenMixtral8x7B), Provider: ProviderMistral, ModelID: mistralModelOpenMixtral8x7B,
		DisplayName:           "Mixtral 8x7B",
		Description:           "Open mixture-of-experts model — fast and capable for most tasks",
		InputPricePer1MTokens: 0.14, OutputPricePer1MTokens: 0.42,
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "https://api.mistral.ai/v1",
		CredentialFields:    credsMistral,
		InputFields:         mistralFields,
	},

	// ── Ollama (self-hosted) ────────────────────────────────────────────────────

	ollamaCatalogKey(ollamaSlugLlama3370B): {
		Key: ollamaCatalogKey(ollamaSlugLlama3370B), Provider: ProviderOllama, ModelID: ollamaModelLlama3370B,
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
	ollamaCatalogKey(ollamaSlugMistralNemo): {
		Key: ollamaCatalogKey(ollamaSlugMistralNemo), Provider: ProviderOllama, ModelID: ollamaModelMistralNemo,
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

	vllmCatalogKey(vllmSlugLlama318B): {
		Key: vllmCatalogKey(vllmSlugLlama318B), Provider: ProviderVLLM, ModelID: vllmModelLlama318B,
		DisplayName:         "Llama 3.1 8B Instruct (vLLM)",
		Description:         "Meta's Llama 3.1 8B instruction-tuned model — fast self-hosted via vLLM",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFieldsVision,
	},
	vllmCatalogKey(vllmSlugLlama3170B): {
		Key: vllmCatalogKey(vllmSlugLlama3170B), Provider: ProviderVLLM, ModelID: vllmModelLlama3170B,
		DisplayName:         "Llama 3.1 70B Instruct (vLLM)",
		Description:         "Meta's Llama 3.1 70B instruction-tuned model — self-hosted via vLLM",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},
	vllmCatalogKey(vllmSlugMistral7B): {
		Key: vllmCatalogKey(vllmSlugMistral7B), Provider: ProviderVLLM, ModelID: vllmModelMistral7B,
		DisplayName:         "Mistral 7B Instruct (vLLM)",
		Description:         "Mistral 7B instruction-tuned — lightweight and fast on commodity GPUs",
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},
	vllmCatalogKey(vllmSlugQwen2572B): {
		Key: vllmCatalogKey(vllmSlugQwen2572B), Provider: ProviderVLLM, ModelID: vllmModelQwen2572B,
		DisplayName:         "Qwen 2.5 72B Instruct (vLLM)",
		Description:         "Alibaba's Qwen 2.5 72B — strong multilingual and coding capabilities",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},
	vllmCatalogKey(vllmSlugDeepSeekCoderV2): {
		Key: vllmCatalogKey(vllmSlugDeepSeekCoderV2), Provider: ProviderVLLM, ModelID: vllmModelDeepSeekCoderV2,
		DisplayName:         "DeepSeek Coder V2 Lite (vLLM)",
		Description:         "Efficient self-hosted coding model — 16B MoE via vLLM",
		Capabilities:        []string{"function_calling"},
		ContextWindowTokens: 163_840,
		DefaultBaseURL:      "http://localhost:8000/v1",
		CredentialFields:    credsVLLM,
		InputFields:         oaiFields,
	},

	// ── LocalAI (self-hosted, OpenAI-compatible) ─────────────────────────────

	localAICatalogKey(localAISlugLlama318B): {
		Key: localAICatalogKey(localAISlugLlama318B), Provider: ProviderLocalAI, ModelID: localAIModelLlama318B,
		DisplayName:         "Llama 3.1 8B Instruct (LocalAI)",
		Description:         "Meta's Llama 3.1 8B via LocalAI — runs on CPU or GPU without CUDA required",
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},
	localAICatalogKey(localAISlugMistral7B): {
		Key: localAICatalogKey(localAISlugMistral7B), Provider: ProviderLocalAI, ModelID: localAIModelMistral7B,
		DisplayName:         "Mistral 7B (LocalAI)",
		Description:         "Mistral 7B instruction model via LocalAI",
		ContextWindowTokens: 32_768,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},
	localAICatalogKey(localAISlugCodeLlama13B): {
		Key: localAICatalogKey(localAISlugCodeLlama13B), Provider: ProviderLocalAI, ModelID: localAIModelCodeLlama13B,
		DisplayName:         "CodeLlama 13B (LocalAI)",
		Description:         "Meta's CodeLlama 13B — code generation and completion via LocalAI",
		ContextWindowTokens: 16_384,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},
	localAICatalogKey(localAISlugPhi3Medium): {
		Key: localAICatalogKey(localAISlugPhi3Medium), Provider: ProviderLocalAI, ModelID: localAIModelPhi3Medium,
		DisplayName:         "Phi-3 Medium (LocalAI)",
		Description:         "Microsoft's Phi-3 Medium — efficient reasoning model with 128K context via LocalAI",
		ContextWindowTokens: 131_072,
		DefaultBaseURL:      "http://localhost:8080/v1",
		CredentialFields:    credsLocalAI,
		InputFields:         oaiFields,
	},

	// ── Kling ───────────────────────────────────────────────────────────────────

	klingCatalogKey(klingSlugV1): {
		Key: klingCatalogKey(klingSlugV1), Provider: ProviderKling, ModelID: klingModelV1,
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
	klingCatalogKey(klingSlugV1Image): {
		Key: klingCatalogKey(klingSlugV1Image), Provider: ProviderKling, ModelID: klingModelV1,
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
	klingCatalogKey(klingSlugV2): {
		Key: klingCatalogKey(klingSlugV2), Provider: ProviderKling, ModelID: klingModelV2,
		DisplayName:      "Kling v2",
		Description:      "Enhanced text-to-video generation",
		Capabilities:     []string{"video_generation"},
		DefaultBaseURL:   "https://api.klingai.com/v1",
		CredentialFields: credsKling,
		InputFields: []InputFieldDef{
			{Key: "prompt", Label: "Prompt", Type: InputTypeText, Required: true, MaxLength: 2500},
		},
	},
	klingCatalogKey(klingSlugV21): {
		Key: klingCatalogKey(klingSlugV21), Provider: ProviderKling, ModelID: klingModelV21,
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
	klingCatalogKey(klingSlugV3MotionControl): {
		Key: klingCatalogKey(klingSlugV3MotionControl), Provider: ProviderKling, ModelID: klingModelV3,
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

	customCatalogKey: {
		Key: customCatalogKey, Provider: ProviderCustom, ModelID: "",
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

	openAIEmbeddingCatalogKey(openAIModelTextEmbedding3Small): {
		Key: openAIEmbeddingCatalogKey(openAIModelTextEmbedding3Small), Provider: ProviderOpenAi, ModelID: openAIModelTextEmbedding3Small,
		DisplayName:           "OpenAI Text Embedding 3 Small",
		Description:           "Fast, cost-effective embedding model for semantic search and routing",
		InputPricePer1MTokens: 0.02,
		ContextWindowTokens:   8_191,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.openai.com/v1",
		CredentialFields:      credsOpenAI,
		InputFields:           []InputFieldDef{},
	},
	openAIEmbeddingCatalogKey(openAIModelTextEmbedding3Large): {
		Key: openAIEmbeddingCatalogKey(openAIModelTextEmbedding3Large), Provider: ProviderOpenAi, ModelID: openAIModelTextEmbedding3Large,
		DisplayName:           "OpenAI Text Embedding 3 Large",
		Description:           "High-accuracy embedding model for demanding semantic applications",
		InputPricePer1MTokens: 0.13,
		ContextWindowTokens:   8_191,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.openai.com/v1",
		CredentialFields:      credsOpenAI,
		InputFields:           []InputFieldDef{},
	},
	openAIEmbeddingCatalogKey(openAIModelTextEmbeddingAda002): {
		Key: openAIEmbeddingCatalogKey(openAIModelTextEmbeddingAda002), Provider: ProviderOpenAi, ModelID: openAIModelTextEmbeddingAda002,
		DisplayName:           "OpenAI Text Embedding Ada 002",
		Description:           "Legacy OpenAI embedding model — use text-embedding-3-small for new projects",
		InputPricePer1MTokens: 0.10,
		ContextWindowTokens:   8_191,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.openai.com/v1",
		CredentialFields:      credsOpenAI,
		InputFields:           []InputFieldDef{},
	},
	mistralCatalogKey(mistralSlugEmbed): {
		Key: mistralCatalogKey(mistralSlugEmbed), Provider: ProviderMistral, ModelID: mistralModelEmbed,
		DisplayName:           "Mistral Embed",
		Description:           "Mistral's embedding model for semantic search and classification",
		InputPricePer1MTokens: 0.10,
		ContextWindowTokens:   8_192,
		Capabilities:          []string{"embedding"},
		DefaultBaseURL:        "https://api.mistral.ai/v1",
		CredentialFields:      credsMistral,
		InputFields:           []InputFieldDef{},
	},
	ollamaCatalogKey(ollamaSlugNomicEmbed): {
		Key: ollamaCatalogKey(ollamaSlugNomicEmbed), Provider: ProviderOllama, ModelID: ollamaModelNomicEmbed,
		DisplayName:      "Nomic Embed Text (Ollama)",
		Description:      "Local open-source embedding model for semantic search and routing",
		Capabilities:     []string{"embedding"},
		CredentialFields: credsOllama,
		InputFields:      []InputFieldDef{},
	},
	ollamaCatalogKey(ollamaSlugMxbaiEmbed): {
		Key: ollamaCatalogKey(ollamaSlugMxbaiEmbed), Provider: ProviderOllama, ModelID: ollamaModelMxbaiEmbed,
		DisplayName:      "MixedBread Embed Large (Ollama)",
		Description:      "High-performance local embedding model via Ollama",
		Capabilities:     []string{"embedding"},
		CredentialFields: credsOllama,
		InputFields:      []InputFieldDef{},
	},
	// ── Azure OpenAI ──────────────────────────────────────────────────────────────

	azureOpenAICatalogKey(openAIModelGPT4o): {
		Key: azureOpenAICatalogKey(openAIModelGPT4o), Provider: ProviderAzureOpenAI, ModelID: openAIModelGPT4o,
		DisplayName:           "GPT-4o (Azure)",
		Description:           "OpenAI GPT-4o hosted on Azure OpenAI Service",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://<resource>.openai.azure.com/openai/deployments/<deployment>",
		CredentialFields:    credsAzureOpenAI,
		InputFields:         oaiFieldsVision,
	},
	azureOpenAICatalogKey(openAIModelGPT4oMini): {
		Key: azureOpenAICatalogKey(openAIModelGPT4oMini), Provider: ProviderAzureOpenAI, ModelID: openAIModelGPT4oMini,
		DisplayName:           "GPT-4o Mini (Azure)",
		Description:           "Fast and affordable GPT-4o Mini on Azure",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://<resource>.openai.azure.com/openai/deployments/<deployment>",
		CredentialFields:    credsAzureOpenAI,
		InputFields:         oaiFieldsVision,
	},
	azureOpenAICatalogKey(openAIModelGPT41): {
		Key: azureOpenAICatalogKey(openAIModelGPT41), Provider: ProviderAzureOpenAI, ModelID: openAIModelGPT41,
		DisplayName:           "GPT-4.1 (Azure)",
		Description:           "OpenAI GPT-4.1 hosted on Azure OpenAI Service",
		InputPricePer1MTokens: 2.00, OutputPricePer1MTokens: 8.00,
		ContextWindowTokens: 1_047_576,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://<resource>.openai.azure.com/openai/deployments/<deployment>",
		CredentialFields:    credsAzureOpenAI,
		InputFields:         oaiFieldsVision,
	},
	azureOpenAICatalogKey(openAIModelO3Mini): {
		Key: azureOpenAICatalogKey(openAIModelO3Mini), Provider: ProviderAzureOpenAI, ModelID: openAIModelO3Mini,
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

	groqCatalogKey(groqSlugLlama3370B): {
		Key: groqCatalogKey(groqSlugLlama3370B), Provider: ProviderGroq, ModelID: groqModelLlama3370B,
		DisplayName:           "Llama 3.3 70B (Groq)",
		Description:           "Meta Llama 3.3 70B via Groq — ultra-fast inference",
		InputPricePer1MTokens: 0.59, OutputPricePer1MTokens: 0.79,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	groqCatalogKey(groqSlugLlama318B): {
		Key: groqCatalogKey(groqSlugLlama318B), Provider: ProviderGroq, ModelID: groqModelLlama318B,
		DisplayName:           "Llama 3.1 8B Instant (Groq)",
		Description:           "Fastest small model on Groq — ideal for high-throughput tasks",
		InputPricePer1MTokens: 0.05, OutputPricePer1MTokens: 0.08,
		ContextWindowTokens: 131_072,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	groqCatalogKey(groqSlugMixtral8x7B): {
		Key: groqCatalogKey(groqSlugMixtral8x7B), Provider: ProviderGroq, ModelID: groqModelMixtral8x7B,
		DisplayName:           "Mixtral 8x7B (Groq)",
		Description:           "Mistral Mixtral MoE model served by Groq",
		InputPricePer1MTokens: 0.24, OutputPricePer1MTokens: 0.24,
		ContextWindowTokens: 32_768,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	groqCatalogKey(groqSlugDeepSeekR170B): {
		Key: groqCatalogKey(groqSlugDeepSeekR170B), Provider: ProviderGroq, ModelID: groqModelDeepSeekR170B,
		DisplayName:           "DeepSeek R1 70B (Groq)",
		Description:           "DeepSeek R1 distilled reasoning model — fast on Groq",
		InputPricePer1MTokens: 0.75, OutputPricePer1MTokens: 0.99,
		ContextWindowTokens: 131_072,
		Capabilities:        []string{"reasoning"},
		DefaultBaseURL:      "https://api.groq.com/openai/v1",
		CredentialFields:    credsGroq,
		InputFields:         oaiFields,
	},
	groqCatalogKey(groqSlugLlama4Scout): {
		Key: groqCatalogKey(groqSlugLlama4Scout), Provider: ProviderGroq, ModelID: groqModelLlama4Scout,
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

	cohereCatalogKey(cohereModelCommandRPlus): {
		Key: cohereCatalogKey(cohereModelCommandRPlus), Provider: ProviderCohere, ModelID: cohereModelCommandRPlus,
		DisplayName:           "Command R+ (Cohere)",
		Description:           "Cohere's most capable model — RAG and complex reasoning",
		InputPricePer1MTokens: 2.50, OutputPricePer1MTokens: 10.00,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.cohere.com",
		CredentialFields:    credsCohere,
		InputFields:         oaiFields,
	},
	cohereCatalogKey(cohereModelCommandR): {
		Key: cohereCatalogKey(cohereModelCommandR), Provider: ProviderCohere, ModelID: cohereModelCommandR,
		DisplayName:           "Command R (Cohere)",
		Description:           "Balanced performance and cost for RAG workflows",
		InputPricePer1MTokens: 0.15, OutputPricePer1MTokens: 0.60,
		ContextWindowTokens: 128_000,
		Capabilities:        []string{"function_calling"},
		DefaultBaseURL:      "https://api.cohere.com",
		CredentialFields:    credsCohere,
		InputFields:         oaiFields,
	},
	cohereCatalogKey(cohereSlugCommandR7B): {
		Key: cohereCatalogKey(cohereSlugCommandR7B), Provider: ProviderCohere, ModelID: cohereModelCommandR7B,
		DisplayName:           "Command R7B (Cohere)",
		Description:           "Compact, efficient model for low-latency tasks",
		InputPricePer1MTokens: 0.0375, OutputPricePer1MTokens: 0.15,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://api.cohere.com",
		CredentialFields:    credsCohere,
		InputFields:         oaiFields,
	},

	// ── AWS Bedrock ───────────────────────────────────────────────────────────────

	bedrockCatalogKey(bedrockSlugClaude35Sonnet): {
		Key: bedrockCatalogKey(bedrockSlugClaude35Sonnet), Provider: ProviderBedrock, ModelID: bedrockModelClaude35Sonnet,
		DisplayName:           "Claude 3.5 Sonnet (Bedrock)",
		Description:           "Anthropic Claude 3.5 Sonnet via AWS Bedrock",
		InputPricePer1MTokens: 3.00, OutputPricePer1MTokens: 15.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         anthropicFields,
	},
	bedrockCatalogKey(bedrockSlugClaude35Haiku): {
		Key: bedrockCatalogKey(bedrockSlugClaude35Haiku), Provider: ProviderBedrock, ModelID: bedrockModelClaude35Haiku,
		DisplayName:           "Claude 3.5 Haiku (Bedrock)",
		Description:           "Fast, affordable Claude 3.5 Haiku via AWS Bedrock",
		InputPricePer1MTokens: 0.80, OutputPricePer1MTokens: 4.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         anthropicFields,
	},
	bedrockCatalogKey(bedrockSlugClaude3Opus): {
		Key: bedrockCatalogKey(bedrockSlugClaude3Opus), Provider: ProviderBedrock, ModelID: bedrockModelClaude3Opus,
		DisplayName:           "Claude 3 Opus (Bedrock)",
		Description:           "Most capable Claude 3 via AWS Bedrock",
		InputPricePer1MTokens: 15.00, OutputPricePer1MTokens: 75.00,
		ContextWindowTokens: 200_000,
		Capabilities:        []string{"vision", "function_calling"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         anthropicFields,
	},
	bedrockCatalogKey(bedrockSlugLlama3370B): {
		Key: bedrockCatalogKey(bedrockSlugLlama3370B), Provider: ProviderBedrock, ModelID: bedrockModelLlama3370B,
		DisplayName:           "Llama 3.3 70B (Bedrock)",
		Description:           "Meta Llama 3.3 70B Instruct via AWS Bedrock",
		InputPricePer1MTokens: 0.72, OutputPricePer1MTokens: 0.72,
		ContextWindowTokens: 128_000,
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         oaiFields,
	},
	bedrockCatalogKey(bedrockSlugNovaPro): {
		Key: bedrockCatalogKey(bedrockSlugNovaPro), Provider: ProviderBedrock, ModelID: bedrockModelNovaPro,
		DisplayName:           "Amazon Nova Pro (Bedrock)",
		Description:           "Amazon's most capable Nova model — multimodal",
		InputPricePer1MTokens: 0.80, OutputPricePer1MTokens: 3.20,
		ContextWindowTokens: 300_000,
		Capabilities:        []string{"vision"},
		DefaultBaseURL:      "https://bedrock-runtime.us-east-1.amazonaws.com",
		CredentialFields:    credsBedrock,
		InputFields:         oaiFieldsVision,
	},
	bedrockCatalogKey(bedrockSlugNovaLite): {
		Key: bedrockCatalogKey(bedrockSlugNovaLite), Provider: ProviderBedrock, ModelID: bedrockModelNovaLite,
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
	if !ok {
		return ModelDefinition{}, false
	}
	return def.withPromptCachePricing(), true
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

func (d ModelDefinition) withPromptCachePricing() ModelDefinition {
	if d.InputPricePer1MTokens == 0 {
		return d
	}
	switch d.Provider {
	case ProviderOpenAi, ProviderAzureOpenAI:
		if d.CachedInputPricePer1MTokens == 0 {
			if price, ok := openAICachedInputPricesPer1M[d.ModelID]; ok {
				d.CachedInputPricePer1MTokens = price
			}
		}
	case ProviderAnthropic:
		if d.CachedInputPricePer1MTokens == 0 {
			d.CachedInputPricePer1MTokens = d.InputPricePer1MTokens * 0.10
		}
		if d.CacheWriteInputPricePer1MTokens == 0 {
			d.CacheWriteInputPricePer1MTokens = d.InputPricePer1MTokens * 1.25
		}
		if d.CacheWrite1hInputPricePer1MTokens == 0 {
			d.CacheWrite1hInputPricePer1MTokens = d.InputPricePer1MTokens * 2.00
		}
	}
	return d
}

// ComputeCostUSD returns the estimated cost in USD for the given total input and output token counts.
func (d *ModelDefinition) ComputeCostUSD(inputTokens, outputTokens int64) float64 {
	return d.ComputeUsageCostUSD(TokenUsage{InputTokens: inputTokens, OutputTokens: outputTokens})
}

// ComputeUsageCostUSD returns the estimated cost in USD for a normalized token breakdown.
func (d *ModelDefinition) ComputeUsageCostUSD(usage TokenUsage) float64 {
	if d.InputPricePer1MTokens == 0 && d.OutputPricePer1MTokens == 0 {
		return 0
	}
	def := d.withPromptCachePricing()
	cachedInputPrice := def.CachedInputPricePer1MTokens
	if cachedInputPrice == 0 {
		cachedInputPrice = def.InputPricePer1MTokens
	}
	cacheWritePrice := def.CacheWriteInputPricePer1MTokens
	if cacheWritePrice == 0 {
		cacheWritePrice = def.InputPricePer1MTokens
	}
	cacheWrite1hPrice := def.CacheWrite1hInputPricePer1MTokens
	if cacheWrite1hPrice == 0 {
		cacheWrite1hPrice = cacheWritePrice
	}

	regularInputTokens := usage.InputTokens - usage.CachedInputTokens - usage.CacheWriteInputTokens - usage.CacheWrite1hInputTokens
	if regularInputTokens < 0 {
		regularInputTokens = 0
	}

	return (float64(regularInputTokens)*def.InputPricePer1MTokens +
		float64(usage.CachedInputTokens)*cachedInputPrice +
		float64(usage.CacheWriteInputTokens)*cacheWritePrice +
		float64(usage.CacheWrite1hInputTokens)*cacheWrite1hPrice +
		float64(usage.OutputTokens)*def.OutputPricePer1MTokens) / 1_000_000
}

// AllDefinitions returns every catalog entry sorted alphabetically by key.
func AllDefinitions() []ModelDefinition {
	out := make([]ModelDefinition, 0, len(catalog))
	for _, d := range catalog {
		out = append(out, d.withPromptCachePricing())
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
