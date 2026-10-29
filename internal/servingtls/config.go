// Package servingtls defines the platform-independent native HTTPS contract used
// by the API and both MCP HTTP transports. Deployment tooling owns policy sourcing.
package servingtls

import (
	"crypto/tls"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// FromEnv validates generic TLS_* controls and preloads serving material before
// a listener is opened. A nil result explicitly selects optional plaintext HTTP.
// Call ListenAndServeTLS with empty filenames to use the validated certificate.
func FromEnv(certFile, keyFile string) (*tls.Config, error) {
	required := false
	if value, set := os.LookupEnv("TLS_REQUIRED"); set {
		var err error
		required, err = strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("TLS_REQUIRED: %w", err)
		}
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}}
	minVersion, hasMin := os.LookupEnv("TLS_MIN_VERSION")
	if hasMin {
		switch minVersion {
		case "1.2":
			cfg.MinVersion = tls.VersionTLS12
		case "1.3":
			cfg.MinVersion = tls.VersionTLS13
		default:
			return nil, fmt.Errorf("TLS_MIN_VERSION must be 1.2 or 1.3, got %q", minVersion)
		}
	}
	ciphers, hasCiphers := os.LookupEnv("TLS_CIPHERS")
	if hasCiphers {
		if cfg.MinVersion == tls.VersionTLS13 {
			return nil, fmt.Errorf("TLS_CIPHERS only configures TLS 1.2; Go does not support TLS 1.3 suite selection")
		}
		supported := map[string]uint16{}
		for _, suite := range tls.CipherSuites() {
			if suite.ID != tls.TLS_AES_128_GCM_SHA256 && suite.ID != tls.TLS_AES_256_GCM_SHA384 && suite.ID != tls.TLS_CHACHA20_POLY1305_SHA256 {
				supported[suite.Name] = suite.ID
			}
		}
		seen := map[uint16]bool{}
		for _, name := range strings.Split(ciphers, ",") {
			id, ok := supported[strings.TrimSpace(name)]
			if !ok || seen[id] {
				return nil, fmt.Errorf("TLS_CIPHERS: unsupported, empty or duplicate TLS 1.2 suite %q", name)
			}
			cfg.CipherSuites = append(cfg.CipherSuites, id)
			seen[id] = true
		}
	}
	curves, hasCurves := os.LookupEnv("TLS_CURVES")
	if hasCurves {
		supported := map[string]tls.CurveID{"P256": tls.CurveP256, "P384": tls.CurveP384, "P521": tls.CurveP521, "X25519": tls.X25519, "X25519MLKEM768": tls.X25519MLKEM768, "SecP256r1MLKEM768": tls.SecP256r1MLKEM768, "SecP384r1MLKEM1024": tls.SecP384r1MLKEM1024}
		seen := map[tls.CurveID]bool{}
		classical := false
		for _, name := range strings.Split(curves, ",") {
			id, ok := supported[strings.TrimSpace(name)]
			if !ok || seen[id] {
				return nil, fmt.Errorf("TLS_CURVES: unsupported, empty or duplicate group %q", name)
			}
			if id == tls.CurveP256 || id == tls.CurveP384 || id == tls.CurveP521 || id == tls.X25519 {
				classical = true
			}
			cfg.CurvePreferences = append(cfg.CurvePreferences, id)
			seen[id] = true
		}
		if cfg.MinVersion == tls.VersionTLS12 && !classical {
			return nil, fmt.Errorf("TLS_CURVES needs a TLS 1.2-capable group or TLS_MIN_VERSION=1.3")
		}
	}
	if (certFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("both TLS certificate and key files must be provided")
	}
	if certFile == "" {
		if required || hasMin || hasCiphers || hasCurves {
			return nil, fmt.Errorf("TLS settings require a serving certificate and key; refusing plaintext fallback")
		}
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("loading TLS serving certificate/key: %w", err)
	}
	cfg.Certificates = []tls.Certificate{certificate}
	return cfg, nil
}
