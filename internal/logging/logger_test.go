package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
)

func TestAsPrettyJson_noMask(t *testing.T) {
	t.Parallel()

	in := map[string]any{"url": "https://example.com", "count": 3}
	out := AsPrettyJson(in)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if parsed["url"] != "https://example.com" || parsed["count"].(float64) != 3 {
		t.Fatalf("unexpected decoded value: %#v", parsed)
	}
	if !strings.Contains(out, `"url"`) || !strings.Contains(out, "example.com") {
		t.Fatalf("expected readable JSON, got:\n%s", out)
	}
}

func TestAsPrettyJson_maskTopLevelFields(t *testing.T) {
	t.Parallel()

	type config struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}

	in := config{URL: "https://api.example", Token: "secret-token-value"}
	out := AsPrettyJson(in, "token")

	if strings.Contains(out, "secret-token-value") {
		t.Fatalf("token should be masked, got:\n%s", out)
	}
	if !strings.Contains(out, `"token"`) || !strings.Contains(out, "*************") {
		t.Fatalf("expected masked token field, got:\n%s", out)
	}
	if !strings.Contains(out, "https://api.example") {
		t.Fatalf("url should remain visible, got:\n%s", out)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["token"] != "*************" {
		t.Fatalf("token value: got %q want %q", parsed["token"], "*************")
	}
	if parsed["url"] != "https://api.example" {
		t.Fatalf("url: got %v", parsed["url"])
	}
}

func TestAsPrettyJson_multipleMaskKeys(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"password": "p1",
		"api_key":  "k1",
		"public":   "visible",
	}
	out := AsPrettyJson(in, "password", "api_key")

	if strings.Contains(out, `"p1"`) || strings.Contains(out, `"k1"`) {
		t.Fatalf("sensitive values should be masked, got:\n%s", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["password"] != "*************" || parsed["api_key"] != "*************" {
		t.Fatalf("masked fields: %#v", parsed)
	}
	if parsed["public"] != "visible" {
		t.Fatalf("public field should be unchanged: %#v", parsed)
	}
}

func TestAsPrettyJson_maskAddsMissingKey(t *testing.T) {
	t.Parallel()

	in := map[string]any{"only": "one"}
	out := AsPrettyJson(in, "missing_key")

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["missing_key"] != "*************" {
		t.Fatalf("missing masked key behavior: %#v", parsed)
	}
}

func TestAsPrettyJson_nonObjectRootWithMask_fallsBackToString(t *testing.T) {
	t.Parallel()

	in := []int{1, 2, 3}
	out := AsPrettyJson(in, "x")

	// json.Unmarshal into map fails for arrays; implementation falls back to fmt.Sprintf("%v", s)
	if out != "[masking failed: unsupported structure]" {
		t.Fatalf("expected fmt failure for non-object root, got %q", out)
	}
}

func TestAsPrettyJson_marshalError_fallsBackToString(t *testing.T) {
	t.Parallel()

	ch := make(chan int)
	out := AsPrettyJson(ch)
	want := fmt.Sprintf("%v", ch)
	if out != want {
		t.Fatalf("marshal failure should fall back to %%v: got %q want %q", out, want)
	}
}

func TestLogRequestSuccess_additionalArgsInOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx := &executioncontext.ExecutionContext{
		Ctx:       context.Background(),
		RequestID: "req-extra-args",
		Logger:    logger,
		StartedAt: time.Now().Add(-50 * time.Millisecond),
	}

	LogRequestSuccess(ctx, 201, map[string]string{"ignored": "response is not logged"}, "extra_key", "extra_value", "count", 42)

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("log output is not valid JSON: %v\n%s", err, buf.String())
	}
	if payload["msg"] != "Request successful" {
		t.Fatalf("msg: got %v want Request successful", payload["msg"])
	}
	if code, ok := payload["code"].(float64); !ok || code != 201 {
		t.Fatalf("code: got %v want 201", payload["code"])
	}
	if payload["extra_key"] != "extra_value" {
		t.Fatalf("extra_key: got %v want extra_value", payload["extra_key"])
	}
	if n, ok := payload["count"].(float64); !ok || n != 42 {
		t.Fatalf("count: got %v want 42", payload["count"])
	}
	if _, ok := payload["duration"]; !ok {
		t.Fatalf("expected duration field in log output: %v", payload)
	}
}
