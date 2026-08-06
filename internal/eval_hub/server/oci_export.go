package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/k8s"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

type kubernetesDockerConfigSecretGetter struct {
	helper *k8s.KubernetesHelper
}

// newKubernetesDockerConfigSecretGetter adapts KubernetesHelper to the evalcards secret getter interface.
func newKubernetesDockerConfigSecretGetter(helper *k8s.KubernetesHelper) evalcards.DockerConfigSecretGetter {
	return &kubernetesDockerConfigSecretGetter{helper: helper}
}

// GetDockerConfigJSON fetches a tenant-namespace secret via the Kubernetes API and returns its
// .dockerconfigjson payload. Eval-hub reads credentials on the fly instead of mounting tenant
// secrets on the service pod.
func (g *kubernetesDockerConfigSecretGetter) GetDockerConfigJSON(ctx context.Context, namespace, secretName string) ([]byte, error) {
	if g == nil || g.helper == nil {
		return nil, fmt.Errorf("kubernetes secret getter is not configured")
	}
	secret, err := g.helper.GetSecret(ctx, namespace, secretName)
	if err != nil {
		return nil, fmt.Errorf("get secret %q in namespace %q: %w", secretName, namespace, err)
	}
	return ociclient.DockerConfigJSONFromSecret(secret.Data)
}

// newOCIPublisherFactory wires the real OCI exporter in cluster mode. Local mode uses a noop
// factory; cluster initialization failures return an error-aware factory so OCI export requests
// fail explicitly instead of being silently discarded.
// The returned cleanup func must be called when the factory is no longer needed to stop the
// Kubernetes EventBroadcaster goroutines started by NewKubernetesHelper.
func newOCIPublisherFactory(logger *slog.Logger, serviceConfig *config.Config) (evalcards.OCIPublisherFactory, func()) {
	noop := func() {}
	if serviceConfig == nil || serviceConfig.Service == nil || serviceConfig.Service.LocalMode {
		return evalcards.NewNoopOCIPublisherFactory(), noop
	}
	helper, err := k8s.NewKubernetesHelper()
	if err != nil {
		if logger != nil {
			logger.Warn("OCI export unavailable: kubernetes client initialization failed", "error", err)
		}
		return newUnavailableOCIPublisherFactory(fmt.Errorf("oci export unavailable: kubernetes client: %w", err)), noop
	}
	httpClient, err := evalcards.NewOCIHTTPClient(serviceConfig, serviceConfig.IsOTELEnabled(), logger)
	if err != nil {
		_ = helper.Close()
		if logger != nil {
			logger.Warn("OCI export unavailable: failed to create oci http client", "error", err)
		}
		return newUnavailableOCIPublisherFactory(fmt.Errorf("oci export unavailable: http client: %w", err)), noop
	}
	return evalcards.NewOCIPublisherFactory(
		newKubernetesDockerConfigSecretGetter(helper),
		httpClient,
	), func() { _ = helper.Close() }
}

type unavailableOCIPublisherFactory struct {
	err error
}

func newUnavailableOCIPublisherFactory(err error) evalcards.OCIPublisherFactory {
	return &unavailableOCIPublisherFactory{err: err}
}

func (f *unavailableOCIPublisherFactory) NewPublisher(_ context.Context, _ *api.EvaluationJobResource) (evalcards.OCIPublisher, error) {
	return nil, f.err
}
