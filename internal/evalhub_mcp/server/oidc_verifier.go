package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

type oidcTokenVerifier struct {
	verifier *oidc.IDTokenVerifier
}

func newOIDCTokenVerifier(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*oidcTokenVerifier, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	issuerURL := strings.TrimSpace(cfg.OIDC.IssuerURL)
	if issuerURL == "" {
		return nil, fmt.Errorf("oidc issuer URL is required")
	}

	httpClient := http.DefaultClient
	if cfg.Insecure {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // dev/self-signed IdP TLS
			},
		}
	}

	ctx = oidc.ClientContext(ctx, httpClient)
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}

	oidcCfg := &oidc.Config{}
	audience := strings.TrimSpace(cfg.OIDC.Audience)
	if audience != "" {
		oidcCfg.ClientID = audience
	} else {
		oidcCfg.SkipClientIDCheck = true
	}

	if logger != nil {
		logger.Info("OIDC bearer token verification enabled",
			"issuer", issuerURL,
			"audience", audience,
		)
	}

	return &oidcTokenVerifier{verifier: provider.Verifier(oidcCfg)}, nil
}

func (v *oidcTokenVerifier) verify(ctx context.Context, rawToken string, _ *http.Request) (*auth.TokenInfo, error) {
	if v == nil || v.verifier == nil {
		return nil, fmt.Errorf("oidc verifier is not configured")
	}

	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, auth.ErrInvalidToken
	}

	var claims tokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, auth.ErrInvalidToken
	}

	exp := idToken.Expiry
	if exp.IsZero() {
		return nil, auth.ErrInvalidToken
	}

	userID := strings.TrimSpace(claims.Subject)
	if userID == "" {
		return nil, auth.ErrInvalidToken
	}

	return &auth.TokenInfo{
		UserID:     userID,
		Expiration: exp,
		Scopes:     tokenScopes(claims),
	}, nil
}

type tokenClaims struct {
	Subject string   `json:"sub"`
	Scope   string   `json:"scope"`
	Scopes  []string `json:"scp"`
}

func tokenScopes(claims tokenClaims) []string {
	if scopes := strings.Fields(strings.TrimSpace(claims.Scope)); len(scopes) > 0 {
		return scopes
	}
	return append([]string(nil), claims.Scopes...)
}
