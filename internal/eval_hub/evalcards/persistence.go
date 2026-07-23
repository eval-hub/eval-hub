package evalcards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	evalhubmlflow "github.com/eval-hub/eval-hub/internal/eval_hub/mlflow"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

// Target identifies an evaluation results export destination.
type Target string

const (
	TargetMLflow Target = "mlflow"
	TargetOCI    Target = "oci"
)

// ResultsExporter exports evaluation cards to configured targets.
type ResultsExporter interface {
	Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (cardURL string, err error)
}

// ExportTarget exports an evaluation card to a single target.
type ExportTarget interface {
	Target() Target
	Enabled(job *api.EvaluationJobResource) bool
	Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (cardURL string, err error)
}

// ManagerConfig configures shared dependencies for export targets.
type ManagerConfig struct {
	MLFlowClient        *mlflowclient.Client
	OCIPublisherFactory OCIPublisherFactory
}

// Manager routes evaluation card export to enabled targets.
type Manager struct {
	logger  *slog.Logger
	targets []ExportTarget
}

// NewManager creates a results exporter for the configured targets.
func NewManager(logger *slog.Logger, cfg ManagerConfig) *Manager {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	ociFactory := cfg.OCIPublisherFactory
	if ociFactory == nil {
		ociFactory = NewNoopOCIPublisherFactory()
	}
	targets := []ExportTarget{
		NewMLflowTarget(cfg.MLFlowClient, logger),
		NewOCITarget(ociFactory, logger),
	}
	return &Manager{logger: logger, targets: targets}
}

// Export writes the evaluation card to all targets enabled by the job configuration.
// It returns the card URL from the first successful target that produces one.
// Errors from individual targets are joined; callers may log and ignore them.
func (m *Manager) Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (string, error) {
	if job == nil || card == nil {
		return "", nil
	}
	logger := m.logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	var cardURL string
	var errs []error
	for _, target := range m.targets {
		if !target.Enabled(job) {
			continue
		}
		url, err := target.Export(ctx, job, card)
		if err != nil {
			logger.Error(
				"Failed to export evaluation results",
				"target", target.Target(),
				"job_id", job.Resource.ID,
				"error", err,
			)
			errs = append(errs, fmt.Errorf("%s: %w", target.Target(), err))
			continue
		}
		if url != "" && cardURL == "" {
			cardURL = url
		}
		logger.Info(
			"Exported evaluation results",
			"target", target.Target(),
			"job_id", job.Resource.ID,
		)
	}
	return cardURL, errors.Join(errs...)
}

type mlflowTarget struct {
	client *mlflowclient.Client
	logger *slog.Logger
}

func NewMLflowTarget(client *mlflowclient.Client, logger *slog.Logger) ExportTarget {
	return &mlflowTarget{client: client, logger: logger}
}

func (t *mlflowTarget) Target() Target {
	return TargetMLflow
}

func (t *mlflowTarget) Enabled(job *api.EvaluationJobResource) bool {
	return evalhubmlflow.HasExperimentName(&job.EvaluationJobConfig) && job.Resource.MLFlowExperimentID != ""
}

func (t *mlflowTarget) Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("mlflow client is not configured")
	}

	cardJSON, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal evaluation card: %w", err)
	}

	client := t.client.WithContext(ctx)
	if t.logger != nil {
		client = client.WithLogger(t.logger)
	}
	if !job.Resource.Tenant.IsEmpty() {
		client = client.WithWorkspace(job.Resource.Tenant.String())
	}

	artifactLocation := ""
	if job.Experiment != nil {
		artifactLocation = job.Experiment.ArtifactLocation
	}

	runID, artifactURL, err := evalhubmlflow.PersistEvalCard(
		client,
		job.Resource.MLFlowExperimentID,
		job.Resource.ID,
		job.Name,
		artifactLocation,
		cardJSON,
	)
	if err != nil {
		return "", err
	}

	if runID != "" && job.Results != nil {
		for i := range job.Results.Benchmarks {
			if job.Results.Benchmarks[i].MLFlowRunID == "" {
				job.Results.Benchmarks[i].MLFlowRunID = runID
			}
		}
	}

	if t.logger != nil {
		t.logger.Info(
			"Uploaded evaluation card artifact to MLflow",
			"job_id", job.Resource.ID,
			"experiment_id", job.Resource.MLFlowExperimentID,
			"artifact_url", artifactURL,
			"mlflow_run_id", runID,
		)
	}
	return artifactURL, nil
}

type ociTarget struct {
	factory OCIPublisherFactory
	logger  *slog.Logger
}

func NewOCITarget(factory OCIPublisherFactory, logger *slog.Logger) ExportTarget {
	if factory == nil {
		factory = NewNoopOCIPublisherFactory()
	}
	return &ociTarget{factory: factory, logger: logger}
}

func (t *ociTarget) Target() Target {
	return TargetOCI
}

func (t *ociTarget) Enabled(job *api.EvaluationJobResource) bool {
	return job.Exports != nil && job.Exports.OCI != nil
}

func (t *ociTarget) Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (string, error) {
	publisher, err := t.factory.NewPublisher(ctx, job)
	if err != nil {
		return "", err
	}
	defer publisher.Close()

	cardJSON, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal evaluation card: %w", err)
	}

	if err := publisher.PublishEvalCard(ctx, cardJSON); err != nil {
		return "", err
	}
	if t.logger != nil {
		t.logger.Info("Exported evaluation card to OCI", "job_id", job.Resource.ID)
	}
	return "", nil
}
