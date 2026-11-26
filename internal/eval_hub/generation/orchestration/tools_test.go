package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/collectiondesign"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCatalogPagesKeepFirstBenchmarkAndAdvance(t *testing.T) {
	t.Parallel()
	for _, rich := range []bool{false, true} {
		t.Run(fmt.Sprintf("rich_metadata=%t", rich), func(t *testing.T) {
			source := testCatalog()
			provider := &source.providers[0]
			provider.Benchmarks = provider.Benchmarks[:2]
			first := &provider.Benchmarks[0]
			if rich {
				// Even before JSON escaping and framing, these fields take
				// the full page budget. '<' increases the encoded size.
				metadata := []string{
					strings.Repeat("<", 96), strings.Repeat("a", 96),
					strings.Repeat("b", 96), strings.Repeat("c", 96),
				}
				first.Domains = metadata
				first.Tasks = metadata
				first.Modalities = metadata
				first.EvaluationTargets = metadata
				first.Metrics = metadata
			}
			tools := catalogTools{source: source}
			for _, call := range []struct {
				name string
				run  loop.ToolHandler
				args string
			}{
				{searchBenchmarks, tools.search, `"query":"toxicity",`},
				{getProviderDetails, tools.provider, `"provider_id":"safety",`},
			} {
				t.Run(call.name, func(t *testing.T) {
					for offset, id := range []string{"toxicity-a", "toxicity-b"} {
						args := fmt.Sprintf(`{%s"limit":1,"offset":%d}`, call.args, offset)
						result, err := call.run(context.Background(), json.RawMessage(args))
						if err != nil {
							t.Fatal(err)
						}
						next := 0
						if offset == 0 {
							next = 1
						}
						assertBenchmarkPage(t, result, id, next, !rich || offset > 0)
					}
				})
			}

			t.Run("initial_catalog_evidence", func(t *testing.T) {
				snapshot := jobSnapshot(t)
				snapshot.Request.MaxBenchmarks = 1
				options, err := collectiondesign.ParseOptions(snapshot.Request.EvaluationGoal, "", "", "")
				if err != nil {
					t.Fatal(err)
				}
				candidates, issue := discover(source.providers, options)
				checkEqual(t, "", issue)
				job := session{request: snapshot.Request, options: options}
				input, err := job.input(candidates, "", loop.Budget{Limits: snapshot.Limits})
				if err != nil {
					t.Fatal(err)
				}
				var prompt struct {
					Evidence json.RawMessage `json:"initial_catalog_evidence"`
				}
				if err := json.Unmarshal([]byte(input.UserPrompt), &prompt); err != nil {
					t.Fatal(err)
				}
				assertBenchmarkPage(t, prompt.Evidence, "toxicity-a", 1, !rich)
			})
		})
	}
}

func TestCatalogToolsEnforceProviderScope(t *testing.T) {
	t.Parallel()
	source := testCatalog()
	outsideProvider := source.providers[0]
	outsideProvider.Resource.ID = "outside"
	source.providers = append(source.providers, outsideProvider)
	outsideCollection := source.collections[0]
	outsideCollection.Resource.ID = "outside-collection"
	outsideCollection.Benchmarks = []api.CollectionBenchmarkConfig{{
		Ref: api.Ref{ID: "toxicity-a"}, ProviderID: "outside",
	}}
	source.collections = append(source.collections, outsideCollection)
	options, err := collectiondesign.ParseOptions("Evaluate toxicity", "safety", "", "")
	if err != nil {
		t.Fatal(err)
	}
	tools := catalogTools{source: source, options: options}
	ctx := context.Background()

	for _, call := range []loop.ToolHandler{tools.search, tools.provider} {
		if result, err := call(ctx, json.RawMessage(`{"provider_id":"outside"}`)); result != nil || err == nil ||
			err.Error() != "provider is outside the request filter" {
			t.Fatalf("out-of-scope provider request = %s, %v", result, err)
		}
	}
	checkEqual(t, 0, source.providerReads)
	checkEqual(t, 0, source.details)

	result, err := tools.search(ctx, json.RawMessage(`{"query":"toxicity","tag":"toxicity"}`))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items      []benchmarkSummary `json:"items"`
		NextOffset int                `json:"next_offset"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, 3, page.NextOffset)
	checkEqual(t, 3, len(page.Items))
	for _, item := range page.Items {
		checkEqual(t, "safety", item.ProviderID)
	}

	result, err = tools.collections(ctx, json.RawMessage(`{"offset":2}`))
	if err != nil {
		t.Fatal(err)
	}
	checkEqual(t, `{"items":[]}`, string(result))

	source.providers[0].Runtime.K8s = nil
	if result, err := tools.provider(ctx, json.RawMessage(`{"provider_id":"safety"}`)); result != nil || err == nil ||
		err.Error() != "provider is unavailable" {
		t.Fatalf("unavailable provider request = %s, %v", result, err)
	}
}

func TestCatalogToolsPropagateReadFailuresWithoutPartialResults(t *testing.T) {
	t.Parallel()
	catalogErr := errors.New("catalog read failed")
	source := testCatalog()
	source.providersError = catalogErr
	source.collectionsError = catalogErr
	tools := catalogTools{source: source}
	for _, tc := range []struct {
		name string
		call loop.ToolHandler
		args json.RawMessage
	}{
		{searchBenchmarks, tools.search, json.RawMessage(`{}`)},
		{listCollections, tools.collections, json.RawMessage(`{}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if result, err := tc.call(context.Background(), tc.args); result != nil || !errors.Is(err, catalogErr) {
				t.Fatalf("read failure = %s, %v, want no partial results and %v", result, err, catalogErr)
			}
		})
	}
}

func TestCatalogPageStopsBeforeSkippingSecondItem(t *testing.T) {
	t.Parallel()
	item := strings.Repeat("x", toolResultBytes/2)
	result, err := boundedPage(toolPage{Limit: 2}, 3, func(int) string { return item }, "")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items      []string `json:"items"`
		NextOffset int      `json:"next_offset"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, []string{item}, page.Items)
	checkEqual(t, 1, page.NextOffset)
}

func assertBenchmarkPage(t *testing.T, data json.RawMessage, id string, next int, wantFullMetadata bool) {
	t.Helper()
	if len(data) > toolResultBytes {
		t.Fatalf("page is %d bytes, limit is %d", len(data), toolResultBytes)
	}
	var page struct {
		Items      []benchmarkSummary `json:"items"`
		NextOffset int                `json:"next_offset"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, next, page.NextOffset)
	checkEqual(t, 1, len(page.Items))
	item := page.Items[0]
	checkEqual(t, id, item.ID)
	checkEqual(t, "safety", item.ProviderID)
	if wantFullMetadata {
		checkEqual(t, []string{"safety"}, item.Domains)
		checkEqual(t, []string{"risk"}, item.Metrics)
	}
}
