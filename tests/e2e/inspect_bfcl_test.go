package e2e

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

const (
	evalHubAPIPodSelector = "app=eval-hub,component=api"
	inspectProviderID     = "inspect"
	bfclBenchmarkID       = "inspect/bfcl"
	bfclAssetLabel        = "evalhub.io/e2e-inspect-bfcl-assets"
	bfclSubPathAnnotation = "evalhub.io/e2e-inspect-bfcl-sub-path"
	bfclSourceRepository  = "https://github.com/ShishirPatil/gorilla.git"
	bfclSourceCommit      = "dac44e7ac9db5ff26a01ab0c1ec5de5a1e703b7a"
	bfclSourceDataPath    = "berkeley-function-call-leaderboard/bfcl_eval/data"
	bfclSourceCanary      = "BFCL_v4_simple_python.json"
	bfclStagerPodPrefix   = "evalhub-e2e-bfcl-stager-"
	bfclClaimPrefix       = "evalhub-e2e-bfcl-assets-"
	mockTargetModelName   = "kind-target-model"
	mockTargetAPIKey      = "target-test-token"
	jobTimeout            = 20 * time.Minute
)

type evalHubInstance struct {
	namespace      string
	serviceName    string
	servicePort    int32
	serverName     string
	serviceAccount string
}

type bfclAsset struct {
	claimName string
	subPath   string
	source    string
	ref       *api.TestDataRef
}

type apiConnection struct {
	client     *evalhubclient.Client
	baseURL    string
	token      string
	tenant     string
	httpClient *http.Client
}

func TestInspectBFCLAirgap(t *testing.T) {
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
	instances, err := discoverEvalHubInstances(ctx, kube)
	if err != nil {
		t.Fatalf("discover EvalHub API services in context %q: %v", contextName, err)
	}
	if len(instances) == 0 {
		t.Skipf("context %q has no ready EvalHub API pod matching %q; no e2e case applies", contextName, evalHubAPIPodSelector)
	}

	for _, instance := range instances {
		instance := instance
		t.Run(instance.namespace+"/"+instance.serviceName, func(t *testing.T) {
			runInspectBFCLAirgapCase(t, ctx, contextName, kube, instance)
		})
	}
}

func runInspectBFCLAirgapCase(
	t *testing.T,
	parentCtx context.Context,
	contextName string,
	kube kubernetes.Interface,
	instance evalHubInstance,
) {
	t.Helper()

	forwardCtx, cancelForward := context.WithCancel(parentCtx)
	baseURL, stopForward, err := startPortForward(forwardCtx, contextName, instance)
	if err != nil {
		t.Fatalf("start temporary API port-forward for %s/%s: %v", instance.namespace, instance.serviceName, err)
	}
	defer func() {
		stopForward()
		cancelForward()
	}()

	httpClient, err := newEvalHubHTTPClient(parentCtx, kube, instance)
	if err != nil {
		t.Fatalf("configure verified TLS for %s/%s: %v", instance.namespace, instance.serviceName, err)
	}
	connection := &apiConnection{
		baseURL:    baseURL,
		tenant:     instance.namespace,
		httpClient: httpClient,
	}
	connectCtx, cancelConnect := context.WithTimeout(parentCtx, time.Minute)
	defer cancelConnect()
	if err := waitForAPI(connectCtx, connection, instance); err != nil {
		t.Fatalf("EvalHub API %s/%s did not become healthy through the temporary port-forward: %v", instance.namespace, instance.serviceName, err)
	}

	token, err := mintServiceAccountToken(parentCtx, kube, instance.namespace, instance.serviceAccount)
	if err != nil {
		t.Fatalf("mint a short-lived token for EvalHub service account %s/%s: %v", instance.namespace, instance.serviceAccount, err)
	}
	connection.token = token
	connection.client = evalhubclient.NewClient(baseURL).
		WithToken(token).
		WithTenant(instance.namespace).
		WithHTTPClient(httpClient).
		WithTimeout(5 * time.Second).
		WithMaxRetries(0).
		WithContext(parentCtx)

	provider, err := findInspectBFCLProvider(parentCtx, connection.client)
	if err != nil {
		t.Fatalf("read providers from EvalHub API in namespace %q: %v", instance.namespace, err)
	}
	if provider == nil {
		t.Skipf("EvalHub %s/%s has no Kubernetes Inspect provider exposing %q; this test does not apply", instance.namespace, instance.serviceName, bfclBenchmarkID)
	}

	policies, err := kube.NetworkingV1().NetworkPolicies(instance.namespace).List(parentCtx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list NetworkPolicies in namespace %q: %v", instance.namespace, err)
	}
	policyNames, restricted, reason := inspectAirgapPolicies(policies.Items)
	if !restricted {
		t.Skipf("EvalHub %s/%s is not configured for this disconnected test: %s", instance.namespace, instance.serviceName, reason)
	}
	t.Logf("detected restrictive evaluation-job egress policies: %s", strings.Join(policyNames, ", "))

	targetService, err := discoverMockTargetService(parentCtx, kube, instance.namespace)
	if err != nil {
		t.Fatalf("discover mock target service in namespace %q: %v", instance.namespace, err)
	}
	if targetService == nil {
		t.Skipf("no ready mock target Service is available in namespace %q; this e2e case needs the dev-deploy mock target", instance.namespace)
	}
	targetPort, err := servicePort(targetService, "http", 8000)
	if err != nil {
		t.Fatalf("mock target Service %s/%s is not usable: %v", instance.namespace, targetService.Name, err)
	}

	assets, err := discoverBFCLAssets(parentCtx, kube, connection.client, instance.namespace)
	if err != nil {
		t.Fatalf("discover staged BFCL PVC in namespace %q: %v", instance.namespace, err)
	}
	if len(assets) > 1 {
		names := make([]string, 0, len(assets))
		for _, asset := range assets {
			names = append(names, asset.claimName+":"+asset.subPath)
		}
		t.Fatalf("found multiple staged BFCL PVC candidates in namespace %q (%s); configure one asset source to make automatic selection unambiguous", instance.namespace, strings.Join(names, ", "))
	}

	var asset bfclAsset
	if len(assets) == 1 {
		asset = assets[0]
		t.Logf("reusing BFCL assets from PVC %s/%s (source: %s, subPath: %q)", instance.namespace, asset.claimName, asset.source, asset.subPath)
	} else {
		image, imagePullSecrets, podSecurityContext, containerSecurityContext, imageErr := discoverMockTargetImage(parentCtx, kube, instance.namespace)
		if imageErr != nil {
			t.Fatalf("find a local Python image for staging BFCL assets: %v", imageErr)
		}
		stageCtx, cancelStage := context.WithTimeout(parentCtx, 15*time.Minute)
		stagedAsset, cleanup, stageErr := stageBFCLAssets(
			stageCtx, kube, contextName, instance.namespace, image, imagePullSecrets,
			podSecurityContext, containerSecurityContext,
		)
		cancelStage()
		if stageErr != nil {
			t.Fatalf("automatically stage BFCL data for the disconnected test: %v", stageErr)
		}
		asset = stagedAsset
		defer cleanup()
		t.Logf("automatically staged pinned BFCL data into temporary PVC %s/%s", instance.namespace, asset.claimName)
	}
	pvc, err := kube.CoreV1().PersistentVolumeClaims(instance.namespace).Get(parentCtx, asset.claimName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read staged BFCL PVC %s/%s discovered from %s: %v", instance.namespace, asset.claimName, asset.source, err)
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		t.Fatalf("staged BFCL PVC %s/%s is %s, want Bound", instance.namespace, asset.claimName, pvc.Status.Phase)
	}

	secretName, err := randomResourceSuffix()
	if err != nil {
		t.Fatalf("generate a unique name for the temporary mock credential Secret: %v", err)
	}
	secretName = "eh-e2e-target-" + secretName
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: instance.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "evalhub-test-e2e",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"api-key": []byte(mockTargetAPIKey),
		},
	}
	if _, err := kube.CoreV1().Secrets(instance.namespace).Create(parentCtx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create temporary credential Secret %s/%s: %v", instance.namespace, secretName, err)
	}

	jobCtx, cancelJob := context.WithTimeout(parentCtx, jobTimeout)
	defer cancelJob()
	jobClient := connection.client.WithContext(jobCtx)
	jobID := ""
	jobFinished := false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleanupClient := connection.client.WithContext(cleanupCtx)
		if jobID != "" && !jobFinished {
			_ = cleanupClient.CancelJob(jobID)
		}
		_ = kube.CoreV1().Secrets(instance.namespace).Delete(cleanupCtx, secretName, metav1.DeleteOptions{})
	}()

	if asset.ref == nil {
		asset.ref = &api.TestDataRef{
			PVC: &api.PVCTestDataRef{ClaimName: asset.claimName, SubPath: asset.subPath},
		}
	}

	jobName := "e2e-inspect-bfcl-" + secretName[len("eh-e2e-target-"):]
	jobConfig := api.EvaluationJobConfig{
		Name: jobName,
		Model: &api.ModelRef{
			URL:  fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/v1", targetService.Name, instance.namespace, targetPort),
			Name: mockTargetModelName,
			Auth: &api.ModelAuth{SecretRef: secretName},
		},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{
				Ref:        api.Ref{ID: bfclBenchmarkID},
				ProviderID: inspectProviderID,
				Parameters: map[string]any{
					"num_examples": 1,
					"model_args": map[string]any{
						"responses_api": false,
					},
					"task_args": map[string]any{
						"categories": "simple_python",
					},
				},
				TestDataRef: asset.ref,
			},
		},
	}
	created, err := jobClient.CreateJob(jobConfig)
	if err != nil {
		t.Fatalf("submit one-example disconnected BFCL evaluation: %v", err)
	}
	jobID = created.Resource.ID
	if jobID == "" {
		t.Fatal("EvalHub accepted the BFCL job but returned no resource.id")
	}
	t.Logf("submitted disconnected BFCL e2e job %s; the job is retained for inspection", jobID)

	poll := time.NewTicker(3 * time.Second)
	defer poll.Stop()
	for {
		job, err := jobClient.GetJob(jobID)
		if err != nil {
			t.Fatalf("get e2e job %s: %v", jobID, err)
		}
		if job.Status != nil && job.Status.State.IsTerminalState() {
			jobFinished = true
			if job.Status.State != api.OverallStateCompleted {
				logText := getBenchmarkLogs(parentCtx, connection, jobID, 40)
				t.Fatalf("disconnected BFCL job %s finished in state %q; message=%v\nadapter log tail:\n%s",
					jobID, job.Status.State, job.Status.Message, logText)
			}
			if len(job.Status.Benchmarks) != 1 {
				t.Fatalf("disconnected BFCL job %s completed with %d benchmark statuses, want 1: %+v",
					jobID, len(job.Status.Benchmarks), job.Status.Benchmarks)
			}
			status := job.Status.Benchmarks[0]
			if status.ProviderID != inspectProviderID || status.ID != bfclBenchmarkID || status.Status != api.StateCompleted {
				logText := getBenchmarkLogs(parentCtx, connection, jobID, 40)
				t.Fatalf("BFCL benchmark status is not completed: %+v\nadapter log tail:\n%s", status, logText)
			}
			t.Logf("BFCL benchmark completed successfully (score is not asserted; this test verifies offline asset consumption and execution)")
			return
		}

		select {
		case <-jobCtx.Done():
			logText := getBenchmarkLogs(parentCtx, connection, jobID, 40)
			t.Fatalf("disconnected BFCL job %s did not finish within %s: %v\nadapter log tail:\n%s",
				jobID, jobTimeout, jobCtx.Err(), logText)
		case <-poll.C:
		}
	}
}

func discoverEvalHubInstances(ctx context.Context, kube kubernetes.Interface) ([]evalHubInstance, error) {
	pods, err := kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{LabelSelector: evalHubAPIPodSelector})
	if err != nil {
		return nil, err
	}

	instances := make([]evalHubInstance, 0)
	seen := make(map[string]struct{})
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning || !podReady(&pod) {
			continue
		}
		services, err := kube.CoreV1().Services(pod.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list Services in namespace %q: %w", pod.Namespace, err)
		}
		matching := make([]corev1.Service, 0)
		for _, service := range services.Items {
			if len(service.Spec.Selector) == 0 ||
				!labels.Set(service.Spec.Selector).AsSelector().Matches(labels.Set(pod.Labels)) {
				continue
			}
			if _, err := servicePort(&service, "https", 8443); err != nil {
				continue
			}
			matching = append(matching, service)
		}
		sort.Slice(matching, func(i, j int) bool {
			iNodePort := matching[i].Spec.Type == corev1.ServiceTypeNodePort
			jNodePort := matching[j].Spec.Type == corev1.ServiceTypeNodePort
			if iNodePort != jNodePort {
				return !iNodePort
			}
			return matching[i].Name < matching[j].Name
		})
		if len(matching) == 0 {
			continue
		}
		service := matching[0]
		port, _ := servicePort(&service, "https", 8443)
		key := pod.Namespace + "/" + service.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		serviceAccount := pod.Spec.ServiceAccountName
		if serviceAccount == "" {
			serviceAccount = "default"
		}
		instances = append(instances, evalHubInstance{
			namespace:      pod.Namespace,
			serviceName:    service.Name,
			servicePort:    port,
			serverName:     service.Name + "." + pod.Namespace + ".svc",
			serviceAccount: serviceAccount,
		})
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].namespace != instances[j].namespace {
			return instances[i].namespace < instances[j].namespace
		}
		return instances[i].serviceName < instances[j].serviceName
	})
	return instances, nil
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func servicePort(service *corev1.Service, preferredName string, fallbackPort int32) (int32, error) {
	for _, port := range service.Spec.Ports {
		if port.Protocol != corev1.ProtocolTCP {
			continue
		}
		if port.Name == preferredName || port.Port == fallbackPort {
			return port.Port, nil
		}
	}
	return 0, fmt.Errorf("no TCP service port named %q or port %d", preferredName, fallbackPort)
}

func startPortForward(
	ctx context.Context,
	contextName string,
	instance evalHubInstance,
) (string, func(), error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return "", nil, fmt.Errorf("kubectl is required for automatic API access: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("allocate a local port: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cmd := exec.CommandContext(
		ctx,
		"kubectl",
		"--context", contextName,
		"--namespace", instance.namespace,
		"port-forward",
		"--address", "127.0.0.1",
		"service/"+instance.serviceName,
		fmt.Sprintf("%d:%d", localPort, instance.servicePort),
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("start kubectl port-forward: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	stop := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Sprintf("https://127.0.0.1:%d", localPort), stop, nil
}

func newEvalHubHTTPClient(
	ctx context.Context,
	kube kubernetes.Interface,
	instance evalHubInstance,
) (*http.Client, error) {
	configMaps, err := kube.CoreV1().ConfigMaps(instance.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list service CA ConfigMaps: %w", err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	for _, configMap := range configMaps.Items {
		for _, key := range []string{"service-ca.crt", "ca.crt", "ca-bundle.crt"} {
			if pem := configMap.Data[key]; pem != "" {
				roots.AppendCertsFromPEM([]byte(pem))
			}
		}
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
			ServerName: instance.serverName,
		},
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: transport}, nil
}

func waitForAPI(ctx context.Context, connection *apiConnection, instance evalHubInstance) error {
	client := evalhubclient.NewClient(connection.baseURL).
		WithHTTPClient(connection.httpClient).
		WithTimeout(2 * time.Second).
		WithMaxRetries(0).
		WithContext(ctx)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		if _, err := client.GetHealth(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("service DNS identity %q: last health error: %w", instance.serverName, errors.Join(ctx.Err(), lastErr))
		case <-ticker.C:
		}
	}
}

func mintServiceAccountToken(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
	serviceAccount string,
) (string, error) {
	expiration := int64(time.Hour.Seconds())
	request, err := kube.CoreV1().ServiceAccounts(namespace).CreateToken(
		ctx,
		serviceAccount,
		&authnv1.TokenRequest{
			Spec: authnv1.TokenRequestSpec{ExpirationSeconds: &expiration},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", err
	}
	if request.Status.Token == "" {
		return "", errors.New("Kubernetes returned an empty service-account token")
	}
	return request.Status.Token, nil
}

func findInspectBFCLProvider(ctx context.Context, client *evalhubclient.Client) (*api.ProviderResource, error) {
	providers := make([]api.ProviderResource, 0)
	for offset := 0; ; {
		page, err := client.ListProviders(evalhubclient.WithLimit(100), evalhubclient.WithOffset(offset))
		if err != nil {
			return nil, err
		}
		providers = append(providers, page.Items...)
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.TotalCount {
			break
		}
	}
	for i := range providers {
		provider := &providers[i]
		if provider.Resource.ID != inspectProviderID {
			continue
		}
		if provider.Runtime == nil || provider.Runtime.K8s == nil || provider.Runtime.K8s.Image == "" {
			return nil, nil
		}
		for _, benchmark := range provider.Benchmarks {
			if benchmark.ID == bfclBenchmarkID {
				return provider, nil
			}
		}
	}
	return nil, nil
}

func inspectAirgapPolicies(policies []networkingv1.NetworkPolicy) ([]string, bool, string) {
	jobLabels := labels.Set(map[string]string{
		"app":       "evalhub",
		"component": "evaluation-job",
	})
	matching := make([]string, 0)
	hasEgressPolicy := false
	allowsExternal := false
	for i := range policies {
		policy := &policies[i]
		selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		if err != nil || !selector.Matches(jobLabels) {
			continue
		}
		egressPolicy := len(policy.Spec.Egress) > 0
		for _, policyType := range policy.Spec.PolicyTypes {
			if policyType == networkingv1.PolicyTypeEgress {
				egressPolicy = true
				break
			}
		}
		if !egressPolicy {
			continue
		}
		hasEgressPolicy = true
		matching = append(matching, policy.Name)
		for _, rule := range policy.Spec.Egress {
			if len(rule.To) == 0 {
				allowsExternal = true
				continue
			}
			for _, peer := range rule.To {
				// This test only treats namespace/pod-scoped destinations as disconnected.
				// An IPBlock could include an external endpoint, so require a stricter policy.
				if peer.IPBlock != nil {
					allowsExternal = true
				}
			}
		}
	}
	sort.Strings(matching)
	if !hasEgressPolicy {
		return matching, false, "no NetworkPolicy selects evaluation-job pods for egress"
	}
	if allowsExternal {
		return matching, false, "a NetworkPolicy selecting evaluation-job pods permits unrestricted or IPBlock egress"
	}
	return matching, true, ""
}

func discoverBFCLAssets(
	ctx context.Context,
	kube kubernetes.Interface,
	client *evalhubclient.Client,
	namespace string,
) ([]bfclAsset, error) {
	candidates := make(map[string]bfclAsset)
	for offset := 0; ; {
		page, err := client.ListCollections(evalhubclient.WithLimit(100), evalhubclient.WithOffset(offset))
		if err != nil {
			return nil, err
		}
		for _, collection := range page.Items {
			for _, benchmark := range collection.Benchmarks {
				if benchmark.ProviderID != inspectProviderID || benchmark.ID != bfclBenchmarkID ||
					benchmark.TestDataRef == nil || benchmark.TestDataRef.PVC == nil {
					continue
				}
				pvcRef := benchmark.TestDataRef.PVC
				asset := bfclAsset{
					claimName: pvcRef.ClaimName,
					subPath:   pvcRef.SubPath,
					source:    "collection " + collection.Resource.ID,
					ref:       benchmark.TestDataRef,
				}
				candidates[asset.claimName+"\x00"+asset.subPath] = asset
			}
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.TotalCount {
			break
		}
	}

	pvcs, err := kube.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list PVCs: %w", err)
	}
	for _, pvc := range pvcs.Items {
		if pvc.Labels[bfclAssetLabel] != "true" {
			continue
		}
		asset := bfclAsset{
			claimName: pvc.Name,
			subPath:   pvc.Annotations[bfclSubPathAnnotation],
			source:    "PVC label " + bfclAssetLabel,
		}
		key := asset.claimName + "\x00" + asset.subPath
		if _, exists := candidates[key]; !exists {
			asset.ref = &api.TestDataRef{
				PVC: &api.PVCTestDataRef{ClaimName: asset.claimName, SubPath: asset.subPath},
			}
			candidates[key] = asset
		}
	}
	assets := make([]bfclAsset, 0, len(candidates))
	for _, asset := range candidates {
		if asset.claimName != "" {
			assets = append(assets, asset)
		}
	}
	sort.Slice(assets, func(i, j int) bool {
		if assets[i].claimName != assets[j].claimName {
			return assets[i].claimName < assets[j].claimName
		}
		return assets[i].subPath < assets[j].subPath
	})
	return assets, nil
}

func discoverMockTargetService(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
) (*corev1.Service, error) {
	services, err := kube.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for i := range services.Items {
		service := &services.Items[i]
		if service.Spec.Selector["app"] != "evalhub-kind-mock-target" {
			continue
		}
		if _, err := servicePort(service, "http", 8000); err != nil {
			continue
		}
		endpoints, err := kube.CoreV1().Endpoints(namespace).Get(ctx, service.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get Endpoints for Service/%s: %w", service.Name, err)
		}
		for _, subset := range endpoints.Subsets {
			if len(subset.Addresses) > 0 {
				return service, nil
			}
		}
		return nil, fmt.Errorf("mock target Service/%s has no ready endpoints", service.Name)
	}
	return nil, nil
}

func discoverMockTargetImage(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
) (string, []corev1.LocalObjectReference, *corev1.PodSecurityContext, *corev1.SecurityContext, error) {
	pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=evalhub-kind-mock-target",
	})
	if err != nil {
		return "", nil, nil, nil, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase != corev1.PodRunning || !podReady(pod) || len(pod.Spec.Containers) == 0 {
			continue
		}
		var podSecurityContext *corev1.PodSecurityContext
		if pod.Spec.SecurityContext != nil {
			podSecurityContext = pod.Spec.SecurityContext.DeepCopy()
		}
		var containerSecurityContext *corev1.SecurityContext
		if pod.Spec.Containers[0].SecurityContext != nil {
			containerSecurityContext = pod.Spec.Containers[0].SecurityContext.DeepCopy()
		}
		return pod.Spec.Containers[0].Image,
			append([]corev1.LocalObjectReference(nil), pod.Spec.ImagePullSecrets...),
			podSecurityContext,
			containerSecurityContext,
			nil
	}
	return "", nil, nil, nil, errors.New("mock target Service has no ready pod with a usable container image")
}

func stageBFCLAssets(
	ctx context.Context,
	kube kubernetes.Interface,
	contextName string,
	namespace string,
	image string,
	imagePullSecrets []corev1.LocalObjectReference,
	podSecurityContext *corev1.PodSecurityContext,
	containerSecurityContext *corev1.SecurityContext,
) (asset bfclAsset, cleanup func(), retErr error) {
	dataDir, cleanupData, err := downloadBFCLData(ctx)
	if err != nil {
		return bfclAsset{}, nil, err
	}
	defer cleanupData()

	suffix, err := randomResourceSuffix()
	if err != nil {
		return bfclAsset{}, nil, fmt.Errorf("generate unique staging resource names: %w", err)
	}
	claimName := bfclClaimPrefix + suffix
	podName := bfclStagerPodPrefix + suffix

	pvcCreated := false
	podCreated := false
	cleanupResources := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if podCreated {
			_ = deletePodAndWait(cleanupCtx, kube, namespace, podName)
		}
		if pvcCreated {
			_ = kube.CoreV1().PersistentVolumeClaims(namespace).Delete(cleanupCtx, claimName, metav1.DeleteOptions{})
		}
	}
	defer func() {
		if retErr != nil {
			cleanupResources()
		}
	}()

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "evalhub-test-e2e",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	if _, err := kube.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return bfclAsset{}, nil, fmt.Errorf("create temporary BFCL PVC %s/%s: %w", namespace, claimName, err)
	}
	pvcCreated = true
	stagerPodSecurityContext := podSecurityContext
	if podSecurityContext != nil {
		stagerPodSecurityContext = podSecurityContext.DeepCopy()
	} else {
		stagerPodSecurityContext = &corev1.PodSecurityContext{}
	}
	if stagerPodSecurityContext.FSGroup == nil {
		groupID := int64(10001)
		if stagerPodSecurityContext.RunAsGroup != nil {
			groupID = *stagerPodSecurityContext.RunAsGroup
		} else if stagerPodSecurityContext.RunAsUser != nil {
			groupID = *stagerPodSecurityContext.RunAsUser
		}
		stagerPodSecurityContext.FSGroup = &groupID
	}
	automountServiceAccountToken := false
	stagerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "evalhub-test-e2e",
				"app":                          "evalhub-e2e-asset-stager",
			},
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: &automountServiceAccountToken,
			ImagePullSecrets:             imagePullSecrets,
			SecurityContext:              stagerPodSecurityContext,
			RestartPolicy:                corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "asset-stager",
				Image:           image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"python", "-c", "import time; time.sleep(3600)"},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "bfcl-assets",
					MountPath: "/test_data",
				}},
				SecurityContext: containerSecurityContext,
			}},
			Volumes: []corev1.Volume{{
				Name: "bfcl-assets",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName},
				},
			}},
		},
	}
	if _, err := kube.CoreV1().Pods(namespace).Create(ctx, stagerPod, metav1.CreateOptions{}); err != nil {
		return bfclAsset{}, nil, fmt.Errorf("create BFCL PVC stager pod %s/%s: %w", namespace, podName, err)
	}
	podCreated = true
	readyCtx, cancelReady := context.WithTimeout(ctx, 3*time.Minute)
	err = waitForPodReady(readyCtx, kube, namespace, podName)
	cancelReady()
	if err != nil {
		return bfclAsset{}, nil, err
	}
	bindCtx, cancelBind := context.WithTimeout(ctx, 3*time.Minute)
	err = waitForPVCBound(bindCtx, kube, namespace, claimName)
	cancelBind()
	if err != nil {
		return bfclAsset{}, nil, err
	}

	if err := streamBFCLDataToPod(ctx, contextName, namespace, podName, dataDir); err != nil {
		return bfclAsset{}, nil, fmt.Errorf("copy BFCL data to temporary PVC %s/%s: %w", namespace, claimName, err)
	}
	if err := deletePodAndWait(ctx, kube, namespace, podName); err != nil {
		return bfclAsset{}, nil, fmt.Errorf("remove BFCL staging pod %s/%s: %w", namespace, podName, err)
	}

	asset = bfclAsset{
		claimName: claimName,
		source:    "automatically downloaded BFCL source commit " + bfclSourceCommit,
		ref: &api.TestDataRef{
			PVC: &api.PVCTestDataRef{ClaimName: claimName},
		},
	}
	cleanup = func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = kube.CoreV1().PersistentVolumeClaims(namespace).Delete(cleanupCtx, claimName, metav1.DeleteOptions{})
	}
	return asset, cleanup, nil
}

func downloadBFCLData(ctx context.Context) (string, func(), error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", nil, fmt.Errorf("git is required to fetch the pinned BFCL test data on the test runner: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "evalhub-e2e-bfcl-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary BFCL download directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	repoDir := filepath.Join(tempDir, "gorilla")

	commands := [][]string{
		{"init", "--quiet", repoDir},
		{"-C", repoDir, "remote", "add", "origin", bfclSourceRepository},
		{"-C", repoDir, "fetch", "--quiet", "--depth=1", "--filter=blob:none", "origin", bfclSourceCommit},
		{"-C", repoDir, "sparse-checkout", "init", "--cone"},
		{"-C", repoDir, "sparse-checkout", "set", "--cone", bfclSourceDataPath},
		{"-C", repoDir, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
	}
	for _, args := range commands {
		command := exec.CommandContext(ctx, "git", args...)
		output, err := command.CombinedOutput()
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("fetch pinned BFCL data from GitHub with git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}

	dataDir := filepath.Join(repoDir, filepath.FromSlash(bfclSourceDataPath))
	if err := validateBFCLDataDirectory(dataDir); err != nil {
		cleanup()
		return "", nil, err
	}
	return dataDir, cleanup, nil
}

func validateBFCLDataDirectory(dataDir string) error {
	for _, relative := range []string{
		bfclSourceCanary,
		filepath.Join("possible_answer", bfclSourceCanary),
	} {
		info, err := os.Stat(filepath.Join(dataDir, relative))
		if err != nil {
			return fmt.Errorf("pinned BFCL source is missing %s: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("pinned BFCL source path %s is not a regular file", relative)
		}
	}
	return nil
}

func waitForPVCBound(ctx context.Context, kube kubernetes.Interface, namespace, name string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		pvc, err := kube.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get temporary BFCL PVC %s/%s: %w", namespace, name, err)
		}
		if pvc.Status.Phase == corev1.ClaimBound {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("temporary BFCL PVC %s/%s did not bind (phase %q): %w; check that the cluster has a default StorageClass", namespace, name, pvc.Status.Phase, ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForPodReady(ctx context.Context, kube kubernetes.Interface, namespace, name string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		pod, err := kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get BFCL staging pod %s/%s: %w", namespace, name, err)
		}
		if pod.Status.Phase == corev1.PodFailed {
			return fmt.Errorf("BFCL staging pod %s/%s failed: %s", namespace, name, pod.Status.Message)
		}
		if pod.Status.Phase == corev1.PodRunning && podReady(pod) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("BFCL staging pod %s/%s did not become Ready (phase %q): %w", namespace, name, pod.Status.Phase, ctx.Err())
		case <-ticker.C:
		}
	}
}

func deletePodAndWait(ctx context.Context, kube kubernetes.Interface, namespace, name string) error {
	if err := kube.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_, err := kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for Pod/%s deletion: %w", name, ctx.Err())
		case <-ticker.C:
		}
	}
}

func streamBFCLDataToPod(ctx context.Context, contextName, namespace, podName, dataDir string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl is required to transfer the downloaded data into the cluster: %w", err)
	}

	const extractScript = "import sys, tarfile\nroot='/test_data'\nwith tarfile.open(fileobj=sys.stdin.buffer, mode='r|') as archive:\n for member in archive:\n  if member.name != 'BFCL' and not member.name.startswith('BFCL/'):\n   raise ValueError('unexpected archive path: ' + member.name)\n  if member.issym() or member.islnk() or not (member.isdir() or member.isfile()):\n   raise ValueError('unsupported archive member: ' + member.name)\n  archive.extract(member, path=root, filter='data')\n"
	command := exec.CommandContext(ctx, "kubectl", "--context", contextName, "--namespace", namespace, "exec", "-i", podName, "--", "python", "-c", extractScript)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output

	reader, writer := io.Pipe()
	command.Stdin = reader
	if err := command.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return fmt.Errorf("start kubectl exec: %w", err)
	}

	archiveDone := make(chan error, 1)
	go func() {
		err := writeBFCLArchive(writer, dataDir)
		_ = writer.CloseWithError(err)
		archiveDone <- err
	}()

	commandErr := command.Wait()
	_ = reader.Close()
	archiveErr := <-archiveDone
	if archiveErr != nil {
		return fmt.Errorf("build archive stream: %w", archiveErr)
	}
	if commandErr != nil {
		return fmt.Errorf("kubectl exec returned an error: %w (%s)", commandErr, strings.TrimSpace(output.String()))
	}
	return nil
}

func writeBFCLArchive(destination io.Writer, dataDir string) error {
	archive := tar.NewWriter(destination)
	if err := archive.WriteHeader(&tar.Header{Name: "BFCL/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		return err
	}
	err := filepath.WalkDir(dataDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == dataDir {
			return nil
		}
		relative, err := filepath.Rel(dataDir, filePath)
		if err != nil {
			return err
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("BFCL asset path escapes source directory: %q", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = path.Join("BFCL", filepath.ToSlash(relative))
		if info.IsDir() {
			header.Mode = 0o755
			header.Typeflag = tar.TypeDir
			header.Size = 0
		} else if info.Mode().IsRegular() {
			header.Mode = 0o644
			header.Typeflag = tar.TypeReg
			header.Size = info.Size()
		} else {
			return fmt.Errorf("unsupported BFCL asset file type at %q", relative)
		}
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(archive, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = archive.Close()
		return err
	}
	return archive.Close()
}

func randomResourceSuffix() (string, error) {
	var data [6]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func getBenchmarkLogs(ctx context.Context, connection *apiConnection, jobID string, tailLines int) string {
	query := url.Values{"tail_lines": []string{fmt.Sprintf("%d", tailLines)}}
	logURL := connection.baseURL + "/api/v1/evaluations/jobs/" +
		url.PathEscape(jobID) + "/benchmarks/0/logs?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL, nil)
	if err != nil {
		return "could not create log request: " + err.Error()
	}
	request.Header.Set("Authorization", "Bearer "+connection.token)
	request.Header.Set("X-Tenant", connection.tenant)
	response, err := connection.httpClient.Do(request)
	if err != nil {
		return "could not retrieve adapter logs: " + err.Error()
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return "could not read adapter logs: " + err.Error()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Sprintf("log endpoint returned HTTP %d: %s", response.StatusCode, string(body))
	}
	return string(body)
}
