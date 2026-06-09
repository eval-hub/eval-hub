package config

import "testing"

func TestDefaultConfigAuthType(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	if cfg.AuthType != AuthTypeNone {
		t.Errorf("expected default auth_type %q, got %q", AuthTypeNone, cfg.AuthType)
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
			name:    "oidc removed",
			cfg:     &Config{Transport: "http", Host: "localhost", Port: 3001, AuthType: "oidc"},
			wantErr: true,
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
