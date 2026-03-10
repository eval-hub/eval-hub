package eval_hub

import (
	"log/slog"
	"os"
	"strings"
)

// ResolveAuthToken returns the auth token to use for a request.
// Token file (authTokenPath) takes precedence over a static token, supporting
// Kubernetes projected SA tokens that are rotated on disk by the kubelet.
// Falls back to the static authToken for local development.
func ResolveAuthToken(logger *slog.Logger, authTokenPath, authToken string) string {
	if authTokenPath != "" {
		tokenData, err := os.ReadFile(authTokenPath)
		if err == nil {
			logger.Info("Read auth token from file", "path", authTokenPath)
			if token := strings.TrimSpace(string(tokenData)); token != "" {
				logger.Debug("Auth token", "token", token)
				return token
			}
		}
	}
	logger.Debug("Using static auth token", "token", authToken)
	return authToken
}
