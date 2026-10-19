package dream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

// This file implements the four built-in model providers of
// docs/11-github-actions-dreaming.md section 2.4. Every provider is called
// with net/http only: no SDK dependency is added to the single-file binary, and
// the request shape stays reviewable. A provider returns raw model text; the
// structured-output contract is enforced by the worker's Step Guard, not by the
// provider transport.
//
// Providers are only constructed inside the GitHub Actions worker. The local
// `memctl dream` path never reads a model key and never calls a model endpoint;
// see docs/11-github-actions-dreaming.md section 3.2.

// Provider identifiers accepted by `--runner` and the HTTP API.
const (
	ProviderDeepSeek  = "deepseek"
	ProviderMiniMax   = "minimax"
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
)

// ProviderIDs is the documented, ordered provider list. The CLI help, the UI
// selector and the error message of an unknown provider all read it.
var ProviderIDs = []string{ProviderDeepSeek, ProviderMiniMax, ProviderOpenAI, ProviderAnthropic}

// ProviderIDsList renders the documented provider list for help and errors.
func ProviderIDsList() string { return strings.Join(ProviderIDs, ", ") }

// KnownProvider reports whether id names a built-in provider.
func KnownProvider(id string) bool {
	for _, candidate := range ProviderIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

// ProviderInfo is the static, non-secret part of one provider: its default
// endpoint, the Actions secret that carries its key and its default model.
type ProviderInfo struct {
	ID           string
	BaseURL      string
	SecretName   string
	DefaultModel string
}

// providerTable is the documented provider registry of
// docs/11-github-actions-dreaming.md section 2.4.
var providerTable = map[string]ProviderInfo{
	ProviderDeepSeek: {
		ID:           ProviderDeepSeek,
		BaseURL:      "https://api.deepseek.com/chat/completions",
		SecretName:   "DEEPSEEK_API_KEY",
		DefaultModel: "deepseek-chat",
	},
	ProviderMiniMax: {
		ID:           ProviderMiniMax,
		BaseURL:      "https://api.minimaxi.com/v1/chat/completions",
		SecretName:   "MINIMAX_API_KEY",
		DefaultModel: "MiniMax-M2.7",
	},
	ProviderOpenAI: {
		ID:           ProviderOpenAI,
		BaseURL:      "https://api.openai.com/v1/chat/completions",
		SecretName:   "OPENAI_API_KEY",
		DefaultModel: "gpt-4.1",
	},
	ProviderAnthropic: {
		ID:           ProviderAnthropic,
		BaseURL:      "https://api.anthropic.com/v1/messages",
		SecretName:   "ANTHROPIC_API_KEY",
		DefaultModel: "claude-sonnet-4-5",
	},
}

// ProviderInfoFor returns the registry entry of one provider.
func ProviderInfoFor(id string) (ProviderInfo, bool) {
	info, ok := providerTable[id]
	return info, ok
}

// CompletionRequest is one structured-output model call. Schema is a JSON
// Schema document; providers that support native schema enforcement send it as
// such, the others embed it in the prompt and rely on the Step Guard.
type CompletionRequest struct {
	System string
	User   string
	Schema []byte
}

// Provider is one model-provider transport. Complete performs exactly one
// non-streaming request and returns the model's text.
type Provider interface {
	// ID is the provider identifier used by `--runner`.
	ID() string
	// Model is the model name this provider was configured with.
	Model() string
	// Complete performs one structured-output request.
	Complete(ctx context.Context, req CompletionRequest) (string, error)
}

// ProviderOptions is the resolved, non-secret configuration of one provider.
// APIKey is read from the process environment inside the Actions runner and is
// never persisted or logged.
type ProviderOptions struct {
	ID      string
	APIKey  string
	BaseURL string
	Model   string
	// HTTPClient defaults to a client with the documented per-call timeout.
	HTTPClient *http.Client
}

// NewProvider builds one provider transport. An unknown id, a missing key or a
// non-HTTP endpoint is a targeted error; the worker converts it into a failed
// result commit so the local side learns the reason.
func NewProvider(opts ProviderOptions) (Provider, error) {
	info, ok := providerTable[opts.ID]
	if !ok {
		return nil, memerr.New(memerr.Unsupported,
			"provider %q is not built in; supported providers are %s", opts.ID, strings.Join(ProviderIDs, ", "))
	}
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, memerr.New(memerr.AuthFailed,
			"the %s API key is not configured; add the %s secret to the repository's GitHub Actions secrets",
			opts.ID, info.SecretName)
	}
	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		baseURL = info.BaseURL
	}
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return nil, memerr.New(memerr.InvalidArgument,
			"the %s endpoint must be an http(s) URL, got %q", opts.ID, baseURL)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = info.DefaultModel
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: ProviderCallTimeout}
	}
	transport := openAICompatible{id: opts.ID, model: model, key: opts.APIKey, url: baseURL, client: client}
	switch opts.ID {
	case ProviderDeepSeek, ProviderMiniMax:
		// DeepSeek documents `response_format: {"type": "json_object"}` only;
		// MiniMax's OpenAI-compatible endpoint does not guarantee schema
		// enforcement and rejects response_format together with streaming.
		transport.mode = modeJSONObject
	case ProviderOpenAI:
		transport.mode = modeJSONSchema
	case ProviderAnthropic:
		return anthropicProvider{model: model, key: opts.APIKey, url: baseURL, client: client}, nil
	}
	return transport, nil
}

// ProviderCallTimeout bounds one model call, as documented in
// docs/11-github-actions-dreaming.md section 2.3.
const ProviderCallTimeout = 120 * time.Second

// OpenAI-compatible request modes.
const (
	modeJSONObject = "json_object"
	modeJSONSchema = "json_schema"
)

// openAICompatible serves the three providers that speak the OpenAI chat
// completions shape. Only the response_format field differs.
type openAICompatible struct {
	id     string
	model  string
	key    string
	url    string
	mode   string
	client *http.Client
}

func (p openAICompatible) ID() string    { return p.id }
func (p openAICompatible) Model() string { return p.model }

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	Temperature    float64         `json:"temperature"`
	Stream         bool            `json:"stream"`
	ResponseFormat any             `json:"response_format,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p openAICompatible) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	system := req.System + "\n\nRespond with a single JSON object that validates against this JSON Schema:\n" + string(req.Schema)
	body := openAIRequest{
		Model:       p.model,
		Messages:    []openAIMessage{{Role: "system", Content: system}, {Role: "user", Content: req.User}},
		Temperature: 0,
		Stream:      false,
	}
	if p.mode == modeJSONSchema {
		body.ResponseFormat = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "dream_step",
				"strict": true,
				"schema": json.RawMessage(req.Schema),
			},
		}
	} else {
		body.ResponseFormat = map[string]any{"type": "json_object"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", memerr.Wrap(memerr.InvalidArgument, err, "cannot encode the %s request", p.id)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(payload))
	if err != nil {
		return "", memerr.Wrap(memerr.InvalidArgument, err, "cannot build the %s request", p.id)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.key)

	response, err := p.client.Do(request)
	if err != nil {
		return "", memerr.Wrap(memerr.NetworkUnavailable, err, "%s request failed", p.id)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes))
	if err != nil {
		return "", memerr.Wrap(memerr.NetworkUnavailable, err, "cannot read the %s response", p.id)
	}
	if response.StatusCode != http.StatusOK {
		return "", providerStatusError(p.id, response.StatusCode, data)
	}
	var decoded openAIResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", memerr.Wrap(memerr.Unsupported, err, "the %s response is not valid JSON", p.id)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		message := "the model returned no content"
		if decoded.Error != nil && decoded.Error.Message != "" {
			message = decoded.Error.Message
		}
		return "", memerr.New(memerr.Unsupported, "%s: %s", p.id, redactSecret(message, p.key))
	}
	return decoded.Choices[0].Message.Content, nil
}

// anthropicProvider uses the Messages API with a single forced tool whose input
// schema is the step schema. Tool use is the documented way to obtain a
// schema-shaped object from Claude, so no beta header is required.
type anthropicProvider struct {
	model  string
	key    string
	url    string
	client *http.Client
}

func (p anthropicProvider) ID() string    { return ProviderAnthropic }
func (p anthropicProvider) Model() string { return p.model }

type anthropicRequest struct {
	Model      string             `json:"model"`
	MaxTokens  int                `json:"max_tokens"`
	System     string             `json:"system"`
	Messages   []anthropicMessage `json:"messages"`
	Tools      []anthropicTool    `json:"tools"`
	ToolChoice map[string]string  `json:"tool_choice"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p anthropicProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	body := anthropicRequest{
		Model:     p.model,
		MaxTokens: 8192,
		System:    req.System,
		Messages:  []anthropicMessage{{Role: "user", Content: req.User}},
		Tools: []anthropicTool{{
			Name:        "emit_dream_step",
			Description: "Emit the memory reorganization step that satisfies the supplied JSON Schema.",
			InputSchema: json.RawMessage(req.Schema),
		}},
		ToolChoice: map[string]string{"type": "tool", "name": "emit_dream_step"},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", memerr.Wrap(memerr.InvalidArgument, err, "cannot encode the anthropic request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(payload))
	if err != nil {
		return "", memerr.Wrap(memerr.InvalidArgument, err, "cannot build the anthropic request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-api-key", p.key)
	request.Header.Set("anthropic-version", "2023-06-01")

	response, err := p.client.Do(request)
	if err != nil {
		return "", memerr.Wrap(memerr.NetworkUnavailable, err, "anthropic request failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes))
	if err != nil {
		return "", memerr.Wrap(memerr.NetworkUnavailable, err, "cannot read the anthropic response")
	}
	if response.StatusCode != http.StatusOK {
		return "", providerStatusError("anthropic", response.StatusCode, data)
	}
	var decoded anthropicResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", memerr.Wrap(memerr.Unsupported, err, "the anthropic response is not valid JSON")
	}
	for _, block := range decoded.Content {
		if block.Type == "tool_use" && len(block.Input) > 0 {
			return string(block.Input), nil
		}
	}
	message := "the model returned no tool input"
	if decoded.Error != nil && decoded.Error.Message != "" {
		message = decoded.Error.Message
	}
	return "", memerr.New(memerr.Unsupported, "anthropic: %s", redactSecret(message, p.key))
}

// maxProviderResponseBytes bounds one provider response body.
const maxProviderResponseBytes = 4 << 20

// providerStatusError maps a non-200 provider response to a contract error and
// strips the key from any echoed body.
func providerStatusError(id string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 400 {
		detail = detail[:400]
	}
	code := memerr.Unsupported
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		code = memerr.AuthFailed
	case status == http.StatusTooManyRequests:
		code = memerr.Busy
	case status >= 500:
		code = memerr.NetworkUnavailable
	}
	return memerr.New(code, "%s returned HTTP %d: %s", id, status, sanitizeProviderDetail(detail))
}

// sanitizeProviderDetail strips anything that looks like a credential from an
// error body before it can reach a commit message or a proposal record.
func sanitizeProviderDetail(detail string) string {
	for _, prefix := range []string{"sk-", "Bearer ", "x-api-key"} {
		if index := strings.Index(detail, prefix); index >= 0 {
			detail = detail[:index] + "[redacted]"
		}
	}
	return detail
}

// redactSecret removes one exact secret value from a message.
func redactSecret(message, secret string) string {
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "[redacted]")
}

// providerFromEnv builds the worker's provider from the Actions environment.
// The request branch names the provider, so no extra variable has to be kept in
// sync with the ref; MEMCTL_DREAM_PROVIDER overrides it if it is set.
func providerFromEnv(getenv func(string) string, fallback string, httpClient *http.Client) (Provider, error) {
	id := strings.TrimSpace(getenv("MEMCTL_DREAM_PROVIDER"))
	if id == "" {
		id = fallback
	}
	if id == "" {
		return nil, memerr.New(memerr.InvalidArgument, "no model provider was selected for this run")
	}
	info, ok := providerTable[id]
	if !ok {
		return nil, memerr.New(memerr.Unsupported,
			"provider %q is not built in; supported providers are %s", id, strings.Join(ProviderIDs, ", "))
	}
	return NewProvider(ProviderOptions{
		ID:         id,
		APIKey:     getenv(info.SecretName),
		BaseURL:    getenv("MEMCTL_DREAM_BASE_URL"),
		Model:      getenv("MEMCTL_DREAM_MODEL"),
		HTTPClient: httpClient,
	})
}

// formatProviderError renders one provider failure for a worker result commit.
func formatProviderError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
