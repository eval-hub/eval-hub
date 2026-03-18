package proxy

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
	// OCI registry auth (when TargetEndpoint == "oci")
	OCIAuthConfigPath string         // path to registry auth config file (OCI secret mount, same format as Docker config.json)
	OCIRepository     string         // optional scope repository (e.g. namespace/repo)
	OCITokenProducer  *TokenProducer // optional; when set, reused for token resolution instead of building from config path
}

const defaultAuthTokenCacheTTL = 5 * time.Minute

type authCacheEntry struct {
	token     string
	expiresAt time.Time
}

var (
	authTokenCache    = make(map[string]authCacheEntry)
	authTokenCacheMu  sync.RWMutex
	ociTokenRefreshMu sync.Mutex // guards GetToken() on the shared OCI TokenProducer
)

// ResolveAuthToken returns the auth token to use for a request.
// It switches on input.TargetEndpoint: eval-hub and mlflow use file/static token and cache;
// oci (URI contains repository name from job spec) uses OCI secret-mounted registry auth and invokes oci GetToken.
func ResolveAuthToken(logger *slog.Logger, input AuthTokenInput) string {
	switch input.TargetEndpoint {
	case "oci":
		return resolveOCIAuthToken(logger, input)
	default:
		return resolveEvalHubOrMLflowToken(logger, input)
	}
}

// resolveOCIAuthToken returns the OCI registry token using the shared TokenProducer created at sidecar startup.
// OCITokenProducer is always set when the OCI proxy is enabled (handlers pass it from parseProxyCall).
func resolveOCIAuthToken(logger *slog.Logger, input AuthTokenInput) string {
	if input.OCITokenProducer == nil {
		logger.Warn("OCI auth called without producer (should not happen in production)")
		return ""
	}
	return resolveOCIAuthTokenWithProducer(logger, input)
}

// resolveOCIAuthTokenWithProducer uses the shared TokenProducer created at sidecar startup.
func resolveOCIAuthTokenWithProducer(logger *slog.Logger, input AuthTokenInput) string {
	tp := input.OCITokenProducer
	cacheKey := "oci:" + tp.Registry + ":" + tp.Repository
	authTokenCacheMu.RLock()
	entry, ok := authTokenCache[cacheKey]
	authTokenCacheMu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.token
	}

	ociTokenRefreshMu.Lock()
	err := tp.GetToken()
	ociTokenRefreshMu.Unlock()
	if err != nil {
		logger.Error("OCI GetToken failed", "error", err)
		return ""
	}
	token := tp.Token
	if token != "" {
		ttl := input.TokenCacheTimeout
		if ttl <= 0 {
			ttl = defaultAuthTokenCacheTTL
		}
		authTokenCacheMu.Lock()
		authTokenCache[cacheKey] = authCacheEntry{token: token, expiresAt: time.Now().Add(ttl)}
		authTokenCacheMu.Unlock()
	}
	return token
}

// resolveEvalHubOrMLflowToken implements the original file/static token + cache behavior for eval-hub and mlflow.
func resolveEvalHubOrMLflowToken(logger *slog.Logger, input AuthTokenInput) string {
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

// cacheKeyForAuthInput returns the map key used for input in the auth token cache, or "" if not cacheable.
func cacheKeyForAuthInput(input AuthTokenInput) string {
	switch input.TargetEndpoint {
	case "oci":
		if input.OCITokenProducer == nil {
			return ""
		}
		tp := input.OCITokenProducer
		return "oci:" + tp.Registry + ":" + tp.Repository
	default:
		if input.TargetEndpoint == "" {
			return ""
		}
		return input.TargetEndpoint
	}
}

// UpdateAuthTokenCache stores token under the cache entry for input (same key as ResolveAuthToken).
// TTL is input.TokenCacheTimeout or defaultAuthTokenCacheTTL. An empty token removes the cache entry.
// For TargetEndpoint "oci", OCITokenProducer must be set to compute the key.
func UpdateAuthTokenCache(input AuthTokenInput, token string) {
	key := cacheKeyForAuthInput(input)
	if key == "" {
		return
	}
	authTokenCacheMu.Lock()
	defer authTokenCacheMu.Unlock()
	if token == "" {
		delete(authTokenCache, key)
		return
	}
	ttl := input.TokenCacheTimeout
	if ttl <= 0 {
		ttl = defaultAuthTokenCacheTTL
	}
	authTokenCache[key] = authCacheEntry{token: token, expiresAt: time.Now().Add(ttl)}
}
