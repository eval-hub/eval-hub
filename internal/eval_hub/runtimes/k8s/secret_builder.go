package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	modelAPIKeySuffix = "_api-key"
	modelURLSuffix    = "_url"
	modelHFTokenKey   = "hf-token"
	modelCACertKey    = "ca_cert"
	modelSingleAPIKey = "api-key"
)

// directAdapterSecretKeys are keys from the model credential secret projected directly into
// the adapter volume rather than replaced with a ref token. These credentials cannot be
// proxied by the sidecar (HF Hub calls bypass the proxy; ca_cert is a TLS artifact, not an
// inference credential), so the adapter receives the real values via a selective real-secret
// projection alongside the internalModelRef secret.
var directAdapterSecretKeys = []string{modelHFTokenKey, modelCACertKey}

// isModelCredentialKey reports whether k is a proxy-injectable credential key
// (api-key, *_api-key, or *_url).
func isModelCredentialKey(k string) bool {
	return k == modelSingleAPIKey || strings.HasSuffix(k, modelAPIKeySuffix) || strings.HasSuffix(k, modelURLSuffix)
}

// modelSecretInfo holds the result of inspecting the model credential secret.
type modelSecretInfo struct {
	// hasCredentialKeys is true when the secret contains at least one proxy-injectable
	// key (api-key, *_api-key, *_url), meaning credential injection should be activated.
	hasCredentialKeys bool
}

// inspectModelSecret reads the model credential secret and reports whether it contains
// proxy-injectable credential keys. Breaks early on first match. When hasCredentialKeys=false
// (e.g. ca_cert-only secret), no internalModelRef secret is created and no model proxy
// is started.
func inspectModelSecret(ctx context.Context, namespace, secretName string, helper *KubernetesHelper) (modelSecretInfo, error) {
	realSecret, err := helper.GetSecret(ctx, namespace, secretName)
	if err != nil {
		return modelSecretInfo{}, fmt.Errorf("get model credential secret %q: %w", secretName, err)
	}
	for k := range realSecret.Data {
		if isModelCredentialKey(k) {
			return modelSecretInfo{hasCredentialKeys: true}, nil
		}
	}
	return modelSecretInfo{}, nil
}

// buildInternalModelRefSecret creates the ephemeral internalModelRef secret in namespace
// by reading the model credential secret and generating synthetic ref/placeholder values.
// Only called when hasCredentialKeys=true (verified by inspectModelSecret).
//
// Key filtering rules applied to model credential secret keys:
//
//   - "api-key"          → value becomes "api-key:ref" (sidecar injects real key)
//   - "*_api-key" suffix → value becomes "<key>:ref"   (sidecar injects real key)
//   - "*_url" suffix     → value becomes sidecarProxyURL (adapter routes through sidecar)
//   - "hf-token"         → omitted; projected directly from the model credential secret
//   - "ca_cert"          → omitted; projected directly from the model credential secret
//   - all other keys     → omitted (conservative; avoids leaking unknown fields)
//
// The internalModelRef secret contains only synthetic ref/placeholder values — no real credentials.
func buildInternalModelRefSecret(
	ctx context.Context,
	namespace string,
	refSecretName string,
	realSecretName string,
	sidecarProxyURL string,
	labels map[string]string,
	helper *KubernetesHelper,
) (*corev1.Secret, error) {
	realSecret, err := helper.GetSecret(ctx, namespace, realSecretName)
	if err != nil {
		return nil, fmt.Errorf("get model credential secret %q: %w", realSecretName, err)
	}
	data := realSecret.Data
	if len(data) == 0 {
		return nil, fmt.Errorf("model credential secret %q has no data keys", realSecretName)
	}

	refData := make(map[string][]byte, len(data))
	for k := range data {
		switch {
		case isModelCredentialKey(k):
			if strings.HasSuffix(k, modelURLSuffix) {
				refData[k] = []byte(sidecarProxyURL)
			} else {
				refData[k] = []byte(k + modelRefValueSuffix)
			}
		case k == modelHFTokenKey || k == modelCACertKey:
			// projected directly from the model credential secret into the adapter volume
		default:
			// unknown key — omitted from the internalModelRef secret (conservative; avoids leaking unknown fields)
		}
	}

	if len(refData) == 0 {
		return nil, fmt.Errorf("model credential secret %q contains no recognised credential keys (expected %q or keys with %q or %q suffix)",
			realSecretName, modelSingleAPIKey, modelAPIKeySuffix, modelURLSuffix)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      refSecretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: refData,
	}
	return helper.CreateSecret(ctx, namespace, secret)
}

// modelRefValueSuffix is shared between secret_builder.go and model_proxy.go.
// Defined here so both sides stay in sync without a cross-package import cycle.
const modelRefValueSuffix = ":ref"
