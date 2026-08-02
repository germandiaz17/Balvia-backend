package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// defaultOpenAIBaseURL / defaultOpenAIModel are used when the user didn't set a
// base URL or model. The base URL is configurable so any OpenAI-compatible
// endpoint works (OpenAI, Groq, OpenRouter, Ollama, LM Studio, …).
const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultOpenAIModel   = "gpt-4o-mini"
)

// OpenAICategorizer implements Categorizer against any OpenAI-compatible
// /chat/completions endpoint, using function calling for structured output.
type OpenAICategorizer struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// NewOpenAICategorizer builds a categorizer from a user's API key, optional base
// URL (defaults to OpenAI) and optional model.
func NewOpenAICategorizer(apiKey, baseURL, model string) *OpenAICategorizer {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultOpenAIBaseURL
	}
	if model == "" {
		model = defaultOpenAIModel
	}
	return &OpenAICategorizer{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (o *OpenAICategorizer) Suggest(ctx context.Context, req SuggestRequest) (Suggestion, error) {
	ids, list := candidateList(req)

	payload := map[string]any{
		"model":       o.model,
		"max_tokens":  256,
		"messages":    []map[string]any{{"role": "user", "content": buildPrompt(req, list)}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": toolName}},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        toolName,
				"description": "Record the single best-matching category id and a confidence from 0 to 1.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"category_id": map[string]any{"type": "string", "enum": ids},
						"confidence":  map[string]any{"type": "number"},
					},
					"required":             []string{"category_id", "confidence"},
					"additionalProperties": false,
				},
			},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Suggestion{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Suggestion{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Suggestion{}, fmt.Errorf("ai categorize (openai): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Suggestion{}, fmt.Errorf("ai categorize (openai): http %d", resp.StatusCode)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Suggestion{}, fmt.Errorf("ai categorize (openai): decode: %w", err)
	}

	// Prefer the tool-call arguments; fall back to message content (some
	// compatible servers return the JSON there instead).
	var args string
	if len(out.Choices) > 0 {
		msg := out.Choices[0].Message
		if len(msg.ToolCalls) > 0 {
			args = msg.ToolCalls[0].Function.Arguments
		} else if strings.TrimSpace(msg.Content) != "" {
			args = msg.Content
		}
	}
	if strings.TrimSpace(args) == "" {
		return Suggestion{}, nil
	}

	var parsed struct {
		CategoryID string  `json:"category_id"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return Suggestion{}, nil // couldn't parse → no suggestion
	}
	id, err := uuid.Parse(parsed.CategoryID)
	if err != nil {
		return Suggestion{}, nil
	}
	return Suggestion{CategoryID: id, Confidence: parsed.Confidence}, nil
}
