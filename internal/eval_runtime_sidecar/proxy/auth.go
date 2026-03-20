package proxy

import (
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type AuthTokenInput struct {
	TargetEndpoint    string
	AuthTokenPath     string
	AuthToken         string
	TokenCacheTimeout time.Duration
	// OCI registry auth (when TargetEndpoint == "oci")
	OCIAuthConfigPath string            // path to registry auth config file (OCI secret mount, same format as Docker config.json)
	OCIRepository     string            // optional scope repository (e.g. namespace/repo)
	OCITokenProducer  *OCITokenProducer // optional; when set, reused for token resolution instead of building from config path
}

const defaultAuthTokenCacheTTL = 5 * time.Minute

// TokenWithExpiry holds a token and when it should be treated as stale for caching.
type TokenWithExpiry struct {
	Token     string
	ExpiresAt time.Time
}

var (
	evalHubCachedToken atomic.Pointer[TokenWithExpiry]
	mlflowCachedToken  atomic.Pointer[TokenWithExpiry]
	ociCachedToken     atomic.Pointer[TokenWithExpiry]
)

// getTokenPointer returns the cache slot for a known proxy target, or nil if the endpoint is not cacheable.
func getTokenPointer(targetEndpoint string) *atomic.Pointer[TokenWithExpiry] {
	switch targetEndpoint {
	case "eval-hub":
		return &evalHubCachedToken
	case "mlflow":
		return &mlflowCachedToken
	case "oci":
		return &ociCachedToken
	default:
		return nil
	}
}

// ResolveAuthToken returns the auth token to use for a request.
// It switches on input.TargetEndpoint: eval-hub and mlflow use file/static token and cache;
// oci (URI contains repository name from job spec) uses OCI secret-mounted registry auth and invokes oci RefreshToken.
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
	// getTokenPointer("oci") is always non-nil (address of ociCachedToken).
	ociCache := getTokenPointer("oci")
	if entry := ociCache.Load(); entry != nil && time.Now().Before(entry.ExpiresAt) {
		return entry.Token
	}

	err := tp.RefreshToken()
	if err != nil {
		logger.Error("OCI RefreshToken failed", "error", err)
		return ""
	}
	token := tp.GetToken()
	if token != "" {
		ttl := input.TokenCacheTimeout
		if ttl <= 0 {
			ttl = defaultAuthTokenCacheTTL
		}
		ociCache.Store(&TokenWithExpiry{Token: token, ExpiresAt: time.Now().Add(ttl)})
	}
	return token
}

// resolveEvalHubOrMLflowToken implements the original file/static token + cache behavior for eval-hub and mlflow.
func resolveEvalHubOrMLflowToken(logger *slog.Logger, input AuthTokenInput) string {
	tokenPointer := getTokenPointer(input.TargetEndpoint)
	if tokenPointer != nil {
		if entry := tokenPointer.Load(); entry != nil && time.Now().Before(entry.ExpiresAt) {
			return entry.Token
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

	if tokenPointer != nil && token != "" {
		ttl := input.TokenCacheTimeout
		if ttl <= 0 {
			ttl = defaultAuthTokenCacheTTL
		}
		tokenPointer.Store(&TokenWithExpiry{Token: token, ExpiresAt: time.Now().Add(ttl)})
	}

	return token
}

// UpdateCachedToken stores token for the target in input.TargetEndpoint (eval-hub, mlflow, or oci).
// TTL is input.TokenCacheTimeout or defaultAuthTokenCacheTTL. An empty token clears the cache slot.
func UpdateCachedToken(input AuthTokenInput, token string) {
	tokenPointer := getTokenPointer(input.TargetEndpoint)
	if tokenPointer == nil {
		return
	}
	if token == "" {
		tokenPointer.Store(nil)
		return
	}
	ttl := input.TokenCacheTimeout
	if ttl <= 0 {
		ttl = defaultAuthTokenCacheTTL
	}
	tokenPointer.Store(&TokenWithExpiry{Token: token, ExpiresAt: time.Now().Add(ttl)})
}
