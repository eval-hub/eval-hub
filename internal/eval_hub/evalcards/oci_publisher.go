package evalcards

import (
	"context"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// OCIPublisher publishes evaluation results to an OCI registry for a single evaluation job.
type OCIPublisher interface {
	PublishEvalCard(ctx context.Context, cardJSON []byte) error
	Close() error
}

// OCIPublisherFactory creates per-job OCI publishers using run-specific coordinates and credentials.
type OCIPublisherFactory interface {
	NewPublisher(ctx context.Context, job *api.EvaluationJobResource) (OCIPublisher, error)
}

// noopOCIPublisherFactory is an explicit discard implementation used by tests and optional callers.
type noopOCIPublisherFactory struct{}

// NewNoopOCIPublisherFactory returns a factory that discards OCI exports without error.
func NewNoopOCIPublisherFactory() OCIPublisherFactory {
	return &noopOCIPublisherFactory{}
}

// NewPublisher returns a no-op publisher that skips OCI upload.
func (f *noopOCIPublisherFactory) NewPublisher(_ context.Context, _ *api.EvaluationJobResource) (OCIPublisher, error) {
	return &noopOCIPublisher{}, nil
}

type noopOCIPublisher struct{}

// PublishEvalCard is a no-op used when OCI export is disabled.
func (p *noopOCIPublisher) PublishEvalCard(_ context.Context, _ []byte) error {
	return nil
}

func (p *noopOCIPublisher) Close() error {
	return nil
}
