package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type selectedAdapterProvider struct {
	adapterID  string
	providerID string
}

var selectedAdapterProviders = []selectedAdapterProvider{
	{adapterID: "inspect", providerID: "inspect"},
	{adapterID: "lm-evaluation-harness", providerID: "lm_evaluation_harness"},
	{adapterID: "lighteval", providerID: "lighteval"},
	{adapterID: "ruler", providerID: "ruler"},
	{adapterID: "ifbench", providerID: "ifbench"},
	{adapterID: "followbench", providerID: "followbench"},
}

// TestDevDeploySelectedAdapterProviderImages verifies that every adapter
// selected by dev-deploy is visible through the EvalHub API and wired to the
// exact image recorded by dev-deploy. It does not submit benchmark jobs.
func TestDevDeploySelectedAdapterProviderImages(t *testing.T) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	rawConfig, err := loadingRules.Load()
	if err != nil {
		t.Skipf("no usable kubeconfig is available for automatic e2e discovery: %v", err)
	}
	contextName := rawConfig.CurrentContext
	if contextName == "" {
		t.Skip("kubeconfig has no current context; select the target cluster with kubectl config use-context")
	}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		t.Fatalf("load Kubernetes client configuration for context %q: %v", contextName, err)
	}
	kube, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		t.Fatalf("create Kubernetes client for context %q: %v", contextName, err)
	}

	ctx := t.Context()
	managedNamespaces, err := discoverManagedDeploymentNamespaces(ctx, kube)
	if err != nil {
		t.Fatalf("discover dev-deploy manifests: %v", err)
	}
	if len(managedNamespaces) == 0 {
		t.Skip("cluster has no dev-deploy manifest; adapter image wiring is only verified for make dev-deploy clusters")
	}
	instances, err := discoverEvalHubInstances(ctx, kube)
	if err != nil {
		t.Fatalf("discover EvalHub API services in context %q: %v", contextName, err)
	}
	instancesByNamespace := make(map[string][]evalHubInstance)
	for _, instance := range instances {
		instancesByNamespace[instance.namespace] = append(instancesByNamespace[instance.namespace], instance)
	}

	for _, namespace := range managedNamespaces {
		namespace := namespace
		namespaceInstances := instancesByNamespace[namespace]
		if len(namespaceInstances) == 0 {
			t.Fatalf("dev-deploy manifest exists in namespace %q, but no ready EvalHub API instance was discovered", namespace)
		}
		manifest, err := discoverDeploymentManifest(ctx, kube, namespace)
		if err != nil {
			t.Fatalf("read dev-deploy manifest in namespace %q: %v", namespace, err)
		}
		for _, instance := range namespaceInstances {
			instance := instance
			t.Run(namespace+"/"+instance.serviceName, func(t *testing.T) {
				connection, cleanup := newDeploymentTestAPIConnection(t, ctx, contextName, kube, instance)
				defer cleanup()
				providers := listDeploymentTestProviders(t, ctx, connection.client)
				for _, selected := range selectedAdapterProviders {
					if !manifest.adapterSelected(selected.adapterID) {
						continue
					}
					selected := selected
					t.Run(selected.adapterID, func(t *testing.T) {
						provider := findDeploymentTestProvider(providers, selected.providerID)
						if provider == nil {
							t.Fatalf("dev-deploy selected %q, but provider %q is absent from the EvalHub API", selected.adapterID, selected.providerID)
						}
						if provider.Runtime == nil || provider.Runtime.K8s == nil || provider.Runtime.K8s.Image == "" {
							t.Fatalf("selected provider %q has no Kubernetes image configured: %+v", selected.providerID, provider.Runtime)
						}
						if len(provider.Benchmarks) == 0 {
							t.Fatalf("selected provider %q exposes no benchmarks", selected.providerID)
						}
						if err := manifest.verifyProviderImage(selected.adapterID, provider.Runtime.K8s.Image); err != nil {
							t.Fatalf("provider image does not match dev-deploy manifest: %v", err)
						}
						logDeployedAdapter(t, manifest, selected.adapterID)
						t.Logf("provider %s exposes %d benchmarks with image %s", provider.Resource.ID, len(provider.Benchmarks), provider.Runtime.K8s.Image)
					})
				}
			})
		}
	}
}

func newDeploymentTestAPIConnection(
	t *testing.T,
	ctx context.Context,
	contextName string,
	kube kubernetes.Interface,
	instance evalHubInstance,
) (*apiConnection, func()) {
	t.Helper()
	forwardCtx, cancelForward := context.WithCancel(ctx)
	baseURL, stopForward, err := startPortForward(forwardCtx, contextName, instance)
	if err != nil {
		cancelForward()
		t.Fatalf("start temporary API port-forward for %s/%s: %v", instance.namespace, instance.serviceName, err)
	}
	cleanup := func() {
		stopForward()
		cancelForward()
	}
	httpClient, err := newEvalHubHTTPClient(ctx, kube, instance)
	if err != nil {
		cleanup()
		t.Fatalf("configure verified TLS for %s/%s: %v", instance.namespace, instance.serviceName, err)
	}
	connection := &apiConnection{baseURL: baseURL, tenant: instance.namespace, httpClient: httpClient}
	connectCtx, cancelConnect := context.WithTimeout(ctx, time.Minute)
	defer cancelConnect()
	if err := waitForAPI(connectCtx, connection, instance); err != nil {
		cleanup()
		t.Fatalf("EvalHub API %s/%s did not become healthy through the temporary port-forward: %v", instance.namespace, instance.serviceName, err)
	}
	token, err := mintServiceAccountToken(ctx, kube, instance.namespace, instance.serviceAccount)
	if err != nil {
		cleanup()
		t.Fatalf("mint a short-lived token for EvalHub service account %s/%s: %v", instance.namespace, instance.serviceAccount, err)
	}
	connection.token = token
	connection.client = evalhubclient.NewClient(baseURL).
		WithToken(token).
		WithTenant(instance.namespace).
		WithHTTPClient(httpClient).
		WithTimeout(5 * time.Second).
		WithMaxRetries(0).
		WithContext(ctx)
	return connection, cleanup
}

func listDeploymentTestProviders(t *testing.T, ctx context.Context, client *evalhubclient.Client) []api.ProviderResource {
	t.Helper()
	providers := make([]api.ProviderResource, 0)
	for offset := 0; ; {
		page, err := client.ListProviders(evalhubclient.WithLimit(100), evalhubclient.WithOffset(offset))
		if err != nil {
			t.Fatalf("list EvalHub providers: %v", err)
		}
		providers = append(providers, page.Items...)
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.TotalCount {
			return providers
		}
		select {
		case <-ctx.Done():
			t.Fatalf("list EvalHub providers: %v", ctx.Err())
		default:
		}
	}
}

func findDeploymentTestProvider(providers []api.ProviderResource, providerID string) *api.ProviderResource {
	for i := range providers {
		if providers[i].Resource.ID == providerID {
			return &providers[i]
		}
	}
	return nil
}
