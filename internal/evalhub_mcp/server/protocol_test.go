package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testProtocolVersion = "2026-07-28"

type protocolResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *jsonrpc.Error  `json:"error"`
}

func protocolServer(t *testing.T) *mcp.Server {
	t.Helper()
	ds := testDataSource()
	info := &ServerInfo{Build: "protocol-test"}
	srv := New(info, discardLogger, CompletionHandlerOption(ds, discardLogger, evalhubclient.DefaultListPageLimit))
	registerVersionResource(srv, info, discardLogger)
	registerResources(srv, ds, discardLogger, evalhubclient.DefaultListPageLimit)
	if err := registerTools(srv, &mockToolClient{listProviderFn: ds.ListProviders, getProviderFn: ds.GetProvider}, ds, discardLogger); err != nil {
		t.Fatal(err)
	}
	if err := registerPrompts(srv, ds, discardLogger); err != nil {
		t.Fatal(err)
	}
	return srv
}

func protocolParams() map[string]any {
	return map[string]any{
		"_meta": map[string]any{
			mcp.MetaKeyProtocolVersion:    testProtocolVersion,
			mcp.MetaKeyClientCapabilities: map[string]any{},
			mcp.MetaKeyClientInfo:         map[string]any{"name": "protocol-test", "version": "1"},
		},
	}
}

// postProtocol deliberately bypasses SDK discovery and fallback. Every call is
// independent, with no initialize exchange or HTTP session ID.
func postProtocol(t *testing.T, endpoint, method string, params map[string]any, headers map[string]string) (*http.Response, protocolResponse) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", testProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	if name, ok := params["name"].(string); ok {
		req.Header.Set("Mcp-Name", name)
	} else if uri, ok := params["uri"].(string); ok {
		req.Header.Set("Mcp-Name", uri)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var wire protocolResponse
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
				if err := json.Unmarshal([]byte(data), &wire); err != nil {
					t.Fatal(err)
				}
				return resp, wire
			}
		}
		t.Fatalf("missing SSE result: %v", scanner.Err())
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	// The authentication wrapper returns plain text before invoking MCP.
	if resp.StatusCode != http.StatusForbidden {
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("status %d: %s: %v", resp.StatusCode, data, err)
		}
	}
	return resp, wire
}

func TestHTTPProtocolWithoutInitialization(t *testing.T) {
	t.Parallel()
	httpServer := httptest.NewServer(NewHTTPHandler(protocolServer(t), &config.Config{AuthType: config.AuthTypeNone}, discardLogger))
	t.Cleanup(httpServer.Close)
	tests := []struct {
		method string
		params map[string]any
		want   string
	}{
		{"server/discover", nil, `"2026-07-28"`},
		{"tools/list", nil, `"discover_providers"`},
		{"resources/list", nil, `"evalhub://server/version"`},
		{"resources/templates/list", nil, `"evalhub://providers/{id}"`},
		{"prompts/list", nil, `"evaluate_model"`},
		{"tools/call", map[string]any{"name": "discover_providers", "arguments": map[string]any{}}, `"lighteval"`},
		{"resources/read", map[string]any{"uri": "evalhub://server/version"}, "protocol-test"},
		{"prompts/get", map[string]any{"name": "edd_workflow", "arguments": map[string]string{"application_type": "rag"}}, `"messages"`},
		{"completion/complete", map[string]any{"ref": map[string]any{"type": "ref/resource", "uri": "evalhub://providers/{id}"}, "argument": map[string]string{"name": "id", "value": "light"}}, `"lighteval"`},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			params := protocolParams()
			for name, value := range tt.params {
				params[name] = value
			}
			resp, wire := postProtocol(t, httpServer.URL, tt.method, params, nil)
			if resp.StatusCode != http.StatusOK || wire.Error != nil {
				t.Fatalf("status %d, error %v", resp.StatusCode, wire.Error)
			}
			if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
				t.Errorf("unexpected HTTP session ID %q", got)
			}
			if !bytes.Contains(wire.Result, []byte(tt.want)) {
				t.Errorf("result %s missing %s", wire.Result, tt.want)
			}
			if tt.method == "server/discover" {
				var discovery mcp.DiscoverResult
				if err := json.Unmarshal(wire.Result, &discovery); err != nil {
					t.Fatal(err)
				}
				caps := discovery.Capabilities
				if !slices.Contains(discovery.SupportedVersions, testProtocolVersion) || caps == nil ||
					caps.Tools == nil || caps.Resources == nil || caps.Prompts == nil || caps.Completions == nil {
					t.Fatalf("unexpected discovery result: %s", wire.Result)
				}
			}
			var result struct {
				Meta map[string]json.RawMessage `json:"_meta"`
			}
			if err := json.Unmarshal(wire.Result, &result); err != nil {
				t.Fatal(err)
			}
			var info mcp.Implementation
			if err := json.Unmarshal(result.Meta[mcp.MetaKeyServerInfo], &info); err != nil {
				t.Fatal(err)
			}
			if info.Name != "evalhub-mcp" || info.Version != "protocol-test" {
				t.Errorf("server metadata = %+v", info)
			}
		})
	}
}

func TestHTTPProtocolErrors(t *testing.T) {
	t.Parallel()
	httpServer := httptest.NewServer(NewHTTPHandler(protocolServer(t), &config.Config{AuthType: config.AuthTypeNone}, discardLogger))
	t.Cleanup(httpServer.Close)
	tests := []struct {
		name    string
		method  string
		meta    map[string]any
		headers map[string]string
		code    int64
	}{
		{"missing protocol version", "tools/list", map[string]any{mcp.MetaKeyClientCapabilities: map[string]any{}}, nil, jsonrpc.CodeInvalidParams},
		{"missing capabilities", "tools/list", map[string]any{mcp.MetaKeyProtocolVersion: testProtocolVersion}, nil, jsonrpc.CodeInvalidParams},
		{"invalid capabilities", "tools/list", map[string]any{mcp.MetaKeyProtocolVersion: testProtocolVersion, mcp.MetaKeyClientCapabilities: "invalid"}, nil, jsonrpc.CodeInvalidParams},
		{"invalid client identity", "tools/list", map[string]any{mcp.MetaKeyProtocolVersion: testProtocolVersion, mcp.MetaKeyClientCapabilities: map[string]any{}, mcp.MetaKeyClientInfo: "invalid"}, nil, jsonrpc.CodeInvalidParams},
		{"unsupported version", "tools/list", map[string]any{mcp.MetaKeyProtocolVersion: "2099-01-01", mcp.MetaKeyClientCapabilities: map[string]any{}}, map[string]string{"MCP-Protocol-Version": "2099-01-01"}, mcp.CodeUnsupportedProtocolVersion},
		{"method mismatch", "tools/list", nil, map[string]string{"Mcp-Method": "resources/list"}, mcp.CodeHeaderMismatch},
		{"name mismatch", "resources/read", nil, map[string]string{"Mcp-Name": "evalhub://providers"}, mcp.CodeHeaderMismatch},
		{"version mismatch", "tools/list", nil, map[string]string{"MCP-Protocol-Version": "2025-11-25"}, mcp.CodeHeaderMismatch},
		{"removed ping", "ping", nil, nil, jsonrpc.CodeMethodNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := protocolParams()
			if tt.method == "resources/read" {
				params["uri"] = "evalhub://server/version"
			}
			if tt.meta != nil {
				params["_meta"] = tt.meta
			}
			_, wire := postProtocol(t, httpServer.URL, tt.method, params, tt.headers)
			if wire.Error == nil || wire.Error.Code != tt.code {
				t.Fatalf("error = %v, want code %d", wire.Error, tt.code)
			}
		})
	}
}

func TestHTTPProtocolRequiresIdentityOnEveryRequest(t *testing.T) {
	t.Parallel()
	httpServer := httptest.NewServer(NewHTTPHandler(protocolServer(t), &config.Config{AuthType: config.AuthTypeRBACProxy}, discardLogger))
	t.Cleanup(httpServer.Close)
	for _, method := range []string{"server/discover", "tools/list"} {
		for _, headers := range []map[string]string{
			{TenantHeader: "tenant-a", UserHeader: "alice", TransactionIDHeader: "request-a"},
			{TenantHeader: "tenant-a"},
			{UserHeader: "alice"},
			{},
			{TenantHeader: "tenant-b", UserHeader: "bob", TransactionIDHeader: "request-b"},
		} {
			resp, wire := postProtocol(t, httpServer.URL, method, protocolParams(), headers)
			if headers[TenantHeader] == "" || headers[UserHeader] == "" {
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("%s without identity: status %d", method, resp.StatusCode)
				}
			} else {
				if resp.StatusCode != http.StatusOK || wire.Error != nil {
					t.Fatalf("%s with identity: status %d, error %v", method, resp.StatusCode, wire.Error)
				}
				if got := resp.Header.Get(TransactionIDHeader); got != headers[TransactionIDHeader] {
					t.Errorf("transaction ID = %q, want %q", got, headers[TransactionIDHeader])
				}
			}
		}
	}
}

func TestHTTPProtocolCompatibility(t *testing.T) {
	t.Parallel()
	for _, version := range mcp.SupportedProtocolVersions() {
		t.Run(version, func(t *testing.T) {
			httpServer := httptest.NewServer(NewHTTPHandler(protocolServer(t), &config.Config{AuthType: config.AuthTypeNone}, discardLogger))
			t.Cleanup(httpServer.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := mcp.NewClient(&mcp.Implementation{Name: "compatibility-test", Version: "1"}, nil)
			cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cs.Close() }()
			if got := cs.InitializeResult().ProtocolVersion; got != version {
				t.Fatalf("protocol version = %q, want %q", got, version)
			}
			result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://server/version"})
			if err != nil || len(result.Contents) != 1 {
				t.Fatalf("ReadResource: result %v, error %v", result, err)
			}
		})
	}
}

func TestLegacySSEProtocolCompatibility(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := &config.Config{
		Transport: config.TransportHTTPSSE,
		AuthType:  config.AuthTypeNone,
		Host:      "127.0.0.1",
		Port:      freePort(t),
	}
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, cfg, &ServerInfo{Build: "legacy-test"}, discardLogger) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("legacy SSE shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("legacy SSE did not stop")
		}
	})
	waitForPort(t, cfg.Host, cfg.Port, 3*time.Second)
	client := mcp.NewClient(&mcp.Implementation{Name: "legacy-test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.SSEClientTransport{Endpoint: fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port)}, &mcp.ClientSessionOptions{ProtocolVersion: "2024-11-05"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	if got := cs.InitializeResult().ProtocolVersion; got != "2024-11-05" {
		t.Fatalf("legacy protocol = %q, want 2024-11-05", got)
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://server/version"}); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolCatalogsAndCaching(t *testing.T) {
	t.Parallel()
	ctx, cs := connectServer(t, &ServerInfo{Build: "test"})
	caps := cs.InitializeResult().Capabilities
	if caps.Tools.ListChanged || caps.Resources.ListChanged || caps.Prompts.ListChanged {
		t.Fatal("static catalogs must not advertise change notifications")
	}
	srv := protocolServer(t)
	cs = connectClient(t, ctx, srv)
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(tools.Tools))
	for i, tool := range tools.Tools {
		names[i] = tool.Name
	}
	if !slices.IsSorted(names) {
		t.Errorf("catalog ordering = %v", names)
	}
	resource, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://jobs"})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []mcp.CacheableResult{tools, resource} {
		if result.GetTTLMs() != 0 || result.GetCacheScope() != "private" {
			t.Errorf("cache hints: TTL %d, scope %q", result.GetTTLMs(), result.GetCacheScope())
		}
	}
}

func TestMCPStdioProcess(t *testing.T) {
	if os.Getenv("EVALHUB_MCP_STDIO_TEST") != "1" {
		return
	}
	srv := New(&ServerInfo{Build: "stdio-test"}, discardLogger, nil)
	registerVersionResource(srv, &ServerInfo{Build: "stdio-test"}, discardLogger)
	if err := runStdio(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
}

func TestStdioProtocolCompatibility(t *testing.T) {
	t.Parallel()
	for _, version := range []string{testProtocolVersion, "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioProcess$")
			cmd.Env = append(os.Environ(), "EVALHUB_MCP_STDIO_TEST=1")
			client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
			cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cs.Close() }()
			if got := cs.InitializeResult().ProtocolVersion; got != version {
				t.Fatalf("protocol = %q, want %q", got, version)
			}
			if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "evalhub://server/version"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
