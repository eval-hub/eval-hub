package features

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/cucumber/godog"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/pkg/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const collectionGenerationPath = "/api/v1/evaluations/collections/generation"

type collectionGenerationScenario struct {
	*scenarioConfig
	admission      api.CollectionGenerationResource
	location       string
	completed      *api.CollectionGenerationResult
	collectionJSON json.RawMessage
}

func InitializeCollectionGenerationSteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {
	sc := &collectionGenerationScenario{scenarioConfig: tc}
	ctx.Step(`^the live collection generation fixture is configured$`, generationFixtureConfigured)
	ctx.Step(`^the unconfigured generation target is selected$`, func() error {
		if os.Getenv("FVT_GENERATION_UNCONFIGURED") != "true" {
			return godog.ErrSkip
		}
		return nil
	})
	ctx.Step(`^I prepare a generation input of (\d+) characters$`, func(length int) error {
		tc.values["generation_long_input"] = strings.Repeat("a", length)
		return nil
	})
	ctx.Step(`^I submit a collection generation request with:$`, sc.submit)
	ctx.Step(`^the collection generation request should be admitted$`, sc.checkAdmission)
	ctx.Step(`^the collection generation request should be rejected with (\d+)$`, sc.checkAdmissionRejection)
	ctx.Step(`^I poll the generation Location until it is "([^"]*)"$`, sc.poll)
	ctx.Step(`^the generated proposal should match the collection creation contract$`, sc.checkProposal)
	ctx.Step(`^the generation should use all visible providers$`, func() error {
		if sc.admission.Request.ProviderFilter != "" {
			return fmt.Errorf("unfiltered generation defaulted to %q", sc.admission.Request.ProviderFilter)
		}
		return nil
	})
	ctx.Step(`^the rejected generation benchmarks should be ordered as "([^"]*)"$`, sc.checkRejectedOrder)
	ctx.Step(`^the generated proposal should select benchmarks from both "([^"]*)" and "([^"]*)"$`, sc.checkSelectedProviders)
	ctx.Step(`^the completed generation should remain readable after its pod exits$`, sc.checkDurability)
	ctx.Step(`^the generation should leave no collection or ordinary evaluation job$`, sc.checkDryRun)
	ctx.Step(`^I submit the proposal as a separate collection$`, sc.createCollection)
	ctx.Step(`^I send a (GET|DELETE) generation request as "([^"]*)"$`, sc.requestAs)
	ctx.Step(`^the guardrail should reject generation with category "([^"]*)"$`, sc.checkGuardrailRejection)
}

func generationFixtureConfigured() error {
	if os.Getenv("FVT_GENERATION_MODEL_URL") == "" {
		return godog.ErrSkip
	}
	for _, key := range []string{"SERVER_URL", "X_TENANT", "FVT_GENERATION_MODEL_NAME"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return fmt.Errorf("%s is required for live collection generation FVT", key)
		}
	}
	return nil
}

func (sc *collectionGenerationScenario) submit(ctx context.Context, doc *godog.DocString) error {
	body, err := sc.substituteValues(doc.Content)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		return fmt.Errorf("invalid collection generation test fixture: %w", err)
	}
	if fields == nil {
		return fmt.Errorf("collection generation fixture must be a JSON object")
	}
	if _, ok := fields["model"]; !ok {
		model := api.ModelRef{Name: os.Getenv("FVT_GENERATION_MODEL_NAME"), URL: os.Getenv("FVT_GENERATION_MODEL_URL")}
		if secret := os.Getenv("FVT_GENERATION_MODEL_SECRET_REF"); secret != "" {
			model.Auth = &api.ModelAuth{SecretRef: secret}
		}
		fields["model"], err = json.Marshal(model)
		if err != nil {
			return err
		}
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return sc.sendScenarioRequest(ctx, http.MethodPost, joinBaseURL(sc.apiFeature.baseURL, collectionGenerationPath), bytes.NewReader(payload), false)
}

func (sc *collectionGenerationScenario) checkAdmission() error {
	if err := sc.theResponseStatusShouldBe(http.StatusAccepted); err != nil {
		return err
	}
	if err := decodeGenerationResponse(sc.body, &sc.admission); err != nil {
		return err
	}
	resource := sc.admission.Resource
	if resource.ID == "" || resource.Tenant.String() != sc.reqHeaders["X-Tenant"] ||
		resource.Owner == "" || resource.CreatedAt.IsZero() || resource.UpdatedAt.Before(resource.CreatedAt) {
		return fmt.Errorf("invalid admitted generation identity or timestamps")
	}
	sc.location = sc.response.Header.Get("Location")
	want := collectionGenerationPath + "/" + url.PathEscape(resource.ID)
	if sc.location != want || sc.admission.Status.State != api.OverallStatePending {
		return fmt.Errorf("expected a pending generation at %q, got state %q and Location %q", want, sc.admission.Status.State, sc.location)
	}
	return nil
}

func (sc *collectionGenerationScenario) checkAdmissionRejection(status int) error {
	if err := sc.theResponseStatusShouldBe(status); err != nil {
		return err
	}
	if sc.response.Header.Get("Location") != "" {
		return fmt.Errorf("rejected generation returned a polling Location")
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(sc.body, &response); err != nil {
		return err
	}
	if _, admitted := response["resource"]; admitted {
		return fmt.Errorf("rejected generation returned a resource")
	}
	return nil
}

func decodeGenerationResponse(body []byte, response *api.CollectionGenerationResource) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(response); err != nil {
		return fmt.Errorf("decode generation resource: %w", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	_, hasResult := envelope["result"]
	if response.Status.State == api.OverallStateCompleted {
		if response.Result == nil {
			return fmt.Errorf("completed generation omitted its proposal")
		}
	} else if hasResult {
		return fmt.Errorf("generation in state %q returned a proposal", response.Status.State)
	}
	if (response.Status.State == api.OverallStateFailed) != (response.Status.Error != nil) {
		return fmt.Errorf("generation in state %q has an invalid error envelope", response.Status.State)
	}
	return checkGenerationProgress(response.Status)
}

func checkGenerationProgress(status api.CollectionGenerationStatus) error {
	progress := status.Progress
	if progress == nil {
		return nil
	}
	if status.State != api.OverallStateRunning {
		return fmt.Errorf("progress was returned in state %q", status.State)
	}
	phases := []string{"guardrails", "catalog_discovery", "model_generation", "validation", "finalization"}
	if !slices.Contains(phases, progress.Phase) || progress.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid generation progress phase or update timestamp")
	}
	if action := progress.LastCompletedAction; action != nil {
		actions := []string{"guardrails_check", "search_benchmarks", "get_provider_details", "list_collections", "validate_proposal", "calibrate_proposal", "finalize_proposal"}
		if !slices.Contains(actions, action.Name) || action.CompletedAt.IsZero() || action.CompletedAt.After(progress.UpdatedAt) {
			return fmt.Errorf("invalid last completed generation action or timestamp")
		}
	}
	return nil
}

func (sc *collectionGenerationScenario) readGeneration(ctx context.Context) (api.CollectionGenerationResource, error) {
	var polled api.CollectionGenerationResource
	if sc.location == "" {
		return polled, fmt.Errorf("no generation Location was admitted")
	}
	if err := sc.iSendARequestWithContext(ctx, http.MethodGet, sc.location, "", "poll collection generation"); err != nil {
		return polled, err
	}
	if err := sc.theResponseStatusShouldBe(http.StatusOK); err != nil {
		return polled, err
	}
	if err := decodeGenerationResponse(sc.body, &polled); err != nil {
		return polled, err
	}
	admitted := sc.admission
	if polled.Resource.ID != admitted.Resource.ID || polled.Resource.Tenant != admitted.Resource.Tenant ||
		polled.Resource.Owner != admitted.Resource.Owner || !polled.Resource.CreatedAt.Equal(admitted.Resource.CreatedAt) ||
		!reflect.DeepEqual(polled.Request, admitted.Request) {
		return polled, fmt.Errorf("polled generation changed the admitted identity or normalized request")
	}
	return polled, nil
}

func (sc *collectionGenerationScenario) poll(ctx context.Context, expected string) error {
	var lastState api.OverallState
	err := wait.PollUntilContextTimeout(ctx, sc.waitInterval, sc.waitDeadline, true, func(ctx context.Context) (bool, error) {
		polled, err := sc.readGeneration(ctx)
		if err != nil {
			return false, err
		}
		lastState = polled.Status.State
		switch lastState {
		case api.OverallStatePending, api.OverallStateRunning, api.OverallStateCompleted, api.OverallStateFailed, api.OverallStateCancelled:
		default:
			return false, fmt.Errorf("unexpected generation state %q", lastState)
		}
		if lastState.String() == expected {
			sc.completed = polled.Result
			return true, nil
		}
		if lastState.IsTerminalState() {
			return false, fmt.Errorf("unexpected terminal generation state %q (%+v)", lastState, polled.Status.Error)
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for generation %q to reach %q (last state %q): %w", sc.admission.Resource.ID, expected, lastState, err)
	}
	if sc.completed != nil {
		var wire struct {
			Result struct {
				Collection json.RawMessage `json:"collection"`
			} `json:"result"`
		}
		if err := json.Unmarshal(sc.body, &wire); err != nil {
			return err
		}
		sc.collectionJSON = wire.Result.Collection
	}
	return nil
}

func (sc *collectionGenerationScenario) checkProposal(ctx context.Context) error {
	result := sc.completed
	if result == nil || result.Actions == nil || result.Explanation.RejectedBenchmarks == nil {
		return fmt.Errorf("completed proposal omitted its result, action array, or rejected benchmark array")
	}
	if err := generation.ValidateResult(sc.admission.Request, *result); err != nil {
		return fmt.Errorf("invalid completed proposal: %w", err)
	}

	// Read the catalog under the same tenant and preserve the poll response for subsequent steps.
	response, body, method, endpoint := sc.response, sc.body, sc.lastMethod, sc.lastURL
	defer func() {
		sc.response, sc.body, sc.lastMethod, sc.lastURL = response, body, method, endpoint
	}()
	catalog := make(map[string]map[string][]string)
	check := func(providerID, benchmarkID string, primary *api.PrimaryScore) error {
		if catalog[providerID] == nil {
			path := "/api/v1/evaluations/providers/" + url.PathEscape(providerID)
			if err := sc.iSendARequestWithContext(ctx, http.MethodGet, path, "", "verify proposal catalog"); err != nil {
				return err
			}
			if err := sc.theResponseStatusShouldBe(http.StatusOK); err != nil {
				return err
			}
			var provider api.ProviderResource
			if err := json.Unmarshal(sc.body, &provider); err != nil {
				return err
			}
			catalog[providerID] = make(map[string][]string, len(provider.Benchmarks))
			for _, benchmark := range provider.Benchmarks {
				metrics := slices.Clone(benchmark.Metrics)
				if benchmark.PrimaryScore != nil {
					metrics = append(metrics, benchmark.PrimaryScore.Metric)
				}
				catalog[providerID][benchmark.ID] = metrics
			}
		}
		metrics, found := catalog[providerID][benchmarkID]
		if !found {
			return fmt.Errorf("proposal referenced invisible benchmark %s/%s", providerID, benchmarkID)
		}
		if primary != nil && !slices.Contains(metrics, primary.Metric) {
			return fmt.Errorf("proposal used unadvertised metric %q for %s/%s", primary.Metric, providerID, benchmarkID)
		}
		return nil
	}
	for _, benchmark := range result.Collection.Benchmarks {
		if err := check(benchmark.ProviderID, benchmark.ID, benchmark.PrimaryScore); err != nil {
			return err
		}
	}
	for _, reason := range result.Explanation.RejectedBenchmarks {
		if err := check(reason.ProviderID, reason.ID, nil); err != nil {
			return err
		}
	}
	return nil
}

func (sc *collectionGenerationScenario) checkRejectedOrder(expected string) error {
	expected, err := sc.getValue(expected)
	if err != nil {
		return err
	}
	var ids []string
	for _, rejection := range sc.completed.Explanation.RejectedBenchmarks {
		ids = append(ids, rejection.ID)
	}
	if strings.Join(ids, ",") != expected {
		return fmt.Errorf("rejected benchmarks = %v, want %q in ranking order", ids, expected)
	}
	return nil
}

func (sc *collectionGenerationScenario) checkSelectedProviders(first, second string) error {
	var providers []string
	for _, benchmark := range sc.completed.Collection.Benchmarks {
		providers = append(providers, benchmark.ProviderID)
	}
	if !slices.Contains(providers, first) || !slices.Contains(providers, second) {
		return fmt.Errorf("proposal must select from both %q and %q, got %v", first, second, providers)
	}
	return nil
}

func (sc *collectionGenerationScenario) checkGuardrailRejection(category string) error {
	var polled api.CollectionGenerationResource
	if err := decodeGenerationResponse(sc.body, &polled); err != nil {
		return err
	}
	failure := polled.Status.Error
	if failure == nil || failure.Code != "guardrails_rejected" || failure.Category != category {
		return fmt.Errorf("expected guardrails_rejected with category %q, got %+v", category, failure)
	}
	if strings.TrimSpace(failure.Message) == "" || strings.Contains(failure.Message, sc.admission.Request.EvaluationGoal) {
		return fmt.Errorf("guardrail error message was empty or echoed the submitted goal")
	}
	return nil
}

func (sc *collectionGenerationScenario) checkDurability(ctx context.Context) error {
	if err := sc.waitForCompletedWorkload(ctx); err != nil {
		return err
	}
	polled, err := sc.readGeneration(ctx)
	if err != nil {
		return err
	}
	if polled.Status.State != api.OverallStateCompleted || !reflect.DeepEqual(polled.Result, sc.completed) {
		return fmt.Errorf("completed proposal changed after the Kubernetes Job finished")
	}
	return nil
}

func (sc *collectionGenerationScenario) waitForCompletedWorkload(ctx context.Context) error {
	k8s, err := newFVTK8sClient()
	if err != nil {
		return err
	}
	resource := sc.admission.Resource
	selector := "job_id=" + resource.ID
	err = wait.PollUntilContextTimeout(ctx, sc.waitInterval, sc.waitDeadline, true, func(ctx context.Context) (bool, error) {
		jobs, err := k8s.listJobs(ctx, resource.Tenant.String(), selector)
		if err != nil {
			return false, err
		}
		if len(jobs) > 1 {
			return false, fmt.Errorf("generation %q created %d Kubernetes Jobs", resource.ID, len(jobs))
		}
		if len(jobs) == 0 {
			return false, nil
		}
		for _, condition := range jobs[0].Status.Conditions {
			if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for the Kubernetes Job for generation %q to complete: %w", resource.ID, err)
	}
	return nil
}

func (sc *collectionGenerationScenario) checkDryRun(ctx context.Context) error {
	query := url.Values{"scope": {"tenant"}, "name": {sc.completed.Collection.Name}}
	if err := sc.iSendARequestWithContext(ctx, http.MethodGet, "/api/v1/evaluations/collections?"+query.Encode(), "", "verify dry run"); err != nil {
		return err
	}
	if err := sc.theResponseStatusShouldBe(http.StatusOK); err != nil {
		return err
	}
	var collections api.CollectionResourceList
	if err := json.Unmarshal(sc.body, &collections); err != nil {
		return err
	}
	if collections.TotalCount != 0 || len(collections.Items) != 0 {
		return fmt.Errorf("generated collection %q already exists before collection creation", sc.completed.Collection.Name)
	}
	id := sc.admission.Resource.ID
	jobsPath := "/api/v1/evaluations/jobs"
	if err := sc.iSendARequestWithContext(ctx, http.MethodGet, jobsPath+"/"+url.PathEscape(id), "", "verify ordinary job GET"); err != nil {
		return err
	}
	if err := sc.theResponseStatusShouldBe(http.StatusNotFound); err != nil {
		return err
	}
	next := jobsPath + "?limit=100"
	visited := map[string]bool{}
	for next != "" {
		href, err := url.Parse(next)
		if err != nil || href.IsAbs() || href.Host != "" || href.Path != jobsPath || visited[next] {
			return fmt.Errorf("invalid ordinary job list pagination link %q", next)
		}
		visited[next] = true
		if err := sc.iSendARequestWithContext(ctx, http.MethodGet, next, "", "verify ordinary job list"); err != nil {
			return err
		}
		if err := sc.theResponseStatusShouldBe(http.StatusOK); err != nil {
			return err
		}
		var page api.EvaluationJobResourceList
		if err := json.Unmarshal(sc.body, &page); err != nil {
			return err
		}
		for _, job := range page.Items {
			if job.Resource.ID == id {
				return fmt.Errorf("ordinary job list exposed generation %q", id)
			}
		}
		if page.Next == nil {
			return nil
		}
		next = page.Next.Href
	}
	return nil
}

func (sc *collectionGenerationScenario) createCollection(ctx context.Context) error {
	collection := sc.completed.Collection
	path := "/api/v1/evaluations/collections"
	if err := sc.sendScenarioRequest(ctx, http.MethodPost, joinBaseURL(sc.apiFeature.baseURL, path), bytes.NewReader(sc.collectionJSON), false); err != nil {
		return err
	}
	if err := sc.theResponseStatusShouldBe(http.StatusCreated); err != nil {
		return err
	}
	if err := sc.iSendARequestWithContext(ctx, http.MethodGet, path+"/{id}", "", "read separately created collection"); err != nil {
		return err
	}
	if err := sc.theResponseStatusShouldBe(http.StatusOK); err != nil {
		return err
	}
	var created api.CollectionResource
	if err := json.Unmarshal(sc.body, &created); err != nil {
		return err
	}
	sameBenchmark := func(a, b api.CollectionBenchmarkConfig) bool {
		return a.ProviderID == b.ProviderID && a.ID == b.ID
	}
	if created.Resource.ID == sc.admission.Resource.ID || created.Name != collection.Name ||
		created.Description != collection.Description || !slices.EqualFunc(created.Benchmarks, collection.Benchmarks, sameBenchmark) ||
		(created.Status != nil && created.Status.RunCount != 0) {
		return fmt.Errorf("created collection does not match the proposal or has recorded evaluation runs")
	}
	return nil
}

func (sc *collectionGenerationScenario) requestAs(ctx context.Context, method, identity string) error {
	path := sc.location
	if path == "" {
		path = collectionGenerationPath + "/00000000-0000-0000-0000-000000000000"
	}
	headers := sc.reqHeaders
	sc.reqHeaders = map[string]string{"X-Tenant": headers["X-Tenant"]}
	defer func() { sc.reqHeaders = headers }()
	var tokenKey string
	switch identity {
	case "same-tenant reader":
		tokenKey = "FVT_GENERATION_READER_TOKEN"
	case "forbidden caller":
		tokenKey = "FVT_GENERATION_FORBIDDEN_TOKEN"
	case "unauthenticated caller":
		sc.reqHeaders["Authorization"] = ""
	default:
		return fmt.Errorf("unknown generation test identity %q", identity)
	}
	if tokenKey != "" {
		token := os.Getenv(tokenKey)
		if token == "" {
			return fmt.Errorf("%s is required for the %s generation test", tokenKey, identity)
		}
		sc.reqHeaders["Authorization"] = "Bearer " + token
	}
	if identity == "same-tenant reader" && method == http.MethodGet {
		polled, err := sc.readGeneration(ctx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(polled.Result, sc.completed) {
			return fmt.Errorf("same-tenant reader received a different proposal")
		}
		return nil
	}
	return sc.iSendARequestWithContext(ctx, method, path, "", "verify collection generation authorization")
}
