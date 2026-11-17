package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	authnv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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
							return
						}
						if provider.Runtime == nil || provider.Runtime.K8s == nil || provider.Runtime.K8s.Image == "" {
							t.Fatalf("selected provider %q has no Kubernetes image configured: %+v", selected.providerID, provider.Runtime)
							return
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
	if err := refreshDeploymentTestAPIClient(ctx, kube, instance, connection); err != nil {
		cleanup()
		t.Fatalf("configure EvalHub API authentication for service account %s/%s: %v", instance.namespace, instance.serviceAccount, err)
	}
	return connection, cleanup
}

// Refresh the short-lived token for each benchmark job. The curated matrix can
// run for several hours, while Kubernetes may cap TokenRequest lifetimes at one
// hour. Rebuilding the client also ensures each request uses the fresh token.
func refreshDeploymentTestAPIClient(
	ctx context.Context,
	kube kubernetes.Interface,
	instance evalHubInstance,
	connection *apiConnection,
) error {
	token, err := mintServiceAccountToken(ctx, kube, instance.namespace, instance.serviceAccount)
	if err != nil {
		return fmt.Errorf("mint service-account token: %w", err)
	}
	connection.token = token
	connection.client = evalhubclient.NewClient(connection.baseURL).
		WithToken(token).
		WithTenant(connection.tenant).
		WithHTTPClient(connection.httpClient).
		WithTimeout(5 * time.Second).
		WithContext(ctx)
	return nil
}

func TestRefreshDeploymentTestAPIClientRenewsToken(t *testing.T) {
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	tokens := []string{"first-token", "renewed-token"}
	issued := 0
	kube := fake.NewSimpleClientset()
	kube.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "token" {
			return false, nil, nil
		}
		if issued >= len(tokens) {
			t.Fatalf("unexpected token request %d", issued+1)
		}
		token := tokens[issued]
		issued++
		return true, &authnv1.TokenRequest{Status: authnv1.TokenRequestStatus{Token: token}}, nil
	})

	ctx := context.Background()
	instance := evalHubInstance{namespace: "system", serviceAccount: "evalhub-local-service"}
	connection := &apiConnection{baseURL: server.URL, tenant: instance.namespace, httpClient: server.Client()}
	for _, wantToken := range tokens {
		if err := refreshDeploymentTestAPIClient(ctx, kube, instance, connection); err != nil {
			t.Fatalf("refresh API client: %v", err)
		}
		if _, err := connection.client.GetHealth(); err != nil {
			t.Fatalf("health request with refreshed token: %v", err)
		}
		if got, want := <-authorization, "Bearer "+wantToken; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
	}
	if issued != len(tokens) {
		t.Fatalf("token requests = %d, want %d", issued, len(tokens))
	}
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
