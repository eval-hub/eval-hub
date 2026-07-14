package evalcards

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultOCIHTTPTimeout = 30 * time.Second
	defaultOCICACertPath  = "/etc/pki/ca-trust/source/anchors/service-ca.crt"
)

// DockerConfigSecretGetter reads kubernetes.io/dockerconfigjson secret payloads.
// The interface keeps evalcards independent of Kubernetes client details while still allowing
// on-demand secret lookup from the tenant namespace at export time.
type DockerConfigSecretGetter interface {
	GetDockerConfigJSON(ctx context.Context, namespace, secretName string) ([]byte, error)
}

// NewOCIHTTPClient creates an HTTP client for OCI registry export using optional sidecar.oci TLS
// settings. Cluster registries often use private CAs or self-signed certs; this mirrors the
// sidecar OCI proxy TLS configuration for eval-hub's outbound export calls.
func NewOCIHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	timeout := defaultOCIHTTPTimeout
	caCertPath := ""
	insecureSkipVerify := false
	if serviceConfig != nil && serviceConfig.Sidecar != nil && serviceConfig.Sidecar.OCI != nil {
		oci := serviceConfig.Sidecar.OCI
		if oci.HTTPTimeout > 0 {
			timeout = oci.HTTPTimeout
		}
		caCertPath = oci.CACertPath
		insecureSkipVerify = oci.InsecureSkipVerify
	}
	tlsConfig, err := buildOCIHTTPClientTLS(caCertPath, insecureSkipVerify, logger)
	if err != nil {
		return nil, err
	}
	transport := http.RoundTripper(&http.Transport{})
	if tlsConfig != nil {
		transport = &http.Transport{TLSClientConfig: tlsConfig}
	}
	if isOTELEnabled {
		transport = otelhttp.NewTransport(transport)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

// buildOCIHTTPClientTLS loads registry TLS settings for the export HTTP client. It falls back to
// the cluster service CA bundle and, when absent, to system roots so export works in dev and prod.
func buildOCIHTTPClientTLS(caCertPath string, insecureSkipVerify bool, logger *slog.Logger) (*tls.Config, error) {
	if caCertPath == "" && !insecureSkipVerify {
		caCertPath = defaultOCICACertPath
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
		return tlsConfig, nil
	}
	if caCertPath == "" {
		return nil, nil
	}
	caPEM, err := os.ReadFile(caCertPath) // #nosec G304 -- CA path from service configuration
	if err != nil {
		if os.IsNotExist(err) {
			if logger != nil {
				logger.Info("OCI CA cert absent, using system roots", "path", caCertPath)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("read oci ca cert %q: %w", caCertPath, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse oci ca cert %q", caCertPath)
	}
	tlsConfig.RootCAs = pool
	return tlsConfig, nil
}

type ociPublisherFactory struct {
	secretGetter DockerConfigSecretGetter
	httpClient   *http.Client
}

// NewOCIPublisherFactory creates per-job OCI publishers that resolve credentials from tenant secrets.
// Each evaluation job may target a different registry/repository and secret, so publishers are
// constructed per export rather than shared globally.
func NewOCIPublisherFactory(secretGetter DockerConfigSecretGetter, httpClient *http.Client) OCIPublisherFactory {
	return &ociPublisherFactory{
		secretGetter: secretGetter,
		httpClient:   httpClient,
	}
}

// NewPublisher builds a per-job OCI publisher from export coordinates and tenant-scoped credentials.
// It fetches the dockerconfigjson secret on demand, parses registry auth, and prepares a client
// tagged for this evaluation job.
func (f *ociPublisherFactory) NewPublisher(ctx context.Context, job *api.EvaluationJobResource) (OCIPublisher, error) {
	if job == nil || job.Exports == nil || job.Exports.OCI == nil {
		return nil, fmt.Errorf("oci export configuration is required")
	}
	if f == nil || f.secretGetter == nil {
		return nil, fmt.Errorf("oci secret getter is not configured")
	}
	if f.httpClient == nil {
		return nil, fmt.Errorf("oci http client is not configured")
	}

	oci := job.Exports.OCI
	if oci.K8s == nil || oci.K8s.Connection == "" {
		return nil, fmt.Errorf("oci k8s connection secret is required")
	}
	namespace := job.Resource.Tenant.String()
	if namespace == "" {
		return nil, fmt.Errorf("tenant namespace is required for oci secret lookup")
	}

	secretData, err := f.secretGetter.GetDockerConfigJSON(ctx, namespace, oci.K8s.Connection)
	if err != nil {
		return nil, err
	}
	creds, err := ociclient.ParseDockerConfigJSON(secretData, oci.Coordinates.OCIHost)
	if err != nil {
		return nil, fmt.Errorf("parse oci credentials: %w", err)
	}
	client, err := ociclient.NewClient(oci.Coordinates.OCIHost, oci.Coordinates.OCIRepository, creds, f.httpClient)
	if err != nil {
		return nil, err
	}
	return &ociPublisher{
		client:      client,
		jobID:       job.Resource.ID,
		ociTag:      oci.Coordinates.OCITag,
		annotations: oci.Coordinates.Annotations,
	}, nil
}

type ociPublisher struct {
	client      *ociclient.Client
	jobID       string
	ociTag      string
	annotations map[string]string
}

// PublishEvalCard pushes the marshaled evaluation card JSON to the configured OCI registry tag.
func (p *ociPublisher) PublishEvalCard(ctx context.Context, cardJSON []byte) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("oci publisher is not configured")
	}
	return p.client.PushEvaluationCard(ctx, p.jobID, cardJSON, p.ociTag, p.annotations)
}

// Close releases per-job publisher resources. The current implementation has no persistent state.
func (p *ociPublisher) Close() error {
	return nil
}
