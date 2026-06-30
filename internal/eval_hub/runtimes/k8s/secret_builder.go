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
	modelTokenSuffix  = "_token"
	modelURLSuffix    = "_url"
	modelHFTokenKey   = "hf-token"
	modelCACertKey    = "ca_cert"
	modelSingleAPIKey = "api-key"
)

// isModelCredentialKey reports whether k is a proxy-injectable credential key
// (api-key, *_api-key, *_token, or *_url).
func isModelCredentialKey(k string) bool {
	return k == modelSingleAPIKey ||
		strings.HasSuffix(k, modelAPIKeySuffix) ||
		strings.HasSuffix(k, modelTokenSuffix) ||
		strings.HasSuffix(k, modelURLSuffix)
}

// modelSecretInfo holds the result of inspecting the model credential secret.
type modelSecretInfo struct {
	// hasCredentialKeys is true when the secret contains at least one proxy-injectable
	// key (api-key, *_api-key, *_url), meaning credential injection should be activated.
	hasCredentialKeys bool
	// data is the full secret data, populated when hasCredentialKeys is true so callers
	// can build the internalModelRef secret without a second API call.
	data map[string][]byte
}

// inspectModelSecret reads the model credential secret and reports whether it contains
// proxy-injectable credential keys. When hasCredentialKeys=true the full data map is
// returned so the caller can pass it directly to buildInternalModelRefSecret without a
// second GetSecret call. When hasCredentialKeys=false (e.g. ca_cert-only secret), no
// internalModelRef secret is created and no model proxy is started.
func inspectModelSecret(ctx context.Context, namespace, secretName string, helper *KubernetesHelper) (modelSecretInfo, error) {
	realSecret, err := helper.GetSecret(ctx, namespace, secretName)
	if err != nil {
		return modelSecretInfo{}, fmt.Errorf("get model credential secret %q: %w", secretName, err)
	}
	for k := range realSecret.Data {
		if isModelCredentialKey(k) {
			return modelSecretInfo{hasCredentialKeys: true, data: realSecret.Data}, nil
		}
	}
	return modelSecretInfo{}, nil
}

// buildInternalModelRefSecret creates the ephemeral internalModelRef secret in namespace
// from the already-read model credential secret data. Only called when hasCredentialKeys=true
// (verified by inspectModelSecret, which also provides the data to avoid a second API call).
//
// Key filtering rules applied to model credential secret keys:
//
//   - "api-key"          → value becomes "api-key:ref" (sidecar injects real key)
//   - "*_api-key" suffix → value becomes "<key>:ref"   (sidecar injects real key)
//   - "*_token" suffix   → value becomes "<key>:ref"   (sidecar injects token or SA token when value empty)
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
	data map[string][]byte,
	sidecarProxyURL string,
	labels map[string]string,
	helper *KubernetesHelper,
) (*corev1.Secret, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("model credential secret data is empty")
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
		return nil, fmt.Errorf("model credential secret data contains no recognised credential keys (expected %q or keys with %q, %q, or %q suffix)",
			modelSingleAPIKey, modelAPIKeySuffix, modelTokenSuffix, modelURLSuffix)
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
