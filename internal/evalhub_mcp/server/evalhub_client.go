package server

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

// scopedEvalHubClient returns a copy of client configured with the MCP request context,
// including request-scoped logging and X-Global-Transaction-Id propagation.
func scopedEvalHubClient(ctx context.Context, client *evalhubclient.Client, fallback *slog.Logger) *evalhubclient.Client {
	if client == nil {
		return nil
	}
	reqCtx := evalhubclient.ContextWithLogger(ctx, requestLogger(ctx, fallback))
	if id := RequestIDFromContext(ctx); id != "" {
		reqCtx = evalhubclient.ContextWithRequestID(reqCtx, id)
	}
	return client.WithContext(reqCtx)
}

func evalHubToolClientForRequest(ctx context.Context, client EvalHubToolClient, fallback *slog.Logger) EvalHubToolClient {
	if c, ok := client.(*evalhubclient.Client); ok {
		return scopedEvalHubClient(ctx, c, fallback)
	}
	return client
}

func evalHubDiscoveryForRequest(ctx context.Context, ds EvalHubDiscovery, fallback *slog.Logger) EvalHubDiscovery {
	if c, ok := ds.(*evalhubclient.Client); ok {
		return scopedEvalHubClient(ctx, c, fallback)
	}
	return ds
}
