package evalcards

import (
	"context"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type budgetTarget struct {
	recordingTarget
	export func(context.Context) (*ExportResult, error)
}

func (t *budgetTarget) Export(ctx context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (*ExportResult, error) {
	return t.export(ctx)
}

type budgetFactory struct {
	create func(context.Context) (OCIPublisher, error)
}

func (f budgetFactory) NewPublisher(ctx context.Context, _ *api.EvaluationJobResource) (OCIPublisher, error) {
	return f.create(ctx)
}

type budgetPublisher struct {
	publish func(context.Context) (*api.OCIArtifactReference, error)
}

func (p budgetPublisher) PublishEvalCard(ctx context.Context, _ []byte) (*api.OCIArtifactReference, error) {
	return p.publish(ctx)
}
func (budgetPublisher) Close() error { return nil }

func assertExportBudget(t *testing.T, ctx context.Context, want time.Duration) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	remaining := time.Until(deadline)
	if !ok || remaining <= 0 || remaining > want || remaining < want-time.Second {
		t.Fatalf("remaining budget=%v, want fresh %v", remaining, want)
	}
	if ctx.Err() != nil {
		t.Fatalf("context already canceled: %v", ctx.Err())
	}
}

func TestExportBudgetsAreIndependent(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			base := context.Background()
			var mlflowCtx context.Context
			mlflow := &budgetTarget{recordingTarget: recordingTarget{name: TargetMLflow, enabled: true}, export: func(ctx context.Context) (*ExportResult, error) {
				// MLflow retains its HTTP client timeout without an added export deadline.
				if ctx != base {
					t.Fatal("MLflow received a separate export context")
				}
				mlflowCtx = ctx
				return &ExportResult{CardURL: "mlflow/card"}, nil
			}}
			factory := budgetFactory{create: func(ctx context.Context) (OCIPublisher, error) {
				if ctx != base {
					t.Fatal("publisher initialization received the push deadline")
				}
				if mlflowCtx.Err() != nil {
					t.Fatal("credential lookup inherited a canceled MLflow context")
				}
				return budgetPublisher{publish: func(ctx context.Context) (*api.OCIArtifactReference, error) {
					want := timeout
					if want == 0 {
						want = config.DefaultOCIPushTimeout
					}
					assertExportBudget(t, ctx, want)
					if ctx == mlflowCtx {
						t.Fatal("publication reused an earlier context")
					}
					return &api.OCIArtifactReference{OCIDigest: "sha256:digest", OCIReference: "registry/card"}, nil
				}}, nil
			}}
			manager := NewManager(nil, ManagerConfig{OCIPublisherFactory: factory, OCIPushTimeout: timeout})
			manager.targets[0] = mlflow
			job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}, EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}}}
			result, err := manager.Export(base, job, cards.NewEvaluationCard(job))
			if err != nil || result.OCIArtifact == nil || result.CardURL != "mlflow/card" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if base.Err() != nil {
				t.Fatal("export canceled parent")
			}
		})
	}
}
