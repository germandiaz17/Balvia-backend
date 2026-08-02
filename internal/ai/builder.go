package ai

import "fmt"

// Build returns a Categorizer for the resolved settings. It is the default
// factory used by Service; tests inject a fake builder to avoid network.
func Build(s ResolvedSettings) (Categorizer, error) {
	switch s.Provider {
	case ProviderAnthropic:
		return NewAnthropicCategorizer(s.APIKey, s.Model), nil
	case ProviderOpenAICompatible:
		return NewOpenAICategorizer(s.APIKey, s.BaseURL, s.Model), nil
	default:
		return nil, fmt.Errorf("ai: unknown provider %q", s.Provider)
	}
}
