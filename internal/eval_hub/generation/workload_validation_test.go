package generation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
		{name: "too many rejected", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Explanation.RejectedBenchmarks = make([]api.CollectionGenerationBenchmarkReason, 6)
		}},
		{name: "too many actions", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Actions = make([]api.CollectionGenerationAction, 51)
		}},
		{name: "schema validation", mutate: func(_ *api.CollectionGenerationRequest, p *api.CollectionGenerationResult) {
			p.Collection.Benchmarks = nil
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
			if err := ValidateResult(request, proposal); err == nil {
				t.Fatal("ValidateResult() succeeded for an invalid proposal")
			}
		})
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

func TestSafeFailureAndFailureFromAdditionalInfo(t *testing.T) {
	for _, code := range []string{"guardrails_rejected", "guardrails_unavailable", "model_failed", "tool_failed", "validation_failed", "no_relevant_benchmarks", "timeout", "launch_failed", "workload_failed"} {
		failure, err := SafeFailure(code, "")
		if err != nil || failure.Code != code || failure.Message == "" {
			t.Errorf("SafeFailure(%q) = (%+v, %v)", code, failure, err)
		}
	}
	if _, err := SafeFailure("unknown", ""); err == nil {
		t.Fatal("unknown failure code was accepted")
	}
	if _, err := SafeFailure("model_failed", "unknown"); err == nil {
		t.Fatal("unknown failure category was accepted")
	}
	for _, category := range []string{"prompt_injection", "exfiltration", "topic_relevance", "unsupported_capability"} {
		if _, err := SafeFailure("guardrails_rejected", category); err != nil {
			t.Errorf("SafeFailure() rejected supported category %q: %v", category, err)
		}
	}
	got, err := FailureFromAdditionalInfo(map[string]any{"collection_generation_error": map[string]any{
		"code": "model_failed", "message": "do not expose model text", "category": "topic_relevance",
	}})
	if err != nil || got.Message != "The generation model request failed." || got.Category != "topic_relevance" {
		t.Fatalf("FailureFromAdditionalInfo() = (%+v, %v)", got, err)
	}
	for _, info := range []map[string]any{
		{},
		{"collection_generation_error": make(chan int)},
		{"collection_generation_error": strings.Repeat("x", 4097)},
		{"collection_generation_error": json.RawMessage("{")},
		{"collection_generation_error": map[string]any{"code": "untrusted"}},
	} {
		if _, err := FailureFromAdditionalInfo(info); err == nil {
			t.Errorf("FailureFromAdditionalInfo(%v) succeeded", info)
		}
	}
}

func TestProgressValidationAndServerTimestamps(t *testing.T) {
	previous := &api.CollectionGenerationProgress{Sequence: 2, Phase: "catalog_discovery", LastCompletedAction: &api.CollectionGenerationProgressAction{Name: "search_benchmarks", CompletedAt: time.Unix(1, 0)}}
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
