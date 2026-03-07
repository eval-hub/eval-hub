package eval_hub

import (
	"os"
	"strings"
)

// ResolveAuthToken returns the auth token to use for a request.
// Token file (authTokenPath) takes precedence over a static token, supporting
// Kubernetes projected SA tokens that are rotated on disk by the kubelet.
// Falls back to the static authToken for local development.
func ResolveAuthToken(authTokenPath, authToken string) string {
	if authTokenPath != "" {
		tokenData, err := os.ReadFile(authTokenPath)
		if err == nil {
			if token := strings.TrimSpace(string(tokenData)); token != "" {
				return token
			}
		}
	}
	return authToken
}
