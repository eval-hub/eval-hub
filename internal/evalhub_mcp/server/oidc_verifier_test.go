package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

func TestTokenScopes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		claim tokenClaims
		want  []string
	}{
		{
			name:  "scope string",
			claim: tokenClaims{Scope: "read write"},
			want:  []string{"read", "write"},
		},
		{
			name:  "scp array",
			claim: tokenClaims{Scopes: []string{"mcp:read"}},
			want:  []string{"mcp:read"},
		},
		{
			name:  "empty",
			claim: tokenClaims{},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tokenScopes(tt.claim)
			if len(got) != len(tt.want) {
				t.Fatalf("tokenScopes() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("tokenScopes() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestOIDCTokenVerifierVerify(t *testing.T) {
	t.Parallel()

	issuer, audience, signToken := newTestOIDCIssuer(t)

	cfg := &config.Config{
		AuthType: config.AuthTypeOIDC,
		OIDC: config.OIDCConfig{
			IssuerURL: issuer,
			Audience:  audience,
		},
	}

	verifier, err := newOIDCTokenVerifier(context.Background(), cfg, discardLogger)
	if err != nil {
		t.Fatalf("newOIDCTokenVerifier: %v", err)
	}

	validToken := signToken(t, testTokenParams{
		subject:   "alice",
		audience:  audience,
		expiresIn: time.Hour,
		scope:     "read write",
	})

	info, err := verifier.verify(context.Background(), validToken, nil)
	if err != nil {
		t.Fatalf("verify valid token: %v", err)
	}
	if info.UserID != "alice" {
		t.Errorf("UserID = %q, want alice", info.UserID)
	}
	if info.Expiration.Before(time.Now()) {
		t.Errorf("Expiration %v is in the past", info.Expiration)
	}
	if len(info.Scopes) != 2 || info.Scopes[0] != "read" || info.Scopes[1] != "write" {
		t.Errorf("Scopes = %v, want [read write]", info.Scopes)
	}

	wrongAudience := signToken(t, testTokenParams{
		subject:   "alice",
		audience:  "other-client",
		expiresIn: time.Hour,
	})
	if _, err := verifier.verify(context.Background(), wrongAudience, nil); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("verify wrong audience: got %v, want ErrInvalidToken", err)
	}

	expired := signToken(t, testTokenParams{
		subject:   "alice",
		audience:  audience,
		expiresIn: -time.Hour,
	})
	if _, err := verifier.verify(context.Background(), expired, nil); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("verify expired token: got %v, want ErrInvalidToken", err)
	}

	if _, err := verifier.verify(context.Background(), "not-a-jwt", nil); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("verify garbage token: got %v, want ErrInvalidToken", err)
	}
}

type testTokenParams struct {
	subject   string
	audience  string
	expiresIn time.Duration
	scope     string
}

func newTestOIDCIssuer(t *testing.T) (issuerURL, audience string, sign func(t *testing.T, params testTokenParams) string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	const kid = "test-key"
	const audienceValue = "evalhub-mcp"

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	issuerURL = server.URL
	jwksURL := server.URL + "/keys"

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuerURL,
			"jwks_uri":               jwksURL,
			"authorization_endpoint": issuerURL + "/authorize",
			"token_endpoint":         issuerURL + "/token",
		})
	})

	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{rsaPublicJWK(&key.PublicKey, kid)},
		})
	})

	sign = func(t *testing.T, params testTokenParams) string {
		t.Helper()
		now := time.Now()
		claims := jwt.MapClaims{
			"iss":   issuerURL,
			"sub":   params.subject,
			"aud":   params.audience,
			"iat":   now.Unix(),
			"exp":   now.Add(params.expiresIn).Unix(),
			"scope": params.scope,
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = kid
		signed, err := token.SignedString(key)
		if err != nil {
			t.Fatalf("SignedString: %v", err)
		}
		return signed
	}

	return issuerURL, audienceValue, sign
}

func rsaPublicJWK(key *rsa.PublicKey, kid string) map[string]any {
	nBytes := key.N.Bytes()
	nLen := (key.N.BitLen() + 7) / 8
	if len(nBytes) < nLen {
		padded := make([]byte, nLen)
		copy(padded[nLen-len(nBytes):], nBytes)
		nBytes = padded
	}

	eBytes := big.NewInt(int64(key.E)).Bytes()

	return map[string]any{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(nBytes),
		"e":   base64.RawURLEncoding.EncodeToString(eBytes),
	}
}

func TestWrapRequestBearerTokenRequiresAuthorization(t *testing.T) {
	t.Parallel()

	issuer, audience, signToken := newTestOIDCIssuer(t)
	cfg := &config.Config{
		AuthType: config.AuthTypeOIDC,
		OIDC: config.OIDCConfig{
			IssuerURL: issuer,
			Audience:  audience,
		},
	}
	verifier, err := newOIDCTokenVerifier(context.Background(), cfg, discardLogger)
	if err != nil {
		t.Fatalf("newOIDCTokenVerifier: %v", err)
	}

	handler := wrapRequest(cfg, verifier.verify, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("missing bearer token", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("valid bearer token", func(t *testing.T) {
		t.Parallel()
		token := signToken(t, testTokenParams{
			subject:   "bob",
			audience:  audience,
			expiresIn: time.Hour,
		})
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusNoContent, rec.Body.String())
		}
	})
}

func TestWrapRequestRBACProxyRequiresHeaders(t *testing.T) {
	t.Parallel()

	handler := wrapRequest(&config.Config{AuthType: config.AuthTypeRBACProxy}, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
