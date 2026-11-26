package generation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func validRequest() api.CollectionGenerationRequest {
	return api.CollectionGenerationRequest{
		EvaluationGoal: " Assess safe tool use ",
		CollectionName: " Safety checks ",
		Model: api.ModelRef{
			URL:        "https://model.example/v1",
			Name:       " model ",
			Auth:       &api.ModelAuth{SecretRef: "model-auth"},
			Parameters: map[string]any{"temperature": 0.1},
			CardURL:    "https://example.test/card",
		},
	}
}

func TestNormalizeRequestDefaultsAndRemovesModelControls(t *testing.T) {
	got, err := NormalizeRequest(validRequest())
	if err != nil {
		t.Fatalf("NormalizeRequest(): %v", err)
	}
	if got.EvaluationGoal != "Assess safe tool use" || got.CollectionName != "Safety checks" {
		t.Fatalf("request strings were not normalized: %+v", got)
	}
	if got.MaxBenchmarks != api.DefaultCollectionGenerationBenchmarks || got.Strictness != "moderate" || got.GenerationEffort != "standard" {
		t.Fatalf("defaults not applied: %+v", got)
	}
	if got.Model.Parameters != nil || got.Model.CardURL != "" {
		t.Fatalf("caller-controlled model fields were retained: %+v", got.Model)
	}
	if got.Model.Auth == nil || got.Model.Auth.SecretRef != "model-auth" {
		t.Fatalf("secret reference was not preserved: %+v", got.Model.Auth)
	}
}

func TestNormalizeRequestRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*api.CollectionGenerationRequest)
	}{
		{name: "blank goal", mutate: func(r *api.CollectionGenerationRequest) { r.EvaluationGoal = " \t " }},
		{name: "oversized goal", mutate: func(r *api.CollectionGenerationRequest) {
			r.EvaluationGoal = strings.Repeat("x", api.MaxCollectionGenerationGoalLength+1)
		}},
		{name: "missing model URL", mutate: func(r *api.CollectionGenerationRequest) { r.Model.URL = "" }},
		{name: "malformed model URL", mutate: func(r *api.CollectionGenerationRequest) { r.Model.URL = "https://[" }},
		{name: "unsupported model URL scheme", mutate: func(r *api.CollectionGenerationRequest) { r.Model.URL = "ftp://model.example/v1" }},
		{name: "model URL fragment", mutate: func(r *api.CollectionGenerationRequest) { r.Model.URL = "https://model.example/v1#fragment" }},
		{name: "malformed model URL query", mutate: func(r *api.CollectionGenerationRequest) { r.Model.URL = "https://model.example/v1?bad=%zz" }},
		{name: "credential query parameter", mutate: func(r *api.CollectionGenerationRequest) { r.Model.URL = "https://model.example/v1?api_key=secret" }},
		{name: "embedded URL credentials", mutate: func(r *api.CollectionGenerationRequest) { r.Model.URL = "https://user:password@model.example/v1" }},
		{name: "credential parameter", mutate: func(r *api.CollectionGenerationRequest) {
			r.Model.Parameters = map[string]any{"headers": map[string]any{"Authorization": "secret"}}
		}},
		{name: "credential nested in array parameter", mutate: func(r *api.CollectionGenerationRequest) {
			r.Model.Parameters = map[string]any{"options": []any{map[string]any{"api_key": "secret"}}}
		}},
		{name: "blank secret reference", mutate: func(r *api.CollectionGenerationRequest) { r.Model.Auth = &api.ModelAuth{SecretRef: " "} }},
		{name: "invalid provider filter", mutate: func(r *api.CollectionGenerationRequest) { r.ProviderFilter = "provider-a,,provider-b" }},
		{name: "invalid max benchmarks", mutate: func(r *api.CollectionGenerationRequest) { r.MaxBenchmarks = api.MaxCollectionGenerationBenchmarks + 1 }},
		{name: "invalid strictness", mutate: func(r *api.CollectionGenerationRequest) { r.Strictness = "unsafe" }},
		{name: "invalid effort", mutate: func(r *api.CollectionGenerationRequest) { r.GenerationEffort = "unbounded" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := validRequest()
			tc.mutate(&request)
			if _, err := NormalizeRequest(request); err == nil {
				t.Fatal("NormalizeRequest() succeeded")
			}
		})
	}
}

func TestNewJobPersistsOnlyNormalizedRequestAndImmutableSnapshot(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	budget, err := loop.DefaultPresets().Resolve(loop.EffortHigh, loop.DefaultHardLimits())
	if err != nil {
		t.Fatal(err)
	}
	retention := 12 * time.Hour
	job, err := NewJob("job-1", api.Tenant("tenant-a"), api.User("user-a"), validRequest(), "registry.example/evalhub:v1", budget, "high", retention, now)
	if err != nil {
		t.Fatalf("NewJob(): %v", err)
	}
	if job.Status.State != api.OverallStatePending || len(job.Benchmarks) != 1 || job.Collection != nil || job.Results != nil {
		t.Fatalf("unexpected initial job shape: %+v", job)
	}
	if job.Model.Auth == nil || job.Model.Auth.SecretRef != "model-auth" || job.Model.Parameters != nil {
		t.Fatalf("unexpected persisted model: %+v", job.Model)
	}
	raw := job.Benchmarks[0].Parameters[ConfigParameter].(string)
	if strings.Contains(raw, "temperature") || strings.Contains(raw, "card.example") {
		t.Fatalf("unsafe request fields leaked into snapshot: %s", raw)
	}
	var snapshot JobConfig
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snapshot.Request.Model.CardURL != "" {
		t.Fatalf("caller-controlled model card URL leaked into snapshot: %q", snapshot.Request.Model.CardURL)
	}
	if snapshot.Limits != budget.Limits || snapshot.RunnerImage != "registry.example/evalhub:v1" || snapshot.ReasoningEffort != "high" || snapshot.Retention != retention {
		t.Fatalf("snapshot did not freeze controls: %+v", snapshot)
	}
	if !job.Resource.CreatedAt.Equal(now) || job.Resource.Owner != "user-a" || job.Resource.Tenant != "tenant-a" {
		t.Fatalf("job identity/timestamp mismatch: %+v", job.Resource)
	}
}

func TestNewJobRejectsMissingIdentityImageAndInvalidRequest(t *testing.T) {
	budget, _ := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	valid := validRequest()
	tests := []struct {
		name      string
		id        string
		tenant    api.Tenant
		owner     api.User
		request   api.CollectionGenerationRequest
		image     string
		retention time.Duration
	}{
		{name: "missing id", tenant: "tenant", owner: "user", request: valid, image: "image"},
		{name: "missing tenant", id: "job", owner: "user", request: valid, image: "image"},
		{name: "missing owner", id: "job", tenant: "tenant", request: valid, image: "image"},
		{name: "missing image", id: "job", tenant: "tenant", owner: "user", request: valid},
		{name: "bad request", id: "job", tenant: "tenant", owner: "user", request: api.CollectionGenerationRequest{EvaluationGoal: "x"}, image: "image"},
		{name: "negative retention", id: "job", tenant: "tenant", owner: "user", request: valid, image: "image", retention: -time.Second},
		{name: "oversized snapshot", id: "job", tenant: "tenant", owner: "user", request: valid, image: strings.Repeat("x", maxConfigBytes)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewJob(tc.id, tc.tenant, tc.owner, tc.request, tc.image, budget, "", tc.retention, time.Time{}); err == nil {
				t.Fatal("NewJob() succeeded")
			}
		})
	}
}

func TestNewJobDefaultsRetentionAndCreationTime(t *testing.T) {
	budget, _ := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	job, err := NewJob("job-defaults", "tenant", "user", validRequest(), "image", budget, "", 0, time.Time{})
	if err != nil {
		t.Fatalf("NewJob(): %v", err)
	}
	snapshot, err := ConfigFromJob(&job.EvaluationJobConfig)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Retention != config.DefaultGenerationRetention {
		t.Fatalf("default retention = %s, want %s", snapshot.Retention, config.DefaultGenerationRetention)
	}
	if job.Resource.CreatedAt.IsZero() {
		t.Fatal("zero creation time was not replaced with the current time")
	}
}

func TestRetentionForJobUsesSnapshotOrBoundedDefault(t *testing.T) {
	budget, _ := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	job, err := NewJob("job-retention", "tenant", "user", validRequest(), "image", budget, "", 3*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := RetentionForJob(&job.EvaluationJobConfig); got != 3*time.Hour {
		t.Fatalf("snapshot retention = %s, want 3h", got)
	}
	if got := RetentionForJob(nil); got != config.DefaultGenerationRetention {
		t.Fatalf("nil job retention = %s, want default %s", got, config.DefaultGenerationRetention)
	}
	job.Benchmarks[0].Parameters[ConfigParameter] = "malformed"
	if got := RetentionForJob(&job.EvaluationJobConfig); got != config.DefaultGenerationRetention {
		t.Fatalf("malformed snapshot retention = %s, want default %s", got, config.DefaultGenerationRetention)
	}
}

func TestConfigFromJobValidatesWorkloadAndSnapshot(t *testing.T) {
	budget, _ := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	job, err := NewJob("job-1", "tenant", "user", validRequest(), "image", budget, "", 24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := ConfigFromJob(&job.EvaluationJobConfig)
	if err != nil {
		t.Fatalf("ConfigFromJob(): %v", err)
	}
	if got.Limits != budget.Limits {
		t.Fatalf("limits = %+v, want %+v", got.Limits, budget.Limits)
	}
	job.Benchmarks = nil
	if _, err := ConfigFromJob(&job.EvaluationJobConfig); err == nil {
		t.Fatal("ConfigFromJob accepted a non-generation job")
	}
}

func TestConfigFromJobRejectsMalformedSnapshotAndProviderBuild(t *testing.T) {
	budget, _ := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	job, err := NewJob("job-1", "tenant", "user", validRequest(), "image", budget, "", 24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	validSnapshot := job.Benchmarks[0].Parameters[ConfigParameter]
	tests := []struct {
		name  string
		value any
	}{
		{name: "missing", value: nil},
		{name: "wrong type", value: 3},
		{name: "malformed", value: "{"},
		{name: "too large", value: strings.Repeat("x", maxConfigBytes+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value == nil {
				delete(job.Benchmarks[0].Parameters, ConfigParameter)
			} else {
				job.Benchmarks[0].Parameters[ConfigParameter] = tc.value
			}
			if _, err := ConfigFromJob(&job.EvaluationJobConfig); err == nil {
				t.Fatal("ConfigFromJob() succeeded")
			}
			job.Benchmarks[0].Parameters[ConfigParameter] = validSnapshot
		})
	}
	provider := RuntimeProviderForJob(&job.EvaluationJobConfig)
	if provider == nil || provider.Runtime.K8s.Image != "image" || provider.Runtime.K8s.Entrypoint[0] != RunnerEntrypoint {
		t.Fatalf("runtime provider = %+v", provider)
	}
	delete(job.Benchmarks[0].Parameters, ConfigParameter)
	job.Benchmarks[0].Parameters[legacyConfigParameter] = validSnapshot
	if snapshot, err := ConfigFromJob(&job.EvaluationJobConfig); err != nil || snapshot.RunnerImage != "image" {
		t.Fatalf("read legacy snapshot = %+v, %v", snapshot, err)
	}
	job.Benchmarks[0].Parameters[ConfigParameter] = "{}"
	if RuntimeProviderForJob(&job.EvaluationJobConfig) != nil {
		t.Fatal("runtime provider accepted invalid persisted config")
	}
	if !IsJob(&job.EvaluationJobConfig) || IsJob(nil) {
		t.Fatal("IsJob() did not identify the generation workload correctly")
	}
	if got := workloads.TypeForJob(&job.EvaluationJobConfig); got != workloads.CollectionGeneration {
		t.Fatalf("TypeForJob() = %q", got)
	}
}
