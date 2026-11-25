package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/collectiondesign"
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
