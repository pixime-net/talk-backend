package domain

import "fmt"

// APIClient identifies the specific API client to use for a model (the SDK)
type APIClient string

const (
	APIClientOpenAI     APIClient = "openai"
	APIClientAnthropic  APIClient = "anthropic"
	APIClientOpenRouter APIClient = "openrouter"
)

// OTLPProvider identifies the LLM provider backend.
type OTLPProvider string

/*
OTLP GenAI semantic conventions for gen_ai.system (https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/):
openai	OpenAI
anthropic	Anthropic
aws.bedrock	AWS Bedrock
az.ai.inference	Azure AI Inference
az.ai.openai	Azure OpenAI
google_vertexai	Google Vertex AI
google_generativeai	Google Gemini
cohere	Cohere
mistral_ai	Mistral AI
perplexity	Perplexity
xai	xAI
deepseek	DeepSeek
groq	Groq
ibm.watsonx_ai	IBM Watsonx
_other	Other provider (use with gen_ai.system_description)
*/
const (
	OTLPProviderAnthropic  OTLPProvider = "anthropic"
	OTLPProviderOpenAI     OTLPProvider = "openai"
	OTLPProviderMistral    OTLPProvider = "mistral_ai"
	OTLPProviderOpenRouter OTLPProvider = "openrouter"
	OTLPProviderPoolside   OTLPProvider = "_other"
)

// ThinkingStyle describes how a model supports thinking/reasoning.
type ThinkingStyle string

const (
	ThinkingStyleNone     ThinkingStyle = ""         // model does not support thinking
	ThinkingStyleAdaptive ThinkingStyle = "adaptive" // Anthropic adaptive thinking (opus, mythos)
	ThinkingStyleBudget   ThinkingStyle = "budget"   // Anthropic enabled thinking with explicit budget_tokens
	ThinkingStyleEffort   ThinkingStyle = "effort"   // OpenAI reasoning_effort (o-series)
)

// OutputLimitParameter identifies the OpenAI-compatible request field used for an output ceiling.
type OutputLimitParameter string

const (
	// MaxTokens is the standard OpenAI-compatible output limit parameter.
	// used by Mistral, Openrouter and old OpenAI models.
	OutputLimitParameterMaxTokens OutputLimitParameter = "max_tokens"
	// OutputLimitParameterMaxCompletionTokens is the OpenAI-compatible output limit parameter for the completion portion of the request.
	// used by some recent OpenAI models.
	OutputLimitParameterMaxCompletionTokens OutputLimitParameter = "max_completion_tokens"
)

// Model maps a friendly model alias to provider-specific details.
type Model struct {
	Name                    string               // friendly alias for a model (e.g. "sonnet-4.6").
	OTLPProvider            OTLPProvider         // The LLM provider following OpenTelemetry GenAI semantic conventions.
	APIClient               APIClient            // The SDK client to use for this model.
	APIKeyName              string               // Name of the environment variable for the API key.
	URL                     string               // Optional base URL for API-compatible providers.
	APIModelID              string               // The model ID to use in the API request.
	ThinkingStyle           ThinkingStyle        // How the model supports thinking/reasoning.
	ContextWindowTokens     int64                // Documented provider context-window capacity.
	ProviderMaxOutputTokens int64                // Documented provider output capacity.
	RequestMaxOutputTokens  int64                // Talk's configured request output ceiling.
	OutputLimitParameter    OutputLimitParameter // OpenAI-compatible output-limit field.
}

// EffectiveOutputLimit resolves Talk's request ceiling against the provider capability.
func (m Model) EffectiveOutputLimit() int64 {
	if m.ProviderMaxOutputTokens > 0 {
		if m.RequestMaxOutputTokens > 0 && m.RequestMaxOutputTokens <= m.ProviderMaxOutputTokens {
			return m.RequestMaxOutputTokens
		}
		return m.ProviderMaxOutputTokens
	}
	if m.RequestMaxOutputTokens > 0 {
		return m.RequestMaxOutputTokens
	}
	return 0
}

// registry holds all supported models.
var registry = []Model{
	{Name: "haiku-4.5", OTLPProvider: OTLPProviderAnthropic, APIClient: APIClientAnthropic, APIKeyName: "ANTHROPIC_API_KEY", APIModelID: "claude-haiku-4-5", ThinkingStyle: ThinkingStyleBudget, ContextWindowTokens: 200_000, ProviderMaxOutputTokens: 64_000, RequestMaxOutputTokens: 8192},
	{Name: "sonnet-4.6", OTLPProvider: OTLPProviderAnthropic, APIClient: APIClientAnthropic, APIKeyName: "ANTHROPIC_API_KEY", APIModelID: "claude-sonnet-4-5", ThinkingStyle: ThinkingStyleBudget, ContextWindowTokens: 200_000, ProviderMaxOutputTokens: 64_000, RequestMaxOutputTokens: 16384},
	{Name: "sonnet-5", OTLPProvider: OTLPProviderAnthropic, APIClient: APIClientAnthropic, APIKeyName: "ANTHROPIC_API_KEY", APIModelID: "claude-sonnet-5", ThinkingStyle: ThinkingStyleAdaptive, ContextWindowTokens: 1_000_000, ProviderMaxOutputTokens: 128_000, RequestMaxOutputTokens: 16384},
	{Name: "opus-4.6", OTLPProvider: OTLPProviderAnthropic, APIClient: APIClientAnthropic, APIKeyName: "ANTHROPIC_API_KEY", APIModelID: "claude-opus-4-6", ThinkingStyle: ThinkingStyleAdaptive, ContextWindowTokens: 1_000_000, ProviderMaxOutputTokens: 128_000, RequestMaxOutputTokens: 16384},
	{Name: "o4-mini", OTLPProvider: OTLPProviderOpenAI, APIClient: APIClientOpenAI, APIKeyName: "OPENAI_API_KEY", APIModelID: "o4-mini", ThinkingStyle: ThinkingStyleEffort, ContextWindowTokens: 200_000, ProviderMaxOutputTokens: 100_000, RequestMaxOutputTokens: 16384, OutputLimitParameter: OutputLimitParameterMaxCompletionTokens},
	{Name: "gpt-5.4", OTLPProvider: OTLPProviderOpenAI, APIClient: APIClientOpenAI, APIKeyName: "OPENAI_API_KEY", APIModelID: "gpt-4o", ContextWindowTokens: 128_000, ProviderMaxOutputTokens: 16_384, RequestMaxOutputTokens: 16384, OutputLimitParameter: OutputLimitParameterMaxTokens},
	{Name: "mistral-small", OTLPProvider: OTLPProviderMistral, APIClient: APIClientOpenAI, APIKeyName: "MISTRAL_API_KEY", URL: "https://api.mistral.ai/v1", APIModelID: "mistral-small-latest", ContextWindowTokens: 256_000, RequestMaxOutputTokens: 16_384, OutputLimitParameter: OutputLimitParameterMaxTokens},
	{Name: "mistral-medium", OTLPProvider: OTLPProviderMistral, APIClient: APIClientOpenAI, APIKeyName: "MISTRAL_API_KEY", URL: "https://api.mistral.ai/v1", APIModelID: "mistral-medium-latest", ContextWindowTokens: 256_000, RequestMaxOutputTokens: 16_384, OutputLimitParameter: OutputLimitParameterMaxTokens},
	// DeepSeek-V3.2: Version améliorée avec excellent raisonnement, 128K contexte, prix $0.14/M input, $0.28/M output
	{Name: "deepseek-v3.2", OTLPProvider: OTLPProviderOpenRouter, APIClient: APIClientOpenRouter, APIKeyName: "OPENROUTER_API_KEY", APIModelID: "deepseek/deepseek-v3.2", ThinkingStyle: ThinkingStyleEffort, ContextWindowTokens: 106_496, ProviderMaxOutputTokens: 8_192, RequestMaxOutputTokens: 8192, OutputLimitParameter: OutputLimitParameterMaxTokens},
	// DeepSeek-4.1-Flash: Version flash rapide et économique, 128K contexte, prix $0.10/M input/output
	{Name: "deepseek-v4.1-flash", OTLPProvider: OTLPProviderOpenRouter, APIClient: APIClientOpenRouter, APIKeyName: "OPENROUTER_API_KEY", APIModelID: "deepseek/deepseek-v4.1-flash", ThinkingStyle: ThinkingStyleEffort, ContextWindowTokens: 128_000, ProviderMaxOutputTokens: 8_192, RequestMaxOutputTokens: 8192, OutputLimitParameter: OutputLimitParameterMaxTokens},
}

// Lookup returns the model details for a given alias.
func Lookup(modelID string) (Model, error) {
	for _, descriptor := range registry {
		if descriptor.Name == modelID {
			return descriptor, nil
		}
	}

	return Model{}, fmt.Errorf("unknown model %q", modelID)
}

// SupportedModels returns all registered model aliases.
func SupportedModels() []string {
	models := make([]string, 0, len(registry))
	for _, descriptor := range registry {
		models = append(models, descriptor.Name)
	}
	return models
}
