package domain

import (
	"math"
	"strings"
	"testing"
)

func TestComputeUsageCostUSDPricesPromptCacheBucketsSeparately(t *testing.T) {
	def := ModelDefinition{
		InputPricePer1MTokens:             10,
		CachedInputPricePer1MTokens:       1,
		CacheWriteInputPricePer1MTokens:   12.5,
		CacheWrite1hInputPricePer1MTokens: 20,
		OutputPricePer1MTokens:            30,
	}
	usage := TokenUsage{
		InputTokens:             1_000,
		CachedInputTokens:       400,
		CacheWriteInputTokens:   100,
		CacheWrite1hInputTokens: 50,
		OutputTokens:            200,
	}

	got := def.ComputeUsageCostUSD(usage)
	want := (450*10 + 400*1 + 100*12.5 + 50*20 + 200*30) / 1_000_000
	if math.Abs(got-want) > 0.0000001 {
		t.Fatalf("cost = %.9f, want %.9f", got, want)
	}
}

func TestComputeUsageCostUSDFallsBackToBaseInputForUnknownCachePrices(t *testing.T) {
	def := ModelDefinition{
		InputPricePer1MTokens:  10,
		OutputPricePer1MTokens: 30,
	}
	usage := TokenUsage{
		InputTokens:           1_000,
		CachedInputTokens:     400,
		CacheWriteInputTokens: 100,
		OutputTokens:          200,
	}

	got := def.ComputeUsageCostUSD(usage)
	want := float64(1_000*10+200*30) / 1_000_000
	if math.Abs(got-want) > 0.0000001 {
		t.Fatalf("cost = %.9f, want %.9f", got, want)
	}
}

func TestOpenAICachedInputPricingUsesModelIDConstants(t *testing.T) {
	cases := []struct {
		modelID string
		want    float64
	}{
		{openAIModelGPT55, 0.50},
		{openAIModelGPT54, 0.25},
		{openAIModelGPT54Mini, 0.075},
		{openAIModelGPT54Nano, 0.02},
		{openAIModelGPT52, 0.175},
		{openAIModelGPT51, 0.125},
		{openAIModelGPT5, 0.125},
		{openAIModelGPT5Mini, 0.025},
		{openAIModelGPT5Nano, 0.005},
		{openAIModelGPT41, 0.50},
		{openAIModelGPT41Mini, 0.10},
		{openAIModelGPT41Nano, 0.025},
		{openAIModelGPT4o, 1.25},
		{openAIModelGPT4oMini, 0.075},
		{openAIModelO4Mini, 0.275},
		{openAIModelO3, 0.50},
		{openAIModelO3Mini, 0.55},
		{openAIModelO1, 7.50},
		{openAIModelO1Mini, 0.55},
	}

	for _, tc := range cases {
		got, ok := openAICachedInputPricesPer1M[tc.modelID]
		if !ok {
			t.Fatalf("cached input price missing for %s", tc.modelID)
		}
		if got != tc.want {
			t.Fatalf("cached input price for %s = %.6f, want %.6f", tc.modelID, got, tc.want)
		}

		catalogKey := openAIChatCatalogKey(tc.modelID)
		def, ok := catalog[catalogKey]
		if !ok {
			t.Fatalf("catalog entry missing for %s", catalogKey)
		}
		if def.Key != catalogKey || def.ModelID != tc.modelID {
			t.Fatalf("catalog entry drift for %s: key=%q modelID=%q", catalogKey, def.Key, def.ModelID)
		}
	}
}

func TestCatalogEntriesUseKnownProviderModelIDConstants(t *testing.T) {
	expectedPrefixes := map[Provider]string{
		ProviderAnthropic: anthropicCatalogPrefix,
		ProviderGemini:    geminiCatalogPrefix,
		ProviderMistral:   mistralCatalogPrefix,
		ProviderOllama:    ollamaCatalogPrefix,
		ProviderVLLM:      vllmCatalogPrefix,
		ProviderLocalAI:   localAICatalogPrefix,
		ProviderKling:     klingCatalogPrefix,
		ProviderGroq:      groqCatalogPrefix,
		ProviderCohere:    cohereCatalogPrefix,
		ProviderBedrock:   bedrockCatalogPrefix,
	}

	for catalogKey, def := range catalog {
		switch def.Provider {
		case ProviderOpenAi:
			switch {
			case strings.HasPrefix(catalogKey, openAIChatCatalogPrefix):
				if _, ok := knownOpenAIChatModelIDs[def.ModelID]; !ok {
					t.Fatalf("%s uses OpenAI chat model ID %q without a shared constant", catalogKey, def.ModelID)
				}
				if want := openAIChatCatalogKey(def.ModelID); def.Key != want || catalogKey != want {
					t.Fatalf("%s key drift: def.Key=%q want=%q", catalogKey, def.Key, want)
				}
			case strings.HasPrefix(catalogKey, openAIEmbeddingCatalogPrefix):
				if _, ok := knownOpenAIEmbeddingModelIDs[def.ModelID]; !ok {
					t.Fatalf("%s uses OpenAI embedding model ID %q without a shared constant", catalogKey, def.ModelID)
				}
				if want := openAIEmbeddingCatalogKey(def.ModelID); def.Key != want || catalogKey != want {
					t.Fatalf("%s key drift: def.Key=%q want=%q", catalogKey, def.Key, want)
				}
			}
		case ProviderAzureOpenAI:
			if _, ok := knownOpenAIChatModelIDs[def.ModelID]; !ok {
				t.Fatalf("%s uses Azure OpenAI model ID %q without a shared constant", catalogKey, def.ModelID)
			}
			if want := azureOpenAICatalogKey(def.ModelID); def.Key != want || catalogKey != want {
				t.Fatalf("%s key drift: def.Key=%q want=%q", catalogKey, def.Key, want)
			}
		case ProviderCustom:
			if catalogKey != customCatalogKey || def.Key != customCatalogKey || def.ModelID != "" {
				t.Fatalf("custom entry drift: catalogKey=%q def.Key=%q modelID=%q", catalogKey, def.Key, def.ModelID)
			}
		default:
			known, ok := knownCatalogModelIDsByProvider[def.Provider]
			if !ok {
				t.Fatalf("%s provider %q has no known model ID set", catalogKey, def.Provider)
			}
			if _, ok := known[def.ModelID]; !ok {
				t.Fatalf("%s uses model ID %q without a shared constant", catalogKey, def.ModelID)
			}
			prefix, ok := expectedPrefixes[def.Provider]
			if !ok {
				t.Fatalf("%s provider %q has no expected catalog prefix", catalogKey, def.Provider)
			}
			if !strings.HasPrefix(catalogKey, prefix) || !strings.HasPrefix(def.Key, prefix) {
				t.Fatalf("%s key drift: def.Key=%q expected prefix=%q", catalogKey, def.Key, prefix)
			}
		}
	}
}
