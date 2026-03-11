package common

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

type AuthTokenInput struct {
	TargetEndpoint    string
	AuthTokenPath     string
	AuthToken         string
	TokenCacheTimeout time.Duration
}

const defaultAuthTokenCacheTTL = 5 * time.Minute

type authCacheEntry struct {
	token     string
	expiresAt time.Time
}

var (
	authTokenCache   = make(map[string]authCacheEntry)
	authTokenCacheMu sync.RWMutex
)

// ResolveAuthToken returns the auth token to use for a request.
// It uses an in-memory cache keyed by targetEndpoint; cached entries expire
// after the given TTL (or defaultAuthTokenCacheTTL if ttl is 0).
// Token file (authTokenPath) takes precedence over a static token, supporting
// Kubernetes projected SA tokens that are rotated on disk by the kubelet.
// Falls back to the static authToken for local development.
func ResolveAuthToken(logger *slog.Logger, input AuthTokenInput) string {
	if input.TargetEndpoint != "" {
		authTokenCacheMu.RLock()
		entry, ok := authTokenCache[input.TargetEndpoint]
		authTokenCacheMu.RUnlock()
		if ok && time.Now().Before(entry.expiresAt) {
			return entry.token
		}
	}

	token := input.AuthToken
	if input.AuthTokenPath != "" {
		tokenData, err := os.ReadFile(input.AuthTokenPath)
		if err == nil {
			logger.Info("Read auth token from file", "path", input.AuthTokenPath)
			if t := strings.TrimSpace(string(tokenData)); t != "" {
				token = t
			}
		}
	}

	if input.TargetEndpoint != "" && token != "" {
		if input.TokenCacheTimeout <= 0 {
			input.TokenCacheTimeout = defaultAuthTokenCacheTTL
		}
		authTokenCacheMu.Lock()
		authTokenCache[input.TargetEndpoint] = authCacheEntry{token: token, expiresAt: time.Now().Add(input.TokenCacheTimeout)}
		authTokenCacheMu.Unlock()
	}

	return token
}
