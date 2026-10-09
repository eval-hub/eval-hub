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

// ExportResult retains each destination's outcome independently.
type ExportResult struct {
	CardURL     string
	OCIArtifact *api.OCIArtifactReference
	OCIError    error
}

// ResultsExporter exports evaluation cards to configured targets.
type ResultsExporter interface {
	Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (ExportResult, error)
}

// ExportTarget exports an evaluation card to a single target.
type ExportTarget interface {
	Target() Target
	Enabled(job *api.EvaluationJobResource) bool
	Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (ExportResult, error)
}

// ManagerConfig configures shared dependencies for export targets.
type ManagerConfig struct {
	MLFlowClient           *mlflowclient.Client
	MLFlowWorkspaceSupport *evalhubmlflow.WorkspaceSupport
	OCIPublisherFactory    OCIPublisherFactory
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
		NewMLflowTarget(cfg.MLFlowClient, cfg.MLFlowWorkspaceSupport, logger),
		NewOCITarget(ociFactory, logger),
	}
	return &Manager{logger: logger, targets: targets}
}

// Export writes the evaluation card to all targets enabled by the job configuration.
// It retains the OCI artifact or error independently of the first successful card URL.
// Errors from individual targets are joined; callers may log and ignore them.
func (m *Manager) Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (ExportResult, error) {
	if job == nil || card == nil {
		return ExportResult{}, nil
	}
	logger := m.logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	var result ExportResult
	var errs []error
	for _, target := range m.targets {
		if !target.Enabled(job) {
			continue
		}
		outcome, err := target.Export(ctx, job, card)
		if err != nil {
			if target.Target() == TargetOCI {
				result.OCIError = err
			}
			logger.Error(
				"Failed to export evaluation results",
				"target", target.Target(),
				"job_id", job.Resource.ID,
				"error", err,
			)
			errs = append(errs, fmt.Errorf("%s: %w", target.Target(), err))
			continue
		}
		if outcome.CardURL != "" && result.CardURL == "" {
			result.CardURL = outcome.CardURL
		}
		if target.Target() == TargetOCI {
			result.OCIArtifact = outcome.OCIArtifact
		}
		logger.Info(
			"Exported evaluation results",
			"target", target.Target(),
			"job_id", job.Resource.ID,
		)
	}
	return result, errors.Join(errs...)
}

type mlflowTarget struct {
	client           *mlflowclient.Client
	workspaceSupport *evalhubmlflow.WorkspaceSupport
	logger           *slog.Logger
}

func NewMLflowTarget(client *mlflowclient.Client, workspaceSupport *evalhubmlflow.WorkspaceSupport, logger *slog.Logger) ExportTarget {
	return &mlflowTarget{client: client, workspaceSupport: workspaceSupport, logger: logger}
}

func (t *mlflowTarget) Target() Target {
	return TargetMLflow
}

func (t *mlflowTarget) Enabled(job *api.EvaluationJobResource) bool {
	return evalhubmlflow.HasExperimentName(&job.EvaluationJobConfig) && job.Resource.MLFlowExperimentID != ""
}

func (t *mlflowTarget) Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (ExportResult, error) {
	if t.client == nil {
		return ExportResult{}, fmt.Errorf("mlflow client is not configured")
	}

	cardJSON, err := json.Marshal(card)
	if err != nil {
		return ExportResult{}, fmt.Errorf("marshal evaluation card: %w", err)
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

	artifactURL, err := evalhubmlflow.PersistEvalCard(
		client,
		t.workspaceSupport,
		job.Resource.MLFlowExperimentID,
		job.Resource.ID,
		job.Name,
		artifactLocation,
		cardJSON,
	)
	if err != nil {
		return ExportResult{}, err
	}
	if t.logger != nil {
		t.logger.Info(
			"Uploaded evaluation card artifact to MLflow",
			"job_id", job.Resource.ID,
			"experiment_id", job.Resource.MLFlowExperimentID,
			"artifact_url", artifactURL,
		)
	}
	return ExportResult{CardURL: artifactURL}, nil
}

type ociTarget struct {
	factory OCIPublisherFactory
}

func NewOCITarget(factory OCIPublisherFactory, _ *slog.Logger) ExportTarget {
	if factory == nil {
		factory = NewNoopOCIPublisherFactory()
	}
	return &ociTarget{factory: factory}
}

func (t *ociTarget) Target() Target {
	return TargetOCI
}

func (t *ociTarget) Enabled(job *api.EvaluationJobResource) bool {
	return job.Exports != nil && job.Exports.OCI != nil
}

func (t *ociTarget) Export(ctx context.Context, job *api.EvaluationJobResource, card *cards.EvaluationCard) (ExportResult, error) {
	if card == nil || card.Metadata.EvaluationJobID == "" {
		return ExportResult{}, &OCIExportError{Code: "OCI_CARD_GENERATION_FAILED", Operation: "generate evaluation card", Cause: "card is missing or has no evaluation job identity"}
	}
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return ExportResult{}, &OCIExportError{Code: "OCI_CARD_SERIALIZATION_FAILED", Operation: "serialize evaluation card", Cause: safeOCICause(err)}
	}
	publisher, err := t.factory.NewPublisher(ctx, job)
	if err != nil {
		return ExportResult{}, &OCIExportError{Code: "OCI_CARD_ACCESS_FAILED", Operation: "initialize OCI publisher and resolve registry credentials", Cause: safeOCICause(err)}
	}
	if publisher == nil {
		return ExportResult{}, &OCIExportError{Code: "OCI_CARD_ACCESS_FAILED", Operation: "initialize OCI publisher", Cause: "publisher is unavailable"}
	}
	defer func() { _ = publisher.Close() }()
	artifact, err := publisher.PublishEvalCard(ctx, cardJSON)
	if err != nil {
		return ExportResult{}, &OCIExportError{Code: "OCI_CARD_PUBLICATION_FAILED", Operation: "publish evaluation card to OCI", Cause: safeOCICause(err)}
	}
	if artifact == nil || artifact.OCIDigest == "" || artifact.OCIReference == "" {
		return ExportResult{}, &OCIExportError{Code: "OCI_CARD_PUBLICATION_FAILED", Operation: "publish evaluation card to OCI", Cause: "publisher returned no usable manifest reference"}
	}
	return ExportResult{OCIArtifact: artifact}, nil
}
