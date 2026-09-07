package config

import (
	"encoding/json"
	"testing"
)

func FuzzDurationUnmarshalJSON(f *testing.F) {
	f.Add([]byte(`"5m"`))
	f.Add([]byte(`"2h"`))
	f.Add([]byte(`"500ms"`))
	f.Add([]byte(`"0s"`))
	f.Add([]byte(`"-1h"`))
	f.Add([]byte(`""`))
	f.Add([]byte(`"not-a-duration"`))
	f.Add([]byte(`123`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`"1h30m45s"`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var d Duration
		err := d.UnmarshalJSON(data)
		if err != nil {
			return
		}

		marshalled, err := d.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON failed after successful unmarshal: %v", err)
		}

		var d2 Duration
		if err := d2.UnmarshalJSON(marshalled); err != nil {
			t.Fatalf("round-trip unmarshal failed: %v", err)
		}
		if d.Duration != d2.Duration {
			t.Fatalf("round-trip mismatch: %v vs %v", d.Duration, d2.Duration)
		}
	})
}

func FuzzSidecarConfigResolvePort(f *testing.F) {
	f.Add("http://localhost:8080")
	f.Add("https://localhost:443")
	f.Add("http://localhost")
	f.Add("ftp://host:21")
	f.Add("")
	f.Add("not-a-url")
	f.Add("http://localhost:0")
	f.Add("http://localhost:99999")
	f.Add("http://localhost:abc")
	f.Add("http://:8080")
	f.Add("http://host:65535")
	f.Add("http://host:65536")

	f.Fuzz(func(t *testing.T, baseURL string) {
		sc := &SidecarConfig{BaseURL: baseURL}
		err := sc.ResolvePort()

		if baseURL == "" {
			if err != nil {
				t.Fatal("expected nil error for empty BaseURL")
			}
			if sc.Port != 0 {
				t.Fatalf("expected zero port for empty BaseURL, got %d", sc.Port)
			}
			return
		}

		if err == nil {
			if sc.Port < 1 || sc.Port > 65535 {
				t.Fatalf("resolved port %d out of valid range", sc.Port)
			}
		}
	})
}

func FuzzSidecarConfigJSON(f *testing.F) {
	f.Add([]byte(`{"base_url":"http://localhost:8080","local_mode":false}`))
	f.Add([]byte(`{"base_url":"","local_mode":true}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"base_url":"http://host:1234","eval_hub":{"base_url":"http://eh:8080"}}`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var sc SidecarConfig
		if err := json.Unmarshal(data, &sc); err != nil {
			return
		}
		_ = sc.ResolvePort()
		_ = sc.EffectiveBaseURL()
	})
}
