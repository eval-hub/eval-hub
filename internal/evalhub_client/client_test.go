package evalhub_client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustMarshal: %v", err)
	}
	return b
}

// newTestClient returns a Client pointed at the given test server.
func newTestClient(srv *httptest.Server) *Client {
	return NewClient(srv.URL)
}

// captureRequest records the last received request so tests can assert on it.
type captureRequest struct {
	method  string
	path    string
	query   string
	headers http.Header
	body    []byte
}

func newCapturingServer(t *testing.T, status int, respBody []byte) (*httptest.Server, *captureRequest) {
	t.Helper()
	capture := &captureRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.query = r.URL.RawQuery
		capture.headers = r.Header.Clone()

		b := make([]byte, r.ContentLength)
		r.Body.Read(b) //nolint:errcheck
		capture.body = b

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(respBody) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

// ─── Health ───────────────────────────────────────────────────────────────────

func TestGetHealth(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	want := api.HealthResponse{Status: "ok", Version: "0.4.0", Timestamp: &now}
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, want))

	got, err := newTestClient(srv).GetHealth()
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if capture.method != http.MethodGet {
		t.Errorf("method = %s, want GET", capture.method)
	}
	if capture.path != "/api/v1/health" {
		t.Errorf("path = %s, want /api/v1/health", capture.path)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
}

// ─── Authorization header ─────────────────────────────────────────────────────

func TestAuthorizationHeader(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.HealthResponse{Status: "ok"}))

	_, err := newTestClient(srv).WithToken("my-secret-token").GetHealth()
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if got := capture.headers.Get("Authorization"); got != "Bearer my-secret-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer my-secret-token")
	}
}

func TestMissingTokenSendsNoAuthHeader(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.HealthResponse{Status: "ok"}))

	_, err := newTestClient(srv).GetHealth()
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if got := capture.headers.Get("Authorization"); got != "" {
		t.Errorf("Authorization header should be absent without a token, got %q", got)
	}
}

// ─── Tenant header ────────────────────────────────────────────────────────────

func TestTenantHeader(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.HealthResponse{Status: "ok"}))

	_, err := newTestClient(srv).WithTenant("my-namespace").GetHealth()
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if got := capture.headers.Get("X-Tenant"); got != "my-namespace" {
		t.Errorf("X-Tenant = %q, want %q", got, "my-namespace")
	}
}

func TestNoTenantHeaderWhenNotConfigured(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.HealthResponse{Status: "ok"}))

	_, err := newTestClient(srv).GetHealth()
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if got := capture.headers.Get("X-Tenant"); got != "" {
		t.Errorf("X-Tenant should be absent when tenant is not configured, got %q", got)
	}
}

// ─── Base URL trailing slash ──────────────────────────────────────────────────

func TestBaseURLTrailingSlashStripped(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.HealthResponse{Status: "ok"}))

	// Append trailing slashes — the client must strip them.
	client := NewClient(srv.URL + "//")
	_, err := client.GetHealth()
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if capture.path != "/api/v1/health" {
		t.Errorf("path = %q, want /api/v1/health", capture.path)
	}
}

// ─── Error responses ─────────────────────────────────────────────────────────

func errorBody(code, msg string) []byte {
	b, _ := json.Marshal(api.Error{MessageCode: code, Message: msg})
	return b
}

func TestErrorUnauthorized(t *testing.T) {
	srv, _ := newCapturingServer(t, http.StatusUnauthorized, errorBody("auth.unauthorized", "invalid token"))

	_, err := newTestClient(srv).GetHealth()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("IsUnauthorized() = false, want true (status=%d)", apiErr.StatusCode)
	}
	if apiErr.Code != "auth.unauthorized" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "auth.unauthorized")
	}
}

func TestErrorForbidden(t *testing.T) {
	srv, _ := newCapturingServer(t, http.StatusForbidden, errorBody("auth.forbidden", "access denied"))

	_, err := newTestClient(srv).GetHealth()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsForbidden() {
		t.Errorf("IsForbidden() = false, want true")
	}
}

func TestErrorNotFound(t *testing.T) {
	srv, _ := newCapturingServer(t, http.StatusNotFound, errorBody("resource.not_found", "not found"))

	_, err := newTestClient(srv).GetJob("missing-id")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsNotFound() {
		t.Errorf("IsNotFound() = false, want true")
	}
}

func TestErrorInternalServerError(t *testing.T) {
	// maxRetries=0 so the test server is only hit once.
	srv, _ := newCapturingServer(t, http.StatusInternalServerError, errorBody("server.error", "internal error"))

	_, err := newTestClient(srv).WithMaxRetries(0).GetHealth()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestErrorMessageFallback(t *testing.T) {
	// Response body is not valid JSON — the client must fall back to the HTTP status text.
	srv, _ := newCapturingServer(t, http.StatusBadRequest, []byte("not json"))

	_, err := newTestClient(srv).GetHealth()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.Message != http.StatusText(http.StatusBadRequest) {
		t.Errorf("Message = %q, want %q", apiErr.Message, http.StatusText(http.StatusBadRequest))
	}
}

// ─── Backend unreachable ──────────────────────────────────────────────────────

func TestBackendUnreachable(t *testing.T) {
	// Point the client at a port where nothing is listening.
	client := NewClient("http://127.0.0.1:1").WithMaxRetries(0)
	_, err := client.GetHealth()
	if err == nil {
		t.Fatal("expected an error when backend is unreachable, got nil")
	}
}

// ─── TLS InsecureSkipVerify ───────────────────────────────────────────────────

func TestInsecureSkipVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.HealthResponse{Status: "ok"}) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	// Without InsecureSkipVerify the TLS server's self-signed cert will be rejected.
	_, err := NewClient(srv.URL).WithMaxRetries(0).GetHealth()
	if err == nil {
		t.Fatal("expected TLS error without InsecureSkipVerify")
	}

	// With InsecureSkipVerify it should succeed.
	got, err := NewClient(srv.URL).WithInsecureSkipVerify().GetHealth()
	if err != nil {
		t.Fatalf("GetHealth with InsecureSkipVerify: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("Status = %q, want ok", got.Status)
	}
}

// ─── Providers ────────────────────────────────────────────────────────────────

func TestListProviders(t *testing.T) {
	want := api.ProviderResourceList{
		Page:  api.Page{TotalCount: 1, Limit: 10},
		Items: []api.ProviderResource{{ProviderConfig: api.ProviderConfig{Name: "lm-evaluation-harness"}}},
	}
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, want))

	got, err := newTestClient(srv).ListProviders()
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if capture.method != http.MethodGet {
		t.Errorf("method = %s, want GET", capture.method)
	}
	if capture.path != "/api/v1/evaluations/providers" {
		t.Errorf("path = %s, want /api/v1/evaluations/providers", capture.path)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "lm-evaluation-harness" {
		t.Errorf("unexpected items: %+v", got.Items)
	}
}

func TestListProvidersPagination(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.ProviderResourceList{}))

	_, err := newTestClient(srv).ListProviders(WithLimit(5), WithOffset(10))
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if !strings.Contains(capture.query, "limit=5") {
		t.Errorf("query %q missing limit=5", capture.query)
	}
	if !strings.Contains(capture.query, "offset=10") {
		t.Errorf("query %q missing offset=10", capture.query)
	}
}

func TestGetProvider(t *testing.T) {
	want := api.ProviderResource{ProviderConfig: api.ProviderConfig{Name: "ragas"}}
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, want))

	got, err := newTestClient(srv).GetProvider("ragas")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if capture.path != "/api/v1/evaluations/providers/ragas" {
		t.Errorf("path = %s, want /api/v1/evaluations/providers/ragas", capture.path)
	}
	if got.Name != "ragas" {
		t.Errorf("Name = %q, want ragas", got.Name)
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

// mockProviderServer returns a server that always replies with a list containing
// a single provider that has the given benchmarks.
func mockProviderServer(t *testing.T, benchmarks []api.BenchmarkResource) *httptest.Server {
	t.Helper()
	list := api.ProviderResourceList{
		Page: api.Page{TotalCount: 1, Limit: 100},
		Items: []api.ProviderResource{
			{ProviderConfig: api.ProviderConfig{Name: "test-provider", Benchmarks: benchmarks}},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestListBenchmarks(t *testing.T) {
	benchmarks := []api.BenchmarkResource{
		{ID: "arc", Name: "ARC", Tags: []string{"reasoning"}},
		{ID: "mmlu", Name: "MMLU", Tags: []string{"knowledge"}},
	}
	srv := mockProviderServer(t, benchmarks)

	got, err := newTestClient(srv).ListBenchmarks()
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(benchmarks) = %d, want 2", len(got))
	}
}

func TestGetBenchmark(t *testing.T) {
	benchmarks := []api.BenchmarkResource{
		{ID: "arc", Name: "ARC"},
		{ID: "mmlu", Name: "MMLU"},
	}
	srv := mockProviderServer(t, benchmarks)

	got, err := newTestClient(srv).GetBenchmark("mmlu")
	if err != nil {
		t.Fatalf("GetBenchmark: %v", err)
	}
	if got.ID != "mmlu" {
		t.Errorf("ID = %q, want mmlu", got.ID)
	}
}

func TestGetBenchmarkNotFound(t *testing.T) {
	srv := mockProviderServer(t, []api.BenchmarkResource{{ID: "arc"}})

	_, err := newTestClient(srv).GetBenchmark("nonexistent")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsNotFound() {
		t.Errorf("IsNotFound() = false, want true")
	}
}

func TestListBenchmarksByLabel(t *testing.T) {
	benchmarks := []api.BenchmarkResource{
		{ID: "arc", Tags: []string{"reasoning", "commonsense"}},
		{ID: "mmlu", Tags: []string{"knowledge"}},
		{ID: "hellaswag", Tags: []string{"reasoning"}},
	}
	srv := mockProviderServer(t, benchmarks)

	got, err := newTestClient(srv).ListBenchmarksByLabel([]string{"reasoning"})
	if err != nil {
		t.Fatalf("ListBenchmarksByLabel: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (arc and hellaswag)", len(got))
	}
	for _, b := range got {
		if b.ID != "arc" && b.ID != "hellaswag" {
			t.Errorf("unexpected benchmark %q in results", b.ID)
		}
	}
}

func TestListBenchmarksByLabelMultipleLabels(t *testing.T) {
	benchmarks := []api.BenchmarkResource{
		{ID: "arc", Tags: []string{"reasoning", "commonsense"}},
		{ID: "hellaswag", Tags: []string{"reasoning"}},
	}
	srv := mockProviderServer(t, benchmarks)

	// Only arc has both labels.
	got, err := newTestClient(srv).ListBenchmarksByLabel([]string{"reasoning", "commonsense"})
	if err != nil {
		t.Fatalf("ListBenchmarksByLabel: %v", err)
	}
	if len(got) != 1 || got[0].ID != "arc" {
		t.Errorf("expected [arc], got %+v", got)
	}
}

// ─── Collections ──────────────────────────────────────────────────────────────

func TestListCollections(t *testing.T) {
	want := api.CollectionResourceList{
		Page:  api.Page{TotalCount: 1, Limit: 10},
		Items: []api.CollectionResource{{CollectionConfig: api.CollectionConfig{Name: "safety-suite"}}},
	}
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, want))

	got, err := newTestClient(srv).ListCollections()
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if capture.path != "/api/v1/evaluations/collections" {
		t.Errorf("path = %s, want /api/v1/evaluations/collections", capture.path)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "safety-suite" {
		t.Errorf("unexpected items: %+v", got.Items)
	}
}

func TestGetCollection(t *testing.T) {
	want := api.CollectionResource{CollectionConfig: api.CollectionConfig{Name: "safety-suite"}}
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, want))

	got, err := newTestClient(srv).GetCollection("col-123")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if capture.path != "/api/v1/evaluations/collections/col-123" {
		t.Errorf("path = %s, want /api/v1/evaluations/collections/col-123", capture.path)
	}
	if got.Name != "safety-suite" {
		t.Errorf("Name = %q, want safety-suite", got.Name)
	}
}

// ─── Evaluation jobs ──────────────────────────────────────────────────────────

func TestListJobs(t *testing.T) {
	want := api.EvaluationJobResourceList{
		Page: api.Page{TotalCount: 2, Limit: 10},
		Items: []api.EvaluationJobResource{
			{EvaluationJobConfig: api.EvaluationJobConfig{Name: "job-a"}},
			{EvaluationJobConfig: api.EvaluationJobConfig{Name: "job-b"}},
		},
	}
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, want))

	got, err := newTestClient(srv).ListJobs()
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if capture.path != "/api/v1/evaluations/jobs" {
		t.Errorf("path = %s, want /api/v1/evaluations/jobs", capture.path)
	}
	if len(got.Items) != 2 {
		t.Errorf("len(items) = %d, want 2", len(got.Items))
	}
}

func TestListJobsPaginationParams(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.EvaluationJobResourceList{}))

	_, err := newTestClient(srv).ListJobs(WithLimit(20), WithOffset(40))
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if !strings.Contains(capture.query, "limit=20") {
		t.Errorf("query %q missing limit=20", capture.query)
	}
	if !strings.Contains(capture.query, "offset=40") {
		t.Errorf("query %q missing offset=40", capture.query)
	}
}

func TestListJobsByStatus(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, api.EvaluationJobResourceList{}))

	_, err := newTestClient(srv).ListJobsByStatus(api.OverallStateRunning)
	if err != nil {
		t.Fatalf("ListJobsByStatus: %v", err)
	}
	if !strings.Contains(capture.query, "status=running") {
		t.Errorf("query %q missing status=running", capture.query)
	}
}

func TestGetJob(t *testing.T) {
	want := api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{Name: "my-job"}}
	srv, capture := newCapturingServer(t, http.StatusOK, mustMarshal(t, want))

	got, err := newTestClient(srv).GetJob("job-abc")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if capture.path != "/api/v1/evaluations/jobs/job-abc" {
		t.Errorf("path = %s, want /api/v1/evaluations/jobs/job-abc", capture.path)
	}
	if got.Name != "my-job" {
		t.Errorf("Name = %q, want my-job", got.Name)
	}
}

func TestCreateJob(t *testing.T) {
	cfg := api.EvaluationJobConfig{
		Name:  "bench-run",
		Model: api.ModelRef{URL: "http://llm:8000", Name: "llama3"},
	}
	want := api.EvaluationJobResource{EvaluationJobConfig: cfg}
	srv, capture := newCapturingServer(t, http.StatusAccepted, mustMarshal(t, want))

	got, err := newTestClient(srv).CreateJob(cfg)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if capture.method != http.MethodPost {
		t.Errorf("method = %s, want POST", capture.method)
	}
	if capture.path != "/api/v1/evaluations/jobs" {
		t.Errorf("path = %s, want /api/v1/evaluations/jobs", capture.path)
	}
	if got.Name != "bench-run" {
		t.Errorf("Name = %q, want bench-run", got.Name)
	}

	var decoded api.EvaluationJobConfig
	if err := json.Unmarshal(capture.body, &decoded); err != nil {
		t.Fatalf("could not decode request body: %v", err)
	}
	if decoded.Name != cfg.Name {
		t.Errorf("request body Name = %q, want %q", decoded.Name, cfg.Name)
	}
}

func TestCancelJob(t *testing.T) {
	srv, capture := newCapturingServer(t, http.StatusNoContent, nil)

	err := newTestClient(srv).CancelJob("job-xyz")
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if capture.method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", capture.method)
	}
	if capture.path != "/api/v1/evaluations/jobs/job-xyz" {
		t.Errorf("path = %s, want /api/v1/evaluations/jobs/job-xyz", capture.path)
	}
}

// ─── Retry logic ──────────────────────────────────────────────────────────────

func TestRetryOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.HealthResponse{Status: "ok"}) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	// Use a tiny sleep so the test does not take long. We override the client's
	// httpClient to intercept the actual retries; the default retry sleep is not
	// configurable per-interval but maxRetries controls how many we allow.
	client := NewClient(srv.URL).WithMaxRetries(3)
	// Speed the test up by using a custom httpClient with a tiny timeout, then
	// restore it. Actually, the retry sleep is hard-coded; just accept the test
	// takes ~1 s (500ms + 0ms for last ok).  Limit retries to 2 so it passes on
	// the 3rd attempt.
	_, err := client.GetHealth()
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if attempts != 3 {
		t.Errorf("server hit %d times, want 3", attempts)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(srv.URL).WithMaxRetries(3).GetHealth()
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if attempts != 1 {
		t.Errorf("server hit %d times, want exactly 1 (no retry on 4xx)", attempts)
	}
}

// ─── matchesAllLabels ─────────────────────────────────────────────────────────

func TestMatchesAllLabels(t *testing.T) {
	cases := []struct {
		tags     []string
		required []string
		want     bool
	}{
		{[]string{"a", "b"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{}, []string{"a"}, false},
		{[]string{"a"}, []string{}, true},
		{[]string{}, []string{}, true},
	}
	for _, tc := range cases {
		got := matchesAllLabels(tc.tags, tc.required)
		if got != tc.want {
			t.Errorf("matchesAllLabels(%v, %v) = %v, want %v", tc.tags, tc.required, got, tc.want)
		}
	}
}

// ─── APIError helpers ─────────────────────────────────────────────────────────

func TestAPIErrorString(t *testing.T) {
	withCode := &APIError{StatusCode: 404, Code: "resource.not_found", Message: "not found"}
	if !strings.Contains(withCode.Error(), "404") {
		t.Errorf("Error() missing status code: %s", withCode.Error())
	}
	if !strings.Contains(withCode.Error(), "resource.not_found") {
		t.Errorf("Error() missing message code: %s", withCode.Error())
	}

	withoutCode := &APIError{StatusCode: 500, Message: "internal error"}
	if strings.Contains(withoutCode.Error(), "code=") {
		t.Errorf("Error() should omit code= when code is empty: %s", withoutCode.Error())
	}
}

func TestAPIErrorHelpers(t *testing.T) {
	cases := []struct {
		status      int
		notFound    bool
		unauthorized bool
		forbidden   bool
	}{
		{http.StatusNotFound, true, false, false},
		{http.StatusUnauthorized, false, true, false},
		{http.StatusForbidden, false, false, true},
		{http.StatusBadRequest, false, false, false},
	}
	for _, tc := range cases {
		e := &APIError{StatusCode: tc.status}
		if e.IsNotFound() != tc.notFound {
			t.Errorf("IsNotFound() for %d = %v, want %v", tc.status, e.IsNotFound(), tc.notFound)
		}
		if e.IsUnauthorized() != tc.unauthorized {
			t.Errorf("IsUnauthorized() for %d = %v, want %v", tc.status, e.IsUnauthorized(), tc.unauthorized)
		}
		if e.IsForbidden() != tc.forbidden {
			t.Errorf("IsForbidden() for %d = %v, want %v", tc.status, e.IsForbidden(), tc.forbidden)
		}
	}
}

// ─── WithContext propagation ──────────────────────────────────────────────────

func TestWithContextCancelled(t *testing.T) {
	// A server that never responds promptly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := NewClient(srv.URL).WithContext(ctx).WithMaxRetries(0).GetHealth()
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}
