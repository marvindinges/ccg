// Package ai talks to any OpenAI-compatible chat-completions endpoint to draft
// a Conventional Commit from a diff. It is provider-agnostic: only base URL,
// model and API key differ, all supplied via config. The response is parsed
// defensively so a malformed reply degrades to an editable draft rather than
// failing the workflow.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/config"
)

// Client is an OpenAI-compatible chat-completions client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	hasKey     bool // whether a real key (not the local placeholder) was supplied
	model      string
	strict     bool
	logger     *log.Logger // optional debug logger; nil disables logging
}

// New builds a Client from provider config. The API key is read from the
// configured environment variable; a placeholder is used when empty so local
// servers (which ignore it) still receive a well-formed request.
func New(cfg config.Config) *Client {
	key := cfg.APIKey()
	hasKey := key != ""
	if key == "" {
		key = "sk-no-key-required"
	}
	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    strings.TrimRight(cfg.Provider.BaseURL, "/"),
		apiKey:     key,
		hasKey:     hasKey,
		model:      cfg.Provider.Model,
		strict:     cfg.Provider.StrictSchema,
	}
}

// WithLogger attaches a debug logger that records the request and response.
func (c *Client) WithLogger(l *log.Logger) *Client {
	c.logger = l
	return c
}

func (c *Client) logf(format string, args ...any) {
	if c.logger != nil {
		c.logger.Printf(format, args...)
	}
}

// chat request/response types (the common OpenAI subset).
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Suggest asks the model for a commit draft. A non-nil error means the request
// failed entirely; callers should fall back to manual entry. Note that an
// unrecognized commit type is NOT an error — it is returned for the user to fix
// during review.
//
// Some OpenAI-compatible providers reject the `response_format` parameter; when
// the first attempt fails and a response_format was set, Suggest retries once
// without it (parsing is defensive, so plain text still works).
func (c *Client) Suggest(ctx context.Context, in SuggestInput) (commit.Commit, error) {
	messages := []chatMessage{
		{Role: "system", Content: systemPrompt(in)},
		{Role: "user", Content: userPrompt(in)},
	}

	c.logf("provider: base_url=%s model=%s api_key=%s", c.baseURL, c.model, c.maskedKey())

	rf := c.responseFormat()
	content, err := c.complete(ctx, messages, rf)
	if err != nil && rf != nil {
		c.logf("first attempt failed (%v); retrying without response_format", err)
		content, err = c.complete(ctx, messages, nil)
	}
	if err != nil {
		return commit.Commit{}, err
	}

	cm, perr := parseCommit(content)
	if perr != nil {
		c.logf("parse failed: %v", perr)
		return commit.Commit{}, fmt.Errorf("parse model output: %w", perr)
	}
	if strings.TrimSpace(cm.Description) == "" {
		c.logf("model returned no usable description (likely echoed the schema or ignored the format)")
		return commit.Commit{}, fmt.Errorf(
			"model returned no usable commit message — it may be too small to follow the format. Try a more capable model, or set strict_schema: true in your config")
	}
	c.logf("parsed draft: %s", cm.Header())
	return cm, nil
}

// complete performs a single chat-completions request and returns the assistant
// message content.
func (c *Client) complete(ctx context.Context, messages []chatMessage, rf map[string]any) (string, error) {
	reqBody := chatRequest{
		Model:          c.model,
		Temperature:    0.2,
		Messages:       messages,
		ResponseFormat: rf,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := c.baseURL + "/chat/completions"
	c.logf("POST %s (response_format=%v)", url, rf)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logf("transport error: %v", err)
		return "", fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.logf("response %s\n%s", resp.Status, truncForLog(string(body)))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("decode provider response: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("provider error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("provider returned no choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// responseFormat selects JSON mode (broadly supported) or strict json_schema
// when opted in.
func (c *Client) responseFormat() map[string]any {
	if c.strict {
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "conventional_commit",
				"strict": true,
				"schema": jsonSchema(),
			},
		}
	}
	return map[string]any{"type": "json_object"}
}

// maskedKey returns a log-safe representation of the API key.
func (c *Client) maskedKey() string {
	if !c.hasKey {
		return "(none — local placeholder)"
	}
	if len(c.apiKey) <= 4 {
		return "****"
	}
	return "****" + c.apiKey[len(c.apiKey)-4:]
}

func truncForLog(s string) string {
	const max = 2000
	if len(s) > max {
		return s[:max] + "… (truncated)"
	}
	return s
}
