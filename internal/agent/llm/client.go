// Package llm implements the OpenAI-compatible chat-completions transport used
// by the runner-side agent loop.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
)

const (
	defaultMaxRequestBytes  = 2 * 1024 * 1024
	defaultMaxResponseBytes = 2 * 1024 * 1024
	defaultRequestTimeout   = 15 * time.Minute
)

var (
	ErrInvalidEndpoint  = errors.New("model endpoint must be an HTTP loopback sidecar URL")
	ErrRequestTooLarge  = errors.New("model request exceeds the configured size limit")
	ErrResponseTooLarge = errors.New("model response exceeds the configured size limit")
	ErrInvalidResponse  = errors.New("model endpoint returned an unsupported response")
	ErrHTTPStatus       = errors.New("model endpoint returned a non-success status")
	ErrTransport        = errors.New("model endpoint request failed")
)

// Config configures the local sidecar transport. The client never accepts an
// upstream credential; the sidecar injects credentials from its Secret mount.
type Config struct {
	BaseURL          string
	HTTPClient       *http.Client
	MaxRequestBytes  int64
	MaxResponseBytes int64
	RequestTimeout   time.Duration
}

// Client sends non-streaming chat-completions requests to a loopback sidecar.
type Client struct {
	endpoint         url.URL
	httpClient       *http.Client
	maxRequestBytes  int64
	maxResponseBytes int64
}

// NewClient validates the sidecar URL and prevents automatic redirects. This
// preserves provider path prefixes and query parameters while keeping requests
// on the trusted local proxy.
func NewClient(config Config) (*Client, error) {
	endpoint, err := parseBaseURL(config.BaseURL)
	if err != nil {
		return nil, ErrInvalidEndpoint
	}
	if config.MaxRequestBytes <= 0 {
		config.MaxRequestBytes = defaultMaxRequestBytes
	}
	if config.MaxResponseBytes <= 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	clientCopy := *httpClient
	clientCopy.Jar = nil
	transport := clientCopy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if standardTransport, ok := transport.(*http.Transport); ok {
		transportCopy := standardTransport.Clone()
		transportCopy.Proxy = nil
		clientCopy.Transport = transportCopy
	}
	if clientCopy.Timeout <= 0 {
		clientCopy.Timeout = config.RequestTimeout
	}
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		endpoint: *endpoint, httpClient: &clientCopy,
		maxRequestBytes: config.MaxRequestBytes, maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

// Complete makes exactly one HTTP attempt. Retry policy belongs to the agent
// loop, which charges every attempt against the admitted job budget.
func (c *Client) Complete(ctx context.Context, request loop.ModelRequest) (loop.ModelResponse, error) {
	if c == nil || ctx == nil || strings.TrimSpace(request.Model) == "" || request.MaxOutputTokens <= 0 {
		return loop.ModelResponse{}, ErrInvalidResponse
	}
	endpoint := completionsURL(c.endpoint)
	wireRequest := chatRequest{
		Model: request.Model, Messages: request.Messages, MaxTokens: request.MaxOutputTokens,
		Stream: false,
	}
	if len(request.Tools) > 0 {
		wireRequest.Tools = request.Tools
		wireRequest.ToolChoice = "auto"
	}
	if request.ReasoningEffort != "" {
		wireRequest.ReasoningEffort = request.ReasoningEffort
	}
	body, err := json.Marshal(wireRequest)
	if err != nil {
		return loop.ModelResponse{}, ErrInvalidResponse
	}
	if int64(len(body)) > c.maxRequestBytes {
		return loop.ModelResponse{}, ErrRequestTooLarge
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return loop.ModelResponse{}, ErrInvalidEndpoint
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return loop.ModelResponse{}, ctx.Err()
		}
		return loop.ModelResponse{}, &TransportError{}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return loop.ModelResponse{}, &StatusError{StatusCode: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return loop.ModelResponse{}, ctx.Err()
		}
		return loop.ModelResponse{}, &TransportError{}
	}
	if int64(len(data)) > c.maxResponseBytes {
		return loop.ModelResponse{}, ErrResponseTooLarge
	}
	return decodeResponse(data)
}

type chatRequest struct {
	Model           string                `json:"model"`
	Messages        []loop.Message        `json:"messages"`
	Tools           []loop.ToolDefinition `json:"tools,omitempty"`
	ToolChoice      string                `json:"tool_choice,omitempty"`
	MaxTokens       int                   `json:"max_tokens"`
	Stream          bool                  `json:"stream"`
	ReasoningEffort string                `json:"reasoning_effort,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		FinishReason *string `json:"finish_reason"`
		Message      struct {
			Role      string         `json:"role"`
			Content   *string        `json:"content"`
			ToolCalls []responseCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
		TotalTokens      *int `json:"total_tokens"`
	} `json:"usage"`
}

type responseCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// StatusError exposes only the HTTP status, never a provider response body that
// could contain secrets, prompts, or other sensitive data.
type StatusError struct {
	StatusCode int
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s: %d", ErrHTTPStatus, e.StatusCode) }
func (e *StatusError) Unwrap() error { return ErrHTTPStatus }
func (e *StatusError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= http.StatusInternalServerError
}

// TransportError intentionally omits the underlying URL/error string.
type TransportError struct{}

func (*TransportError) Error() string   { return ErrTransport.Error() }
func (*TransportError) Unwrap() error   { return ErrTransport }
func (*TransportError) Retryable() bool { return true }

func decodeResponse(data []byte) (loop.ModelResponse, error) {
	var wire chatResponse
	if err := json.Unmarshal(data, &wire); err != nil || len(wire.Choices) != 1 {
		return loop.ModelResponse{}, ErrInvalidResponse
	}
	choice := wire.Choices[0]
	if choice.Message.Role != string(loop.RoleAssistant) {
		return loop.ModelResponse{}, ErrInvalidResponse
	}
	message := loop.Message{Role: loop.RoleAssistant}
	if choice.Message.Content != nil {
		message.Content = *choice.Message.Content
	}
	for _, call := range choice.Message.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, loop.ToolCall{
			ID: call.ID, Type: call.Type,
			Function: loop.FunctionCall{Name: call.Function.Name, Arguments: call.Function.Arguments},
		})
	}
	response := loop.ModelResponse{Message: message}
	if choice.FinishReason != nil {
		response.FinishReason = *choice.FinishReason
	}
	if wire.Usage != nil {
		usage := &loop.TokenUsage{}
		if wire.Usage.PromptTokens != nil {
			usage.PromptTokens = *wire.Usage.PromptTokens
			usage.PromptTokensPresent = true
		}
		if wire.Usage.CompletionTokens != nil {
			usage.CompletionTokens = *wire.Usage.CompletionTokens
			usage.CompletionTokensPresent = true
		}
		if wire.Usage.TotalTokens != nil {
			usage.TotalTokens = *wire.Usage.TotalTokens
			usage.TotalTokensPresent = true
		}
		response.Usage = usage
	}
	return response, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, ErrInvalidEndpoint
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, ErrInvalidEndpoint
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" {
		u.Host = canonicalHost(host, u.Port())
		return u, nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, ErrInvalidEndpoint
	}
	u.Host = canonicalHost(ip.String(), u.Port())
	return u, nil
}

func canonicalHost(host, port string) string {
	if port != "" {
		return net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func completionsURL(base url.URL) url.URL {
	escapedPath := strings.TrimRight(base.EscapedPath(), "/")
	escapedPath += "/chat/completions"
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		// parseBaseURL only accepts URLs that net/url can parse, so this is
		// defensive; the original path can still be used without changing host.
		decodedPath = strings.TrimRight(base.Path, "/") + "/chat/completions"
		base.RawPath = ""
	} else {
		base.RawPath = escapedPath
	}
	base.Path = decodedPath
	return base
}
