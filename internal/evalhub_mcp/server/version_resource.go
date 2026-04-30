package server

import (
	"context"
	"log/slog"
	"runtime"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpLibraryPath = "github.com/modelcontextprotocol/go-sdk"

type VersionResponse struct {
	Version           string `json:"version"`
	GitHash           string `json:"git_hash"`
	BuildDate         string `json:"build_date"`
	GoVersion         string `json:"go_version"`
	OS                string `json:"os"`
	Arch              string `json:"arch"`
	MCPLibrary        string `json:"mcp_library"`
	MCPLibraryVersion string `json:"mcp_library_version"`
}

func registerVersionResource(srv *mcp.Server, info *ServerInfo, logger *slog.Logger) {
	srv.AddResource(&mcp.Resource{
		Name:        "server-version",
		Description: "Server version and build metadata",
		MIMEType:    "application/json",
		URI:         "evalhub://server/version",
	}, versionHandler(info, logger))
}

func versionHandler(info *ServerInfo, logger *slog.Logger) mcp.ResourceHandler {
	resp := VersionResponse{
		GoVersion:         runtime.Version(),
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		MCPLibrary:        mcpLibraryPath,
		MCPLibraryVersion: mcpLibraryVersion(),
	}
	if info != nil {
		resp.Version = info.Version
		resp.GitHash = info.Build
		resp.BuildDate = info.BuildDate
	}

	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		logger.Debug("reading resource", "uri", req.Params.URI)
		return jsonResult(resp)
	}
}

func mcpLibraryVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range bi.Deps {
		if dep.Path == mcpLibraryPath {
			return dep.Version
		}
	}
	return "unknown"
}
