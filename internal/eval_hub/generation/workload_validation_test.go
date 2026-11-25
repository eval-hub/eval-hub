package generation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/pkg/api"
	"sigs.k8s.io/yaml"
)

func validResult(t *testing.T) api.CollectionGenerationResult {
	t.Helper()
	collection := api.CollectionConfig{
		Name:        "Generated collection",
		Description: "A concise generated evaluation collection.",
		Domains:     []string{"safety"},
		Benchmarks: []api.CollectionBenchmarkConfig{{
			Ref: api.Ref{ID: "bench-a"}, ProviderID: "provider-a",
		}},
	}
	serialized, err := yaml.Marshal(collection)
	if err != nil {
		t.Fatalf("marshal valid collection YAML: %v", err)
	}
	return api.CollectionGenerationResult{
		Collection: collection,
		YAML:       string(serialized),
		Rationale:  "This benchmark directly measures the requested evaluation goal.",
		Explanation: api.CollectionGenerationExplanation{SelectedBenchmarks: []api.CollectionGenerationBenchmarkReason{{
			ProviderID: "provider-a", ID: "bench-a", Reason: "It directly measures the requested behavior.",
		}}},
		Actions: []api.CollectionGenerationAction{{
			Name: "search_benchmarks", Outcome: "success", Summary: "Search completed.",
		}},
	}
}

func TestValidateResultAcceptsCompleteDryRunProposal(t *testing.T) {
	request := api.CollectionGenerationRequest{MaxBenchmarks: 2}
	if err := ValidateResult(request, validResult(t)); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

func TestResultFromAdditionalInfoBoundsNormalizedSize(t *testing.T) {
	const key = "collection_generation_result"
	request := api.CollectionGenerationRequest{MaxBenchmarks: 1}
	proposal := validResult(t)
	if _, err := ResultFromAdditionalInfo(request, map[string]any{key: proposal}); err != nil {
		t.Fatalf("small proposal rejected: %v", err)
	}
	proposal.Rationale = strings.Repeat("x", 10_000)
	proposal.Actions[0].Summary = "x"
	withPadding := func(size int) []byte {
		t.Helper()
		proposal.Collection.Benchmarks[0].Parameters = map[string]any{"fixture": strings.Repeat("x", size)}
		serialized, err := yaml.Marshal(proposal.Collection)
		if err != nil {
			t.Fatal(err)
		}
		proposal.YAML = string(serialized)
		raw, err := json.Marshal(proposal)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := withPadding(1)
	// Every byte of padding appears once in the structured config and once
	// in the matching YAML. Leave at most two bytes for summary normalization.
	raw = withPadding(1 + (maxResultBytes-len(raw)-1)/2)
	if remaining := maxResultBytes - len(raw); remaining < 1 || remaining > 2 {
		t.Fatalf("fixture has %d bytes left under the proposal size limit", remaining)
	}
	if err := ValidateResult(request, proposal); err != nil {
		t.Fatalf("proposal should pass validation before normalization: %v", err)
	}
	if _, err := ResultFromAdditionalInfo(request, map[string]any{key: proposal}); err == nil || err.Error() != "generation proposal is malformed or exceeds the size limit" {
		t.Fatalf("ResultFromAdditionalInfo() = %v, want size error after normalization", err)
	}
}

func TestValidateResultRejectsInvalidProposalFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*api.CollectionGenerationRequest, *api.CollectionGenerationResult)
	}{
		{name: "name mismatch", mutate: func(r *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			r.CollectionName = "Requested"
		}},
		{name: "description mismatch", mutate: func(r *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			r.CollectionDescription = "Requested description"
		}},
		{name: "curation order", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Collection.CurationOrder = 1
		}},
		{name: "benchmark count", mutate: func(r *api.CollectionGenerationRequest, _ *api.CollectionGenerationResult) { r.MaxBenchmarks = 0 }},
		{name: "too much YAML", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.YAML = strings.Repeat("x", 256*1024+1)
		}},
		{name: "too much rationale", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Rationale = strings.Repeat("x", 10_001)
		}},
		{name: "explanation count", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Explanation.SelectedBenchmarks = nil
		}},
		{name: "selected explanations out of collection order", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Collection.Benchmarks = append(p.Collection.Benchmarks, api.CollectionBenchmarkConfig{
				Ref: api.Ref{ID: "bench-b"}, ProviderID: "provider-b",
			})
			p.Explanation.SelectedBenchmarks = append(p.Explanation.SelectedBenchmarks, api.CollectionGenerationBenchmarkReason{
				ProviderID: "provider-b", ID: "bench-b", Reason: "It measures the second requested behavior.",
			})
			p.Explanation.SelectedBenchmarks[0], p.Explanation.SelectedBenchmarks[1] = p.Explanation.SelectedBenchmarks[1], p.Explanation.SelectedBenchmarks[0]
			serialized, err := yaml.Marshal(p.Collection)
			if err != nil {
				t.Fatalf("marshal reversed-order collection YAML: %v", err)
			}
			p.YAML = string(serialized)
		}},
		{name: "too many rejected", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Explanation.RejectedBenchmarks = make([]api.CollectionGenerationBenchmarkReason, 6)
		}},
		{name: "too many actions", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Actions = make([]api.CollectionGenerationAction, 51)
		}},
		{name: "schema validation", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Collection.Description = strings.Repeat("x", 1025)
			serialized, err := yaml.Marshal(p.Collection)
			if err != nil {
				t.Fatalf("marshal schema-invalid collection YAML: %v", err)
			}
			p.YAML = string(serialized)
		}},
		{name: "invalid YAML", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) { p.YAML = "name: [" }},
		{name: "unknown YAML field", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.YAML = "name: x\nunknown: true\nbenchmarks: []\n"
		}},
		{name: "multiple YAML documents", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.YAML += "\n---\nname: second\n"
		}},
		{name: "YAML mismatch", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.YAML = "name: Different\nbenchmarks:\n- id: bench-a\n  provider_id: provider-a\n"
		}},
		{name: "incomplete benchmark", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Collection.Benchmarks[0].ProviderID = ""
		}},
		{name: "benchmark URL", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Collection.Benchmarks[0].URL = "https://outside.example"
		}},
		{name: "provider filter", mutate: func(r *api.CollectionGenerationRequest, _ *api.CollectionGenerationResult) {
			r.ProviderFilter = "provider-b"
		}},
		{name: "duplicate benchmark", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Collection.Benchmarks = append(p.Collection.Benchmarks, p.Collection.Benchmarks[0])
			p.Explanation.SelectedBenchmarks = append(p.Explanation.SelectedBenchmarks, p.Explanation.SelectedBenchmarks[0])
			serialized, err := yaml.Marshal(p.Collection)
			if err != nil {
				t.Fatalf("marshal duplicate benchmark YAML: %v", err)
			}
			p.YAML = string(serialized)
		}},
		{name: "bad selected explanation", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Explanation.SelectedBenchmarks[0].Reason = " "
		}},
		{name: "long selected explanation", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Explanation.SelectedBenchmarks[0].Reason = strings.Repeat("x", 301)
		}},
		{name: "bad rejected explanation", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{{ProviderID: "provider-a", ID: "bench-b", Reason: " "}}
		}},
		{name: "rejected outside filter", mutate: func(r *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			r.ProviderFilter = "provider-a"
			p.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{{ProviderID: "provider-b", ID: "bench-b", Reason: "Not relevant."}}
		}},
		{name: "duplicate rejected explanation", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			reason := api.CollectionGenerationBenchmarkReason{ProviderID: "provider-a", ID: "bench-b", Reason: "Not relevant."}
			p.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{reason, reason}
		}},
		{name: "selected and rejected", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Explanation.RejectedBenchmarks = []api.CollectionGenerationBenchmarkReason{{ProviderID: "provider-a", ID: "bench-a", Reason: "Rejected."}}
		}},
		{name: "unknown action", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Actions[0].Name = "browse_web"
		}},
		{name: "invalid action outcome", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Actions[0].Outcome = "pending"
		}},
		{name: "empty action summary", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Actions[0].Summary = " "
		}},
		{name: "long action summary", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Actions[0].Summary = strings.Repeat("x", 301)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := api.CollectionGenerationRequest{MaxBenchmarks: 12}
			proposal := validResult(t)
			tc.mutate(&request, &proposal)
			err := ValidateResult(request, proposal)
			if err == nil {
				t.Fatal("ValidateResult() succeeded for an invalid proposal")
			}
			if tc.name == "schema validation" && err.Error() != "proposal collection does not satisfy the collection schema" {
				t.Fatalf("schema validation error = %q", err)
			}
			if tc.name == "duplicate benchmark" && err.Error() != "proposal contains a duplicate benchmark" {
				t.Fatalf("duplicate benchmark error = %q", err)
			}
			if tc.name == "selected explanations out of collection order" && err.Error() != "selected benchmark explanation does not match the proposal" {
				t.Fatalf("selected explanation order error = %q", err)
			}
		})
	}
}

func TestValidateYAMLMatchesCollectionRejectsDuplicateKeys(t *testing.T) {
	proposal := validResult(t)
	proposal.YAML = strings.Replace(proposal.YAML, "name: Generated collection", "name: Generated collection\nname: Replaced collection", 1)
	if err := validateYAMLMatchesCollection(proposal.YAML, proposal.Collection); err == nil {
		t.Fatal("duplicate YAML key was accepted")
	}
}

func TestResultFromAdditionalInfoValidatesAndRedactsActionText(t *testing.T) {
	proposal := validResult(t)
	proposal.Actions[0].Summary = "model-generated text containing a fake credential"
	info := map[string]any{"collection_generation_result": proposal}
	got, err := ResultFromAdditionalInfo(api.CollectionGenerationRequest{MaxBenchmarks: 2}, info)
	if err != nil {
		t.Fatalf("ResultFromAdditionalInfo(): %v", err)
	}
	if got.Actions[0].Summary != "Visible benchmark catalog search completed." {
		t.Fatalf("untrusted action summary was retained: %q", got.Actions[0].Summary)
	}
	if _, err := ResultFromAdditionalInfo(api.CollectionGenerationRequest{}, map[string]any{}); err == nil {
		t.Fatal("missing result was accepted")
	}
	if _, err := ResultFromAdditionalInfo(api.CollectionGenerationRequest{}, map[string]any{"collection_generation_result": make(chan int)}); err == nil {
		t.Fatal("unmarshalable result was accepted")
	}
	if _, err := ResultFromAdditionalInfo(api.CollectionGenerationRequest{}, map[string]any{"collection_generation_result": strings.Repeat("x", maxResultBytes+1)}); err == nil {
		t.Fatal("oversized result was accepted")
	}
	if _, err := ResultFromAdditionalInfo(api.CollectionGenerationRequest{}, map[string]any{"collection_generation_result": json.RawMessage("{")}); err == nil {
		t.Fatal("malformed result was accepted")
	}
}

func TestResultFromAdditionalInfoEncodesRequiredArrays(t *testing.T) {
	proposal := validResult(t)
	proposal.Actions = nil
	proposal.Explanation.RejectedBenchmarks = nil
	result, err := ResultFromAdditionalInfo(api.CollectionGenerationRequest{MaxBenchmarks: 1}, map[string]any{"collection_generation_result": proposal})
	if err != nil {
		t.Fatalf("ResultFromAdditionalInfo(): %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	if actions, ok := shape["actions"].([]any); !ok || actions == nil {
		t.Fatalf("actions must encode as an array: %s", encoded)
	}
	explanation, ok := shape["explanation"].(map[string]any)
	if !ok {
		t.Fatalf("explanation missing: %s", encoded)
	}
	if rejected, ok := explanation["rejected_benchmarks"].([]any); !ok || rejected == nil {
		t.Fatalf("rejected_benchmarks must encode as an array: %s", encoded)
	}
}

func TestResourceFromJobMapsOnlyPublicGenerationFields(t *testing.T) {
	request, err := NormalizeRequest(api.CollectionGenerationRequest{
		EvaluationGoal: "safe tool use", Model: api.ModelRef{URL: "https://model.example/v1", Name: "generator"},
	})
	if err != nil {
		t.Fatal(err)
	}
	limits, err := loop.DefaultPresets().Resolve(loop.EffortStandard, loop.DefaultHardLimits())
	if err != nil {
		t.Fatal(err)
	}
	job, err := NewJob("generation-id", "tenant-a", "owner-a", request, "runner:v1", limits, "", 24*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	job.Status.State = api.OverallStateRunning
	job.Status.CollectionGenerationProgress = &api.CollectionGenerationProgress{
		Sequence: 7, CollectionGenerationStatusProgress: api.CollectionGenerationStatusProgress{Phase: "validation", UpdatedAt: time.Now().UTC()},
	}
	resource, err := ResourceFromJob(job)
	if err != nil {
		t.Fatalf("ResourceFromJob(): %v", err)
	}
	encoded, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	status := shape["status"].(map[string]any)
	progress := status["progress"].(map[string]any)
	if _, exists := progress["sequence"]; exists {
		t.Fatalf("internal sequence counter leaked in public response: %s", encoded)
	}
	if shape["request"] == nil || status["state"] != string(api.OverallStateRunning) {
		t.Fatalf("incomplete public generation resource: %s", encoded)
	}
	job.Status.State = api.OverallStateCompleted
	proposal := validResult(t)
	job.Results = &api.EvaluationJobResults{Benchmarks: []api.BenchmarkResult{{
		ProviderID: ProviderID, ID: BenchmarkID, BenchmarkIndex: 0,
		AdditionalInfo: map[string]any{"collection_generation_result": proposal},
	}}}
	resource, err = ResourceFromJob(job)
	if err != nil || resource.Result == nil || resource.Status.Progress != nil {
		t.Fatalf("completed resource mapping = (%+v, %v)", resource, err)
	}
	for _, state := range []api.OverallState{api.OverallStatePending, api.OverallStateCancelled} {
		job.Status.State = state
		job.Status.CollectionGenerationProgress = nil
		job.Results = nil
		resource, err = ResourceFromJob(job)
		if err != nil || resource.Status.State != state || resource.Result != nil || resource.Status.Error != nil {
			t.Fatalf("%s resource mapping = (%+v, %v)", state, resource, err)
		}
	}
	job.Status.State = api.OverallStateFailed
	job.Results = &api.EvaluationJobResults{Benchmarks: []api.BenchmarkResult{{
		ProviderID: ProviderID, ID: BenchmarkID, BenchmarkIndex: 0,
		AdditionalInfo: map[string]any{"collection_generation_error": api.CollectionGenerationError{Code: "model_failed", Message: "The generation model failed."}},
	}}}
	resource, err = ResourceFromJob(job)
	if err != nil || resource.Status.State != api.OverallStateFailed || resource.Status.Error == nil || resource.Status.Error.Code != "model_failed" {
		t.Fatalf("failed resource mapping = (%+v, %v)", resource, err)
	}
	job.Status.State = api.OverallStateCompleted
	job.Results.Benchmarks[0].AdditionalInfo = map[string]any{}
	if _, err := ResourceFromJob(job); err == nil {
		t.Fatal("ResourceFromJob() accepted an invalid stored completed result")
	}
}

func TestSafeFailureAndFailureFromAdditionalInfo(t *testing.T) {
	for _, code := range []string{"guardrails_unavailable", "model_failed", "tool_failed", "validation_failed", "no_relevant_benchmarks", "timeout", "launch_failed", "workload_failed"} {
		failure, err := SafeFailure(code, "")
		if err != nil || failure.Code != code || failure.Message == "" {
			t.Errorf("SafeFailure(%q) = (%+v, %v)", code, failure, err)
		}
	}
	if _, err := SafeFailure("unknown", ""); err == nil {
		t.Fatal("unknown failure code was accepted")
	}
	if _, err := SafeFailure("guardrails_rejected", "unknown"); err == nil {
		t.Fatal("unknown failure category was accepted")
	}
	for _, category := range []string{"prompt_injection", "exfiltration", "topic_relevance", "unsupported_capability"} {
		if _, err := SafeFailure("guardrails_rejected", category); err != nil {
			t.Errorf("SafeFailure() rejected supported category %q: %v", category, err)
		}
	}
	got, err := FailureFromAdditionalInfo(map[string]any{"collection_generation_error": map[string]any{
		"code": "guardrails_rejected", "message": "do not expose model text", "category": "topic_relevance",
	}})
	if err != nil || got.Message != "The request was rejected by generation safety checks." || got.Category != "topic_relevance" {
		t.Fatalf("FailureFromAdditionalInfo() = (%+v, %v)", got, err)
	}
	for _, info := range []map[string]any{
		{},
		{"collection_generation_error": make(chan int)},
		{"collection_generation_error": strings.Repeat("x", 4097)},
		{"collection_generation_error": json.RawMessage("{")},
		{"collection_generation_error": map[string]any{"code": "untrusted"}},
		{"collection_generation_error": map[string]any{"code": "guardrails_rejected"}},
		{"collection_generation_error": map[string]any{"code": "model_failed", "category": "topic_relevance"}},
	} {
		if _, err := FailureFromAdditionalInfo(info); err == nil {
			t.Errorf("FailureFromAdditionalInfo(%v) succeeded", info)
		}
	}
}

func TestProgressValidationAndServerTimestamps(t *testing.T) {
	previous := &api.CollectionGenerationProgress{Sequence: 2, CollectionGenerationStatusProgress: api.CollectionGenerationStatusProgress{Phase: "catalog_discovery", LastCompletedAction: &api.CollectionGenerationProgressAction{Name: "search_benchmarks", CompletedAt: time.Unix(1, 0)}}}
	for _, event := range []api.CollectionGenerationProgressEvent{
		{Sequence: 0, Phase: "guardrails"},
		{Sequence: 1, Phase: "unknown"},
		{Sequence: 3, Phase: "validation", LastCompletedAction: "browse_web"},
	} {
		if err := ValidateProgressEvent(event, nil); err == nil {
			t.Errorf("ValidateProgressEvent(%+v) accepted invalid event", event)
		}
	}
	if err := ValidateProgressEvent(api.CollectionGenerationProgressEvent{Sequence: 2, Phase: "validation"}, previous); err == nil {
		t.Fatal("non-increasing sequence was accepted")
	}
	if err := ValidateProgressEvent(api.CollectionGenerationProgressEvent{Sequence: 3, Phase: "guardrails"}, previous); err == nil {
		t.Fatal("backward phase was accepted")
	}
	valid := api.CollectionGenerationProgressEvent{Sequence: 3, Phase: "validation", LastCompletedAction: "validate_proposal"}
	if err := ValidateProgressEvent(valid, previous); err != nil {
		t.Fatalf("valid progress rejected: %v", err)
	}
	correction := api.CollectionGenerationProgress{Sequence: 3, CollectionGenerationStatusProgress: api.CollectionGenerationStatusProgress{Phase: "validation"}}
	if err := ValidateProgressEvent(api.CollectionGenerationProgressEvent{Sequence: 4, Phase: "model_generation"}, &correction); err != nil {
		t.Fatalf("validation-to-model correction phase rejected: %v", err)
	}
	secondValidation := api.CollectionGenerationProgress{Sequence: 4, CollectionGenerationStatusProgress: api.CollectionGenerationStatusProgress{Phase: "model_generation"}}
	if err := ValidateProgressEvent(api.CollectionGenerationProgressEvent{Sequence: 5, Phase: "validation"}, &secondValidation); err != nil {
		t.Fatalf("repeated validation phase rejected: %v", err)
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 123, time.FixedZone("EST", -5*60*60))
	progress := ProgressFromEvent(api.CollectionGenerationProgressEvent{Sequence: 4, Phase: "finalization"}, previous, now)
	if progress.Sequence != 4 || progress.Phase != "finalization" || progress.UpdatedAt.Location() != time.UTC || progress.LastCompletedAction != previous.LastCompletedAction {
		t.Fatalf("ProgressFromEvent() = %+v", progress)
	}
	progress = ProgressFromEvent(valid, nil, now)
	if progress.LastCompletedAction == nil || progress.LastCompletedAction.Name != "validate_proposal" || !progress.LastCompletedAction.CompletedAt.Equal(now) {
		t.Fatalf("progress action was not server timestamped: %+v", progress.LastCompletedAction)
	}
}

func TestSafeActionSummaryAndAllowlist(t *testing.T) {
	for _, action := range []string{"guardrails_check", "search_benchmarks", "get_provider_details", "list_collections", "validate_proposal", "calibrate_proposal", "finalize_proposal"} {
		if !isAllowedAction(action) || safeActionSummary(action, "success") == "" || safeActionSummary(action, "error") == "" {
			t.Errorf("action %q not handled by allowlist/summary", action)
		}
	}
	if isAllowedAction("browse_web") || safeActionSummary("future_action", "success") == "" {
		t.Fatal("unknown action handling is incorrect")
	}
}
