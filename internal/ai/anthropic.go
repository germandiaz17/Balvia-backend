package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
)

// defaultAnthropicModel is the Claude model used when the user didn't override
// it. Haiku is the fastest/cheapest tier — plenty for a bounded classification.
const defaultAnthropicModel = "claude-haiku-4-5"

// AnthropicCategorizer implements Categorizer against the Claude API.
type AnthropicCategorizer struct {
	client anthropic.Client
	model  anthropic.Model
}

// NewAnthropicCategorizer builds a categorizer from a user's API key and optional
// model override.
func NewAnthropicCategorizer(apiKey, model string) *AnthropicCategorizer {
	if model == "" {
		model = defaultAnthropicModel
	}
	return &AnthropicCategorizer{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  anthropic.Model(model),
	}
}

// Suggest forces a single tool call whose input schema constrains category_id to
// the exact set of candidate ids (via enum), so the model can't return an id we
// didn't offer.
func (a *AnthropicCategorizer) Suggest(ctx context.Context, req SuggestRequest) (Suggestion, error) {
	ids, list := candidateList(req)
	enum := make([]any, len(ids))
	for i, v := range ids {
		enum[i] = v
	}

	tool := anthropic.ToolParam{
		Name:        toolName,
		Description: anthropic.String("Record the single best-matching category for the expense, with a confidence from 0 to 1."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"category_id": map[string]any{
					"type":        "string",
					"enum":        enum,
					"description": "The id of the chosen category. Must be one of the provided ids.",
				},
				"confidence": map[string]any{
					"type":        "number",
					"description": "Confidence in the choice, from 0.0 (pure guess) to 1.0 (certain).",
				},
			},
			Required: []string{"category_id", "confidence"},
		},
	}

	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 256,
		Tools:     []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: toolName},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(buildPrompt(req, list))),
		},
	})
	if err != nil {
		return Suggestion{}, fmt.Errorf("ai categorize (anthropic): %w", err)
	}

	for _, block := range resp.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		var out struct {
			CategoryID string  `json:"category_id"`
			Confidence float64 `json:"confidence"`
		}
		if err := json.Unmarshal([]byte(tu.JSON.Input.Raw()), &out); err != nil {
			return Suggestion{}, fmt.Errorf("ai categorize (anthropic): bad tool input: %w", err)
		}
		id, err := uuid.Parse(out.CategoryID)
		if err != nil {
			return Suggestion{}, nil // non-uuid → treat as no suggestion
		}
		return Suggestion{CategoryID: id, Confidence: out.Confidence}, nil
	}
	return Suggestion{}, nil // model didn't call the tool
}
