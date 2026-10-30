package benchmarks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"go.yaml.in/yaml/v4"
)

func fixtureEnv(key string) (string, bool) {
	if key == "MODEL_MAX_LENGTH" {
		return "4096", true
	}
	if strings.HasSuffix(key, "_TEST_DATA_REF") {
		return `{"s3":{"bucket":"smoke","key":"single-record","secret_ref":"smoke-data"}}`, true
	}
	if strings.HasSuffix(key, "URL") {
		return "http://model.example/v1", true
	}
	return "smoke-model", true
}

func TestMaxLengthEnvironmentOverride(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        string
	}{
		{"unset uses fixture default", "", "4096"},
		{"model context override", "32768", "32768"},
		{"whitespace", " 8192 ", "8192"},
		{"not a number", "model-context", ""},
		{"fractional", "4096.5", ""},
		{"zero", "0", ""},
		{"one", "1", ""},
		{"negative", "-4096", ""},
		{"overflow", "999999999999999999999999999", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixture(t, t.TempDir(), "lm_evaluation_harness", "longbenchv2",
				`{"parameters":{"max_length":4096},"max_length_env":"MODEL_MAX_LENGTH","timeout":"5m"}`)
			f, err := loadFixture(path, func(key string) (string, bool) {
				if key != "MODEL_MAX_LENGTH" {
					t.Fatalf("unexpected environment lookup: %s", key)
				}
				return tc.value, tc.value != ""
			})
			if tc.want == "" {
				if err == nil || errors.Is(err, errMissingInput) {
					t.Fatalf("invalid context limit must fail, not skip: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			job := f.jobConfig("lm_evaluation_harness", "longbenchv2", api.ModelRef{Name: "smoke", URL: "http://model.example/v1"})
			// The adapter rejects quoted numbers; check the value sent on the wire.
			encoded, err := json.Marshal(job.Benchmarks[0].Parameters["max_length"])
			if err != nil || string(encoded) != tc.want {
				t.Fatalf("wire max_length = %s, %v; want integer %s", encoded, err, tc.want)
			}
		})
	}
	for _, fallback := range []string{`null`, `"4096"`, `true`, `0`, `1.5`} {
		t.Run("invalid fallback "+fallback, func(t *testing.T) {
			path := writeFixture(t, t.TempDir(), "p", "b",
				`{"parameters":{"max_length":`+fallback+`},"max_length_env":"MODEL_MAX_LENGTH","timeout":"5m"}`)
			if _, err := loadFixture(path, func(string) (string, bool) { return "", false }); err == nil {
				t.Fatal("expected invalid context fallback to fail")
			}
		})
	}
}

func writeFixture(t *testing.T, root, provider, benchmark, body string) string {
	t.Helper()
	path := fixturePath(root, provider, benchmark)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCatalogFixtures(t *testing.T) {
	if _, err := os.Stat("testdata"); errors.Is(err, os.ErrNotExist) {
		t.Skip("catalog fixtures are optional; add the fixture PR or run make generate-benchmark-configs")
	} else if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../config/providers/*.yaml")
	if err != nil {
		t.Fatalf("find provider catalog: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no provider YAML files found in ../../config/providers")
	}
	validate, err := validation.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	paths := make(map[string]bool)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var provider struct {
			ID         string `yaml:"id"`
			Benchmarks []struct {
				ID string `yaml:"id"`
			} `yaml:"benchmarks"`
		}
		if err := yaml.Unmarshal(data, &provider); err != nil {
			t.Fatal(err)
		}
		for _, benchmark := range provider.Benchmarks {
			count++
			path := fixturePath("testdata", provider.ID, benchmark.ID)
			if paths[path] {
				t.Fatalf("duplicate fixture path %s", path)
			}
			paths[path] = true
			t.Run(provider.ID+"/"+benchmark.ID, func(t *testing.T) {
				f, err := loadFixture(path, fixtureEnv)
				if err != nil {
					t.Fatal(err)
				}
				job := f.jobConfig(provider.ID, benchmark.ID, api.ModelRef{Name: "smoke", URL: "http://model.example/v1"})
				if err := validate.Struct(job); err != nil {
					t.Fatal(err)
				}
				if len(job.Benchmarks) != 1 || job.Benchmarks[0].ID != benchmark.ID || job.Benchmarks[0].ProviderID != provider.ID {
					t.Fatal("request must preserve exactly one catalog benchmark")
				}
			})
		}
	}
	t.Logf("validated %d fixtures across %d providers", count, len(files))
}

func TestLoadFixture(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		missing    bool
	}{
		{"null root", `null`, false},
		{"missing parameters", `{"timeout":"1m"}`, true},
	} {
		t.Run(tc.name+" classification", func(t *testing.T) {
			path := writeFixture(t, t.TempDir(), "p", "b", tc.body)
			_, err := loadFixture(path, fixtureEnv)
			if err == nil || errors.Is(err, errMissingInput) != tc.missing {
				t.Fatalf("expected error with missing input=%v, got %v", tc.missing, err)
			}
		})
	}
	for _, tc := range []struct{ name, body string }{
		{"malformed", `{"parameters":`},
		{"unknown field", `{"config":{},"timeout":"1m"}`},
		{"missing parameters", `{"timeout":"1m"}`},
		{"null parameters", `{"parameters":null,"timeout":"1m"}`},
		{"zero timeout", `{"parameters":{},"timeout":"0s"}`},
		{"trailing document", `{"parameters":{},"timeout":"1m"} {}`},
		{"missing env", `{"parameters":{"task":"${MISSING}"},"timeout":"1m"}`},
		{"invalid data ref", `{"parameters":{},"timeout":"1m","test_data_ref_env":"BAD_REF"}`},
		{"conflicting refs", `{"parameters":{},"timeout":"1m","test_data_ref":{},"test_data_ref_env":"REF"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixture(t, t.TempDir(), "p", "b", tc.body)
			if _, err := loadFixture(path, func(string) (string, bool) { return "", false }); err == nil {
				t.Fatal("expected invalid fixture error")
			}
		})
	}
	if _, err := loadFixture(filepath.Join(t.TempDir(), "missing.json"), fixtureEnv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing fixture must remain distinguishable: %v", err)
	}
	path := writeFixture(t, t.TempDir(), "p", "b", `{"parameters":{"nested":["${VALUE}"]},"timeout":"1m"}`)
	value := "quoted \"value\"\n${DO_NOT_EXPAND_AGAIN}"
	f, err := loadFixture(path, func(string) (string, bool) { return value, true })
	if err != nil {
		t.Fatal(err)
	}
	if f.Parameters["nested"].([]any)[0] != value {
		t.Fatal("environment strings must be substituted safely without JSON injection or recursive expansion")
	}
}

func TestExperimentName(t *testing.T) {
	for _, tc := range []struct {
		name, fields, want string
		invalid            bool
	}{
		{name: "disabled", fields: ``, want: ""},
		{name: "generated", fields: `,"experiment":true`, want: "generated"},
		{name: "literal", fields: `,"experiment_name":"shared-smoke"`, want: "shared-smoke"},
		{name: "environment", fields: `,"experiment_name":"${MLFLOW_EXPERIMENT_NAME}"`, want: "team-smoke"},
		{name: "overrides generated", fields: `,"experiment":true,"experiment_name":"shared-smoke"`, want: "shared-smoke"},
		{name: "overrides disabled", fields: `,"experiment":false,"experiment_name":"shared-smoke"`, want: "shared-smoke"},
		{name: "empty literal", fields: `,"experiment_name":""`, want: ""},
		{name: "blank", fields: `,"experiment_name":"  "`, invalid: true},
		{name: "missing environment", fields: `,"experiment_name":"${MISSING}"`, invalid: true},
		{name: "empty environment", fields: `,"experiment_name":"${EMPTY}"`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixture(t, t.TempDir(), "p", "b", `{"parameters":{},"timeout":"1m"`+tc.fields+`}`)
			f, err := loadFixture(path, func(key string) (string, bool) {
				switch key {
				case "MLFLOW_EXPERIMENT_NAME":
					return "team-smoke", true
				case "EMPTY":
					return "", true
				default:
					return "", false
				}
			})
			if tc.invalid {
				if err == nil {
					t.Fatal("expected invalid experiment configuration")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			job := f.jobConfig("p", "b", api.ModelRef{Name: "model", URL: "http://model.example"})
			if tc.want == "" {
				if job.Experiment != nil {
					t.Fatal("experiment should be disabled")
				}
				return
			}
			if job.Experiment == nil {
				t.Fatal("experiment reference missing")
			}
			want := tc.want
			if want == "generated" {
				want = job.Name
			}
			if job.Experiment.Name != want {
				t.Fatalf("experiment name = %q; want %q", job.Experiment.Name, want)
			}
		})
	}
}

func TestFixturePaths(t *testing.T) {
	if got, want := fixturePath("testdata", "provider:name", "arc:challenge"), filepath.Join("testdata", "provider%3Aname", "arc%3Achallenge.json"); got != want {
		t.Fatalf("fixture path = %q; want %q", got, want)
	}
	seen := make(map[string]bool)
	for _, id := range []string{"../escape", "..", ".", "a/b", "a%2Fb", "a:b", "a%3Ab", "a.b", "a%2Eb"} {
		path := fixturePath("testdata", id, id)
		rel, err := filepath.Rel("testdata", path)
		if err != nil || strings.HasPrefix(rel, "..") || len(strings.Split(rel, string(filepath.Separator))) != 2 {
			t.Fatalf("unsafe fixture path for %q: %s", id, path)
		}
		if seen[path] {
			t.Fatalf("filename collision for %q", id)
		}
		seen[path] = true
	}
}

func TestDiscover(t *testing.T) {
	for _, mode := range []string{"pagination", "repeated", "empty-page", "http-error", "duplicate-benchmark"} {
		t.Run(mode, func(t *testing.T) {
			var offsets []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/providers") {
					offset := r.URL.Query().Get("offset")
					offsets = append(offsets, offset)
					if r.URL.Query().Get("limit") != "100" {
						t.Error("missing page limit")
					}
					if mode == "http-error" {
						w.WriteHeader(503)
						return
					}
					if mode == "empty-page" {
						_, _ = fmt.Fprint(w, `{"total_count":2,"items":[]}`)
						return
					}
					id := "p1"
					if offset == "1" && mode != "repeated" {
						id = "p2"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 2, "items": []any{map[string]any{"resource": map[string]any{"id": id}}}})
					return
				}
				id := filepath.Base(r.URL.Path)
				benchmarks := []any{map[string]any{"id": "b"}}
				if mode == "duplicate-benchmark" {
					benchmarks = append(benchmarks, benchmarks[0])
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"resource": map[string]any{"id": id}, "benchmarks": benchmarks})
			}))
			defer server.Close()
			client := evalhubclient.NewClient(server.URL).WithMaxRetries(0)
			providers, err := discover(t.Context(), client)
			if mode != "pagination" {
				if err == nil {
					t.Fatal("expected discovery error")
				}
				return
			}
			if err != nil || len(providers) != 2 || strings.Join(offsets, ",") != "0,1" {
				t.Fatalf("pagination providers=%v offsets=%v err=%v", providers, offsets, err)
			}
		})
	}
}

func TestWaitForCompletion(t *testing.T) {
	for _, mode := range []string{"completed", "failed", "partially_failed", "cancelled", "timeout", "wrong-status", "wrong-result", "no-results", "http-error"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http-error" {
					w.WriteHeader(500)
					return
				}
				state := mode
				if strings.HasPrefix(mode, "wrong-") || mode == "no-results" {
					state = "completed"
				}
				if mode == "timeout" {
					state = "running"
				}
				job := api.EvaluationJobResource{
					Status:  &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallState(state)}, Benchmarks: []api.BenchmarkStatus{{ID: "b", ProviderID: "p", Status: api.StateCompleted}}},
					Results: &api.EvaluationJobResults{Benchmarks: []api.BenchmarkResult{{ID: "b", ProviderID: "p"}}},
				}
				if mode == "wrong-status" {
					job.Status.Benchmarks[0].ProviderID = "other"
				}
				if mode == "wrong-result" {
					job.Results.Benchmarks[0].ID = "other"
				}
				if mode == "no-results" {
					job.Results = nil
				}
				_ = json.NewEncoder(w).Encode(job)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			err := waitForCompletion(ctx, evalhubclient.NewClient(server.URL).WithMaxRetries(0), "job", "p", "b", time.Millisecond)
			if mode == "completed" && err != nil {
				t.Fatal(err)
			}
			if mode != "completed" && err == nil {
				t.Fatal("expected failure")
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected timeout: %v", err)
			}
		})
	}
}

func TestCleanupJob(t *testing.T) {
	for _, status := range []int{204, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Query().Get("hard_delete") != "true" {
					t.Error("cleanup must hard-delete runtime resources and record")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			err := cleanupJob(t.Context(), server.Client(), server.URL, "job")
			if (err != nil) != (status == 500) {
				t.Fatalf("unexpected cleanup outcome: %v", err)
			}
		})
	}
}

// Exercise the actual test entry point in a child process so skips, failures,
// t.Parallel and cleanup are checked without contaminating the parent result.
func TestSmokeHarness(t *testing.T) {
	for _, mode := range []string{"parallel", "zero-configured", "missing-input", "missing-data-ref", "missing-parameters", "missing-timeout", "missing-model", "malformed", "failed", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if mode != "zero-configured" {
				for _, provider := range []string{"p1", "p2"} {
					timeout := "5s"
					if mode == "timeout" {
						timeout = "100ms"
					}
					extra := `{"parameters":{"task":"${BENCHMARK_REQUIRED_INPUT}"},"timeout":"1m"}`
					switch mode {
					case "missing-data-ref":
						extra = `{"parameters":{},"timeout":"1m","test_data_ref_env":"BENCHMARK_REQUIRED_INPUT"}`
					case "missing-parameters":
						extra = `{"timeout":"1m"}`
					case "missing-timeout":
						extra = `{"parameters":{}}`
					case "missing-model":
						extra = `{"parameters":{},"timeout":"1m","model":{}}`
					case "malformed":
						extra = `{"parameters":`
					}
					writeFixture(t, root, provider, "missing", extra)
					writeFixture(t, root, provider, "b", fmt.Sprintf(`{"parameters":{"num_examples":1},"timeout":%q}`, timeout))
				}
			}
			var mu sync.Mutex
			jobs := make(map[string]string)
			created, deleted, active, maxActive := 0, 0, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Tenant") != "tenant" || r.Header.Get("X-User") != "user" {
					t.Error("missing API identity headers")
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/providers"):
					_, _ = fmt.Fprint(w, `{"total_count":2,"items":[{"resource":{"id":"p1"}},{"resource":{"id":"p2"}}]}`)
				case strings.Contains(r.URL.Path, "/providers/"):
					id := filepath.Base(r.URL.Path)
					_ = json.NewEncoder(w).Encode(map[string]any{"resource": map[string]any{"id": id}, "benchmarks": []any{map[string]any{"id": "b"}, map[string]any{"id": "missing"}}})
				case r.Method == http.MethodPost:
					var job api.EvaluationJobConfig
					if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					if len(job.Benchmarks) != 1 || job.Benchmarks[0].ID != "b" || job.Benchmarks[0].Parameters["num_examples"] != float64(1) {
						t.Error("incorrect single-benchmark request")
						w.WriteHeader(400)
						return
					}
					created++
					active++
					if active > maxActive {
						maxActive = active
					}
					id := fmt.Sprint(created)
					jobs[id] = job.Benchmarks[0].ProviderID
					w.WriteHeader(202)
					_ = json.NewEncoder(w).Encode(map[string]any{"resource": map[string]any{"id": id}})
				case r.Method == http.MethodDelete:
					if r.URL.Query().Get("hard_delete") != "true" {
						t.Error("expected hard delete")
					}
					deleted++
					active--
					w.WriteHeader(204)
				default:
					id := filepath.Base(r.URL.Path)
					state := "running"
					if created == 2 && mode != "timeout" {
						state = "completed"
					}
					if mode == "failed" {
						state = "failed"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"status":  map[string]any{"state": state, "benchmarks": []any{map[string]any{"provider_id": jobs[id], "id": "b", "status": "completed"}}},
						"results": map[string]any{"benchmarks": []any{map[string]any{"provider_id": jobs[id], "id": "b"}}},
					})
				}
			}))
			defer server.Close()
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBenchmarkSmoke$", "-test.v", "-test.parallel=2", "-test.timeout=1m")
			overrides := map[string]string{"BENCHMARK_SMOKE": "1", "SERVER_URL": server.URL, "MODEL_NAME": "smoke", "MODEL_URL": "http://model.example/v1", "BENCHMARK_CONFIG_DIR": root, "BENCHMARK_POLL_INTERVAL": "1ms", "EVALHUB_TOKEN": "test-token", "X_TENANT": "tenant", "X_USER": "user", "EVALHUB_CA_CERT_PATH": "", "MODEL_AUTH_SECRET_REF": "", "BENCHMARK_REQUIRED_INPUT": ""}
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if _, ok := overrides[key]; !ok {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			for key, value := range overrides {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			output, err := cmd.CombinedOutput()
			wantSuccess := mode == "parallel" || strings.HasPrefix(mode, "missing-")
			if wantSuccess && err != nil {
				t.Fatalf("smoke suite failed: %v\n%s", err, output)
			}
			if !wantSuccess && err == nil {
				t.Fatalf("smoke suite should fail:\n%s", output)
			}
			mu.Lock()
			defer mu.Unlock()
			if mode == "zero-configured" {
				if created != 0 || !strings.Contains(string(output), "no configured benchmarks were submitted") {
					t.Fatalf("zero-configured run: created=%d\n%s", created, output)
				}
				for _, message := range []string{"missing JSON fixture for provider", "benchmark", "make generate-benchmark-configs", root} {
					if !strings.Contains(string(output), message) {
						t.Fatalf("missing fixture diagnostic %q:\n%s", message, output)
					}
				}
			} else if created != 2 || deleted != 2 {
				t.Fatalf("created=%d deleted=%d\n%s", created, deleted, output)
			}
			if wantSuccess && (!strings.Contains(string(output), "missing required") || !strings.Contains(string(output), "skipped=2 submitted=2")) {
				t.Fatalf("missing input diagnostic or counts absent:\n%s", output)
			}
			if mode == "malformed" && !strings.Contains(string(output), "unexpected EOF") {
				t.Fatalf("malformed fixture did not fail clearly:\n%s", output)
			}
			if mode == "parallel" && maxActive != 2 {
				t.Fatalf("cross-provider parallelism was %d\n%s", maxActive, output)
			}
			if mode != "zero-configured" && mode != "malformed" && !strings.Contains(string(output), "--- SKIP:") {
				t.Fatalf("missing input was not skipped:\n%s", output)
			}
		})
	}
}

func TestSmokeEnvAliases(t *testing.T) {
	for _, tc := range []struct{ primary, alias string }{
		{"EVALHUB_TOKEN", "AUTH_TOKEN"},
		{"JUDGE_MODEL_NAME", "JUDGE_MODEL"},
		{"JUDGE_BASE_URL", "JUDGE_API_BASE"},
		{"JUDGE_BASE_URL", "JUDGE_PROXY_URL"},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			for _, key := range []string{"EVALHUB_TOKEN", "AUTH_TOKEN", "JUDGE_MODEL_NAME", "JUDGE_MODEL", "JUDGE_BASE_URL", "JUDGE_API_BASE", "JUDGE_PROXY_URL"} {
				t.Setenv(key, "")
			}
			t.Setenv(tc.alias, "alias-value")
			if got, ok := smokeEnv(tc.primary); !ok || got != "alias-value" {
				t.Fatalf("alias lookup = %q, %v", got, ok)
			}
			t.Setenv(tc.primary, "primary-value")
			if got, _ := smokeEnv(tc.primary); got != "primary-value" {
				t.Fatalf("primary must win: %q", got)
			}
		})
	}
}
