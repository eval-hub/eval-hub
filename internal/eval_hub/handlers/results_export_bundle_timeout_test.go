package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type timeoutBundlePublisher func(context.Context) (*api.OCIArtifactReference, error)

func (p timeoutBundlePublisher) NewPublisher(context.Context, *api.EvaluationJobResource) (oci.EvaluationBundlePublisher, error) {
	return p, nil
}

func (p timeoutBundlePublisher) Publish(ctx context.Context, _ *api.EvaluationJobResource, _ *api.OCIArtifactReference) (*api.OCIArtifactReference, error) {
	return p(ctx)
}

func TestBundlePublicationDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
		want time.Duration
	}{
		{name: "default", want: config.DefaultOCIPushTimeout},
		{name: "configured", cfg: &config.Config{Sidecar: &config.SidecarConfig{OCI: &config.SidecarOCIConfig{PushTimeout: time.Minute}}}, want: time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Now()
			h := &Handlers{
				serviceConfig: tc.cfg,
				validate:      newOCIWorkflowValidator(t),
				bundlePublisherFactory: timeoutBundlePublisher(func(ctx context.Context) (*api.OCIArtifactReference, error) {
					deadline, ok := ctx.Deadline()
					if !ok {
						t.Fatal("bundle publication has no deadline")
					}
					if deadline.Before(started.Add(tc.want)) || deadline.After(time.Now().Add(tc.want)) {
						t.Fatalf("bundle deadline = %v, want timeout %v", deadline, tc.want)
					}
					return publishedBundle(), nil
				}),
			}
			if _, err := h.publishEvaluationBundle(context.Background(), completedOCIJob(), publishedCard()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBundlePublicationTimeoutPersistsFailure(t *testing.T) {
	storage := newDetachedExportStorage(completedOCIJob())
	h := &Handlers{
		serviceConfig:   &config.Config{Sidecar: &config.SidecarConfig{OCI: &config.SidecarOCIConfig{PushTimeout: time.Millisecond}}},
		validate:        newOCIWorkflowValidator(t),
		resultsExporter: &ociOutcomeExporter{result: evalcards.ExportResult{OCIArtifact: publishedCard()}},
		bundlePublisherFactory: timeoutBundlePublisher(func(ctx context.Context) (*api.OCIArtifactReference, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
	}
	h.exportEvaluationResultsAsync(context.Background(), storage, storage.job, nil)
	nextStatusWrite(t, storage, api.OCIProcessingRunning)
	failed := nextStatusWrite(t, storage, api.OCIProcessingFailed)
	if !strings.Contains(failed.message, context.DeadlineExceeded.Error()) {
		t.Fatalf("timeout cause missing: %s", failed.message)
	}
	if failed.card == nil || failed.card.OCIDigest != publishedCard().OCIDigest {
		t.Fatal("bundle timeout lost the published card")
	}
}
