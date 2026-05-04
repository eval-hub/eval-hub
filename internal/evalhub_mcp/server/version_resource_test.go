package server

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- test helpers ---

func connectClient(t *testing.T, ctx context.Context, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

func connectWithVersion(t *testing.T, info *ServerInfo) (context.Context, *mcp.ClientSession) {
	t.Helper()

	srv := New(info, discardLogger)
	registerVersionResource(srv, info, discardLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	return ctx, connectClient(t, ctx, srv)
}

// --- version resource ---

func TestVersionResourceJSONStructure(t *testing.T) {
	t.Parallel()

	info := &ServerInfo{
		Version:   "0.4.0",
		Build:     "abc123",
		BuildDate: "2026-04-30T10:00:00Z",
	}

	ctx, cs := connectWithVersion(t, info)

	resp := readResourceJSON[VersionResponse](t, ctx, cs, "evalhub://server/version")

	if resp.Version == "" {
		t.Error("version field is empty")
	}
	if resp.GoVersion == "" {
		t.Error("go_version field is empty")
	}
	if resp.OS == "" {
		t.Error("os field is empty")
	}
	if resp.Arch == "" {
		t.Error("arch field is empty")
	}
	if resp.MCPLibrary == "" {
		t.Error("mcp_library field is empty")
	}
	if resp.MCPLibraryVersion == "" {
		t.Error("mcp_library_version field is empty")
	}
}

func TestVersionResourceMatchesBuildValues(t *testing.T) {
	t.Parallel()

	info := &ServerInfo{
		Version:   "1.2.3",
		Build:     "deadbeef",
		BuildDate: "2026-01-15T12:00:00Z",
	}

	ctx, cs := connectWithVersion(t, info)

	resp := readResourceJSON[VersionResponse](t, ctx, cs, "evalhub://server/version")

	if resp.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", resp.Version, "1.2.3")
	}
	if resp.GitHash != "deadbeef" {
		t.Errorf("git_hash = %q, want %q", resp.GitHash, "deadbeef")
	}
	if resp.BuildDate != "2026-01-15T12:00:00Z" {
		t.Errorf("build_date = %q, want %q", resp.BuildDate, "2026-01-15T12:00:00Z")
	}
}

func TestVersionResourceMatchesRuntime(t *testing.T) {
	t.Parallel()

	info := &ServerInfo{Version: "0.1.0"}

	ctx, cs := connectWithVersion(t, info)

	resp := readResourceJSON[VersionResponse](t, ctx, cs, "evalhub://server/version")

	if resp.GoVersion != runtime.Version() {
		t.Errorf("go_version = %q, want %q", resp.GoVersion, runtime.Version())
	}
	if resp.OS != runtime.GOOS {
		t.Errorf("os = %q, want %q", resp.OS, runtime.GOOS)
	}
	if resp.Arch != runtime.GOARCH {
		t.Errorf("arch = %q, want %q", resp.Arch, runtime.GOARCH)
	}
}

func TestVersionResourceAvailableWithoutBackend(t *testing.T) {
	t.Parallel()

	info := &ServerInfo{Version: "0.1.0"}
	srv := New(info, discardLogger)
	RegisterHandlers(srv, nil, info, discardLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	clientSession := connectClient(t, ctx, srv)

	result, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}

	found := false
	for _, r := range result.Resources {
		if r.URI == "evalhub://server/version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("server/version resource not found when backend is unreachable")
	}

	resp := readResourceJSON[VersionResponse](t, ctx, clientSession, "evalhub://server/version")
	if resp.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", resp.Version, "0.1.0")
	}
}

func TestVersionResourceInResourcesList(t *testing.T) {
	t.Parallel()

	info := &ServerInfo{Version: "0.3.0", Build: "test"}

	ctx, cs := connectWithVersion(t, info)

	result, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}

	found := false
	for _, r := range result.Resources {
		if r.URI == "evalhub://server/version" {
			found = true
			if r.MIMEType != "application/json" {
				t.Errorf("MIME type = %q, want %q", r.MIMEType, "application/json")
			}
			if r.Name != "server-version" {
				t.Errorf("name = %q, want %q", r.Name, "server-version")
			}
			break
		}
	}
	if !found {
		t.Error("server/version resource not found in resources/list")
	}
}

func TestVersionResourceNilInfo(t *testing.T) {
	t.Parallel()

	srv := New(nil, discardLogger)
	registerVersionResource(srv, nil, discardLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	clientSession := connectClient(t, ctx, srv)

	resp := readResourceJSON[VersionResponse](t, ctx, clientSession, "evalhub://server/version")
	if resp.Version != "" {
		t.Errorf("version = %q, want empty string for nil info", resp.Version)
	}
	if resp.GoVersion == "" {
		t.Error("go_version should still be populated from runtime")
	}
}
