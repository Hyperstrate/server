package domain

// Provider identifies the API backend a model registration targets.
// CUSTOM is the first value and covers any self-hosted or third-party endpoint.
type Provider string

const (
	ProviderCustom    Provider = "CUSTOM"
	ProviderOpenAi   Provider = "OPENAI"
	ProviderAnthropic Provider = "ANTHROPIC"
	ProviderMistral   Provider = "MISTRAL"
	ProviderOllama    Provider = "OLLAMA"
	ProviderGemini    Provider = "GEMINI"
	ProviderKling     Provider = "KLING"
	// ProviderVLLM targets a self-hosted vLLM inference server.
	// It speaks the OpenAI-compatible chat completions API.
	ProviderVLLM    Provider = "VLLM"
	// ProviderLocalAI targets a LocalAI server (OpenAI-compatible).
	ProviderLocalAI Provider = "LOCALAI"
	// ProviderAzureOpenAI targets Azure OpenAI deployments.
	// Uses the same request format as OpenAI but requires an api-key header and
	// a deployment-scoped base URL.
	ProviderAzureOpenAI Provider = "AZURE_OPENAI"
	// ProviderGroq targets the Groq inference API (OpenAI-compatible).
	ProviderGroq Provider = "GROQ"
	// ProviderCohere targets the Cohere Chat v2 API.
	ProviderCohere Provider = "COHERE"
	// ProviderBedrock targets AWS Bedrock via the Converse API.
	// Authentication uses AWS SigV4 (access key + secret, or IAM role).
	ProviderBedrock Provider = "BEDROCK"
)

var knownProviders = map[Provider]bool{
	ProviderCustom:      true,
	ProviderOpenAi:      true,
	ProviderAnthropic:   true,
	ProviderMistral:     true,
	ProviderOllama:      true,
	ProviderGemini:      true,
	ProviderKling:       true,
	ProviderVLLM:        true,
	ProviderLocalAI:     true,
	ProviderAzureOpenAI: true,
	ProviderGroq:        true,
	ProviderCohere:      true,
	ProviderBedrock:     true,
}

func (p Provider) IsValid() bool {
	return knownProviders[p]
}

// InputType is the media kind of a single model input field.
type InputType string

const (
	InputTypeText  InputType = "text"
	InputTypeImage InputType = "image"
	InputTypeFile  InputType = "file"
	InputTypeAudio InputType = "audio"
	InputTypeVideo InputType = "video"
)
