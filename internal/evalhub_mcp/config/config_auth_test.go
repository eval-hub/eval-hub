package config

import "testing"

func TestDefaultConfigAuthType(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	if cfg.AuthType != AuthTypeNone {
		t.Errorf("expected default auth_type %q, got %q", AuthTypeNone, cfg.AuthType)
	}
}

func TestValidateBearerTokenRequiresOIDCIssuer(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Transport: "http",
		Host:      "localhost",
		Port:      3001,
		AuthType:  AuthTypeOIDC,
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation error when oidc.issuer_url is missing")
	}
}

func TestValidateBearerTokenWithOIDC(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Transport: "http",
		Host:      "localhost",
		Port:      3001,
		AuthType:  AuthTypeOIDC,
		OIDC: OIDCConfig{
			IssuerURL: "https://auth.example.com",
			Audience:  "evalhub-mcp",
			Scopes:    []string{"read"},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateAuthType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "none",
			cfg:     &Config{Transport: "stdio", Host: "localhost", AuthType: AuthTypeNone},
			wantErr: false,
		},
		{
			name:    "rbac-proxy",
			cfg:     &Config{Transport: "http", Host: "localhost", Port: 3001, AuthType: AuthTypeRBACProxy},
			wantErr: false,
		},
		{
			name:    "bearer missing issuer",
			cfg:     &Config{Transport: "http", Host: "localhost", Port: 3001, AuthType: AuthTypeOIDC},
			wantErr: true,
		},
		{
			name: "bearer with issuer",
			cfg: &Config{
				Transport: "http",
				Host:      "localhost",
				Port:      3001,
				AuthType:  AuthTypeOIDC,
				OIDC:      OIDCConfig{IssuerURL: "https://auth.example.com"},
			},
			wantErr: false,
		},
		{
			name:    "invalid auth type",
			cfg:     &Config{Transport: "stdio", Host: "localhost", AuthType: "standalone"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadFlagsAuthTypeOverride(t *testing.T) {
	clearEnv(t)
	defer clearEnv(t)

	configFile := writeConfig(t, `
    auth_type: none
`)
	authType := AuthTypeRBACProxy
	cfg, err := Load(&Flags{ConfigPath: configFile, AuthType: &authType}, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AuthType != AuthTypeRBACProxy {
		t.Errorf("AuthType = %q, want %q", cfg.AuthType, AuthTypeRBACProxy)
	}
}

func TestLoadEnvAuthTypeAndOIDC(t *testing.T) {
	clearEnv(t)
	defer clearEnv(t)
	t.Setenv("EVALHUB_AUTH_TYPE", AuthTypeOIDC)
	t.Setenv("EVALHUB_OIDC_ISSUER_URL", "https://auth.example.com")
	t.Setenv("EVALHUB_OIDC_AUDIENCE", "evalhub-mcp")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AuthType != AuthTypeOIDC {
		t.Errorf("AuthType = %q, want %q", cfg.AuthType, AuthTypeOIDC)
	}
	if cfg.OIDC.IssuerURL != "https://auth.example.com" {
		t.Errorf("OIDC.IssuerURL = %q", cfg.OIDC.IssuerURL)
	}
	if cfg.OIDC.Audience != "evalhub-mcp" {
		t.Errorf("OIDC.Audience = %q", cfg.OIDC.Audience)
	}
}
