package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/k8s"
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

// newOCIPublisherFactory wires the real OCI exporter in cluster mode. Local mode and startup
// failures fall back to the noop factory so the API service can still start without registry access.
func newOCIPublisherFactory(logger *slog.Logger, serviceConfig *config.Config) evalcards.OCIPublisherFactory {
	if serviceConfig == nil || serviceConfig.Service == nil || serviceConfig.Service.LocalMode {
		return evalcards.NewNoopOCIPublisherFactory()
	}
	helper, err := k8s.NewKubernetesHelper()
	if err != nil {
		if logger != nil {
			logger.Warn("OCI export disabled: kubernetes client unavailable", "error", err)
		}
		return evalcards.NewNoopOCIPublisherFactory()
	}
	httpClient, err := evalcards.NewOCIHTTPClient(serviceConfig, serviceConfig.IsOTELEnabled(), logger)
	if err != nil {
		if logger != nil {
			logger.Warn("OCI export disabled: failed to create oci http client", "error", err)
		}
		return evalcards.NewNoopOCIPublisherFactory()
	}
	return evalcards.NewOCIPublisherFactory(
		newKubernetesDockerConfigSecretGetter(helper),
		httpClient,
	)
}
