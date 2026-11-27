package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestClientPreservesProviderPrefixAndQuery(t *testing.T) {
	var observed bool
	client := testClient(t, "http://127.0.0.1:8080/provider-prefix/v1?api-version=2026-01", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		observed = true
		if got, want := r.URL.Path, "/provider-prefix/v1/chat/completions"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got := r.URL.Query().Get("api-version"); got != "2026-01" {
			t.Errorf("api-version = %q", got)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("client sent Authorization header %q", got)
		}
		var request map[string]any
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request["model"] != "catalog-model" || request["max_tokens"] != float64(128) || request["stream"] != false {
			t.Errorf("request fields = %#v", request)
		}
		tools, ok := request["tools"].([]any)
		if !ok || len(tools) != 1 || request["tool_choice"] != "auto" {
			t.Errorf("tools/tool_choice = %#v / %#v", request["tools"], request["tool_choice"])
		}
		if request["reasoning_effort"] != "low" {
			t.Errorf("reasoning_effort = %#v", request["reasoning_effort"])
		}
		return testResponse(r, http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`), nil
	}))
	response, err := client.Complete(context.Background(), loop.ModelRequest{
		Model: "catalog-model", Messages: []loop.Message{{Role: loop.RoleUser, Content: "goal"}},
		Tools:           []loop.ToolDefinition{{Type: "function", Function: loop.FunctionMetadata{Name: "list_catalog", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		MaxOutputTokens: 128, ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if !observed || response.Message.Content != "done" || response.Usage == nil || !response.Usage.PromptTokensPresent || !response.Usage.CompletionTokensPresent || response.Usage.TotalTokensPresent {
		t.Fatalf("observed=%v decoded response=%+v", observed, response)
	}
}

func TestClientSupportsLocalSidecarJobPath(t *testing.T) {
	client := testClient(t, "http://localhost:8082/model/job-123/llm/served-model/v1/?routing=internal", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.URL.Path, "/model/job-123/llm/served-model/v1/chat/completions"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got := r.URL.Query().Get("routing"); got != "internal" {
			t.Errorf("routing query = %q", got)
		}
		return testResponse(r, http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`), nil
	}))
	if _, err := client.Complete(context.Background(), modelRequest()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestClientRejectsNonLoopbackAndMalformedEndpoints(t *testing.T) {
	for _, raw := range []string{
		"https://model.example/v1", "http://localhost.example/v1", "http://user:pass@localhost/v1",
		"ftp://127.0.0.1/v1", "http://127.0.0.1/v1#fragment", "/relative/v1", "http://127.0.0.1.evil/",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NewClient(Config{BaseURL: raw}); !errors.Is(err, ErrInvalidEndpoint) {
				t.Fatalf("NewClient() error = %v", err)
			}
		})
	}
	if _, err := NewClient(Config{BaseURL: "http://[::1]:8080/v1"}); err != nil {
		t.Fatalf("IPv6 loopback rejected: %v", err)
	}
	client := testClient(t, "http://LOCALHOST.:8082/v1", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.URL.Host, "localhost:8082"; got != want {
			t.Errorf("canonical endpoint host = %q, want %q", got, want)
		}
		return testResponse(r, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`), nil
	}))
	if _, err := client.Complete(context.Background(), modelRequest()); err != nil {
		t.Fatalf("Complete() with localhost alias: %v", err)
	}
}

func TestClientDoesNotUseCookiesOrEnvironmentProxy(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse("http://localhost:8082/v1")
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(endpoint, []*http.Cookie{{Name: "session", Value: "private"}})
	t.Setenv("HTTP_PROXY", "http://proxy.example:3128")
	t.Setenv("NO_PROXY", "")
	var cookieHeader string
	originalClient := &http.Client{
		Jar: jar,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			cookieHeader = r.Header.Get("Cookie")
			return testResponse(r, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`), nil
		}),
	}
	client, err := NewClient(Config{BaseURL: endpoint.String(), HTTPClient: originalClient})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), modelRequest()); err != nil {
		t.Fatalf("Complete() with cookie jar: %v", err)
	}
	if cookieHeader != "" {
		t.Fatalf("request Cookie header = %q, want empty", cookieHeader)
	}
	if originalClient.Jar != jar {
		t.Fatal("NewClient() mutated the supplied HTTP client's cookie jar")
	}

	originalTransport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	client, err = NewClient(Config{BaseURL: endpoint.String(), HTTPClient: &http.Client{Transport: originalTransport}})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport = %T, want *http.Transport", client.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("client transport still uses an environment proxy")
	}
	if originalTransport.Proxy == nil {
		t.Fatal("NewClient() mutated the supplied transport")
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var calls int
	client := testClient(t, "http://127.0.0.1:8080/v1", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		header := make(http.Header)
		header.Set("Location", "https://model.example/v1/chat/completions")
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
	_, err := client.Complete(context.Background(), modelRequest())
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("Complete() error = %v, want redirect status error", err)
	}
	if calls != 1 {
		t.Fatalf("transport received %d request(s), want only the loopback request", calls)
	}
}

func TestClientDoesNotExposeErrorBody(t *testing.T) {
	client := testClient(t, "http://127.0.0.1:8080", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return testResponse(r, http.StatusUnauthorized, "private-token and prompt contents"), nil
	}))
	_, err := client.Complete(context.Background(), loop.ModelRequest{Model: "m", Messages: []loop.Message{{Role: loop.RoleUser, Content: "private prompt"}}, MaxOutputTokens: 10})
	if err == nil || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "private prompt") {
		t.Fatalf("unsafe error = %v", err)
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusUnauthorized || statusErr.Retryable() {
		t.Fatalf("status error = %#v", err)
	}
}

func TestClientBoundsRequestAndResponseBodies(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		var calls int
		client, err := NewClient(Config{BaseURL: "http://127.0.0.1:8080", MaxRequestBytes: 64, HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, nil
		})}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Complete(context.Background(), loop.ModelRequest{Model: "m", Messages: []loop.Message{{Role: loop.RoleUser, Content: strings.Repeat("x", 1024)}}, MaxOutputTokens: 10})
		if !errors.Is(err, ErrRequestTooLarge) || calls != 0 {
			t.Fatalf("Complete() error=%v calls=%d", err, calls)
		}
	})
	t.Run("response", func(t *testing.T) {
		client, err := NewClient(Config{BaseURL: "http://127.0.0.1:8080", MaxResponseBytes: 100, HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return testResponse(r, http.StatusOK, strings.Repeat("x", 200)), nil
		})}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Complete(context.Background(), modelRequest())
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("Complete() error = %v", err)
		}
	})
}

func TestClientClassifiesRetryableStatuses(t *testing.T) {
	for code, want := range map[int]bool{http.StatusTooManyRequests: true, http.StatusBadGateway: true, http.StatusBadRequest: false} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client := testClient(t, "http://127.0.0.1:8080", roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return testResponse(r, code, ""), nil
			}))
			_, err := client.Complete(context.Background(), modelRequest())
			var statusErr *StatusError
			if !errors.As(err, &statusErr) || statusErr.Retryable() != want {
				t.Fatalf("error = %#v, want retryable=%v", err, want)
			}
		})
	}
}

func TestClientRejectsInvalidRequests(t *testing.T) {
	var nilClient *Client
	if _, err := nilClient.Complete(context.Background(), modelRequest()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("nil client error = %v", err)
	}

	client := testClient(t, "http://127.0.0.1:8080", roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached the transport")
		return nil, nil
	}))
	cases := []struct {
		name    string
		ctx     context.Context
		request loop.ModelRequest
	}{
		{name: "nil context", request: modelRequest()},
		{name: "blank model", ctx: context.Background(), request: loop.ModelRequest{MaxOutputTokens: 1}},
		{name: "no output allowance", ctx: context.Background(), request: loop.ModelRequest{Model: "m"}},
		{name: "invalid raw tool schema", ctx: context.Background(), request: loop.ModelRequest{
			Model: "m", MaxOutputTokens: 1,
			Tools: []loop.ToolDefinition{{Type: "function", Function: loop.FunctionMetadata{Name: "bad", Parameters: json.RawMessage("{")}}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := client.Complete(tc.ctx, tc.request); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("Complete() error = %v", err)
			}
		})
	}
}

func TestClientSanitizesTransportErrorsAndPropagatesCancellation(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		client := testClient(t, "http://127.0.0.1:8080", roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("private host and credential")
		}))
		_, err := client.Complete(context.Background(), modelRequest())
		if !errors.Is(err, ErrTransport) || strings.Contains(err.Error(), "private host") || strings.Contains(err.Error(), "credential") {
			t.Fatalf("transport error was not safely wrapped: %v", err)
		}
	})

	t.Run("request canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		client := testClient(t, "http://127.0.0.1:8080", roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport cancellation")
		}))
		if _, err := client.Complete(ctx, modelRequest()); !errors.Is(err, context.Canceled) {
			t.Fatalf("Complete() error = %v, want context cancellation", err)
		}
	})
}

func TestClientClassifiesResponseReadErrors(t *testing.T) {
	t.Run("transport failure", func(t *testing.T) {
		client := testClient(t, "http://127.0.0.1:8080", roundTripFunc(func(r *http.Request) (*http.Response, error) {
			response := testResponse(r, http.StatusOK, "")
			response.Body = failingReadCloser{}
			return response, nil
		}))
		_, err := client.Complete(context.Background(), modelRequest())
		var transportErr *TransportError
		if !errors.As(err, &transportErr) || !transportErr.Retryable() || errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("Complete() error = %v, want retryable transport error", err)
		}
	})

	t.Run("canceled while reading", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := testClient(t, "http://127.0.0.1:8080", roundTripFunc(func(r *http.Request) (*http.Response, error) {
			response := testResponse(r, http.StatusOK, "")
			response.Body = cancelingReadCloser{cancel: cancel}
			return response, nil
		}))
		_, err := client.Complete(ctx, modelRequest())
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrTransport) {
			t.Fatalf("Complete() error = %v, want context cancellation", err)
		}
	})
}

func TestDecodeResponseIncludesToolCallsAndAllUsageFields(t *testing.T) {
	response, err := decodeResponse([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"list_catalog","arguments":"{\"limit\":1}"}}]}}],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`))
	if err != nil {
		t.Fatalf("decodeResponse() error = %v", err)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].ID != "call-1" || response.Message.ToolCalls[0].Function.Name != "list_catalog" {
		t.Fatalf("decoded tool calls = %+v", response.Message.ToolCalls)
	}
	if response.FinishReason != "tool_calls" || response.Usage == nil || !response.Usage.TotalTokensPresent || response.Usage.TotalTokens != 13 {
		t.Fatalf("decoded response metadata = %+v", response)
	}
}

func TestDecodeResponseRejectsUnsupportedShapes(t *testing.T) {
	for _, body := range []string{
		`{`, `{"choices":[]}`, `{"choices":[{"message":{"role":"user","content":"x"}}]}`,
		`{"choices":[{"message":{"role":"assistant","content":["not", "a string"]}}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"x"}},{"message":{"role":"assistant","content":"y"}}]}`,
	} {
		if _, err := decodeResponse([]byte(body)); !errors.Is(err, ErrInvalidResponse) {
			t.Errorf("decodeResponse(%q) error = %v", body, err)
		}
	}
}

func TestStatusAndTransportErrorsAreRetryableAsExpected(t *testing.T) {
	if !(&StatusError{StatusCode: 503}).Retryable() || (&StatusError{StatusCode: 400}).Retryable() || !(&TransportError{}).Retryable() {
		t.Fatal("retry classification is incorrect")
	}
	if !errors.Is(&TransportError{}, ErrTransport) {
		t.Fatal("transport error does not unwrap to sentinel")
	}
	if !errors.Is(&StatusError{StatusCode: 401}, ErrHTTPStatus) {
		t.Fatal("status error does not unwrap to sentinel")
	}
	if !strings.Contains((&StatusError{StatusCode: 401}).Error(), "401") {
		t.Fatal("status error does not include its status")
	}
}

func testClient(t *testing.T, baseURL string, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := NewClient(Config{BaseURL: baseURL, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func testResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("private read failure") }
func (failingReadCloser) Close() error             { return nil }

type cancelingReadCloser struct{ cancel context.CancelFunc }

func (r cancelingReadCloser) Read([]byte) (int, error) {
	r.cancel()
	return 0, errors.New("private read failure")
}
func (cancelingReadCloser) Close() error { return nil }

func modelRequest() loop.ModelRequest {
	return loop.ModelRequest{Model: "m", Messages: []loop.Message{{Role: loop.RoleUser, Content: "x"}}, MaxOutputTokens: 10}
}
