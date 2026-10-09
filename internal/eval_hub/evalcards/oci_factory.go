package evalcards

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/internal/safefile"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultOCIHTTPTimeout = 30 * time.Second
	defaultOCICACertPath  = "/etc/pki/ca-trust/source/anchors/service-ca.crt"
)

// NewOCIHTTPClient creates an HTTP client for OCI registry export using optional sidecar.oci TLS
// settings. Cluster registries often use private CAs; this mirrors the sidecar OCI proxy TLS
// configuration for eval-hub's outbound export calls. TLS verification is always enabled.
func NewOCIHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	timeout := defaultOCIHTTPTimeout
	caCertPath := ""
	if serviceConfig != nil && serviceConfig.Sidecar != nil && serviceConfig.Sidecar.OCI != nil {
		oci := serviceConfig.Sidecar.OCI
		if oci.HTTPTimeout > 0 {
			timeout = oci.HTTPTimeout
		}
		caCertPath = oci.CACertPath
	}
	tlsConfig, err := buildOCIHTTPClientTLS(caCertPath, logger)
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
func buildOCIHTTPClientTLS(caCertPath string, logger *slog.Logger) (*tls.Config, error) {
	if caCertPath == "" {
		caCertPath = defaultOCICACertPath
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	caPEM, err := safefile.ReadFile(filepath.Dir(caCertPath), filepath.Base(caCertPath))
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
	credentialResolver oci.OCICredentialResolver
	httpClient         *http.Client
}

// NewOCIPublisherFactory creates per-job OCI publishers using the supplied credential resolver.
// Each evaluation job may target a different registry/repository and connection, so publishers are
// constructed per export rather than shared globally.
func NewOCIPublisherFactory(resolver oci.OCICredentialResolver, httpClient *http.Client) OCIPublisherFactory {
	return &ociPublisherFactory{
		credentialResolver: resolver,
		httpClient:         httpClient,
	}
}

// NewPublisher resolves credentials and builds a client tagged for this evaluation job.
func (f *ociPublisherFactory) NewPublisher(ctx context.Context, job *api.EvaluationJobResource) (OCIPublisher, error) {
	if job == nil || job.Exports == nil || job.Exports.OCI == nil {
		return nil, fmt.Errorf("oci export configuration is required")
	}
	if f == nil || f.credentialResolver == nil {
		return nil, fmt.Errorf("oci credential resolver is not configured")
	}
	if f.httpClient == nil {
		return nil, fmt.Errorf("oci http client is not configured")
	}

	export := job.Exports.OCI
	creds, err := f.credentialResolver.Resolve(ctx, oci.OCICredentialRequest{
		RegistryHost: export.Coordinates.OCIHost,
		Tenant:       job.Resource.Tenant.String(),
		Connection:   export.K8s,
	})
	if err != nil {
		return nil, err
	}
	client, err := ociclient.NewClient(export.Coordinates.OCIHost, export.Coordinates.OCIRepository, creds, f.httpClient)
	if err != nil {
		return nil, err
	}
	return &ociPublisher{
		client:      client,
		jobID:       job.Resource.ID,
		ociTag:      export.Coordinates.OCITag,
		annotations: export.Coordinates.Annotations,
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
