package e2e

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const curatedAssetStagerLabel = "evalhub.io/e2e-asset-stager"
const inspectHLEBenchmarkID = "inspect/hle"

//go:embed stagers/*.py
var curatedAssetStagers embed.FS

type curatedBenchmarkCase struct {
	collectionID string
	providerID   string
	benchmarkID  string
	occurrence   int
}

// This is the intentional smoke matrix for the nine curated collections. Keep
// duplicate provider/benchmark rows distinct by occurrence (Tool Use has two
// separate BFCL rows with different category lists).
var curatedBenchmarkMatrix = []curatedBenchmarkCase{
	{collectionID: "coding-v1", providerID: "lighteval", benchmarkID: "lcb:codegeneration_v6"},
	{collectionID: "document-understanding-v1", providerID: "ruler", benchmarkID: "common-words-extraction"},
	{collectionID: "document-understanding-v1", providerID: "ruler", benchmarkID: "frequency-words-extraction"},
	{collectionID: "document-understanding-v1", providerID: "ruler", benchmarkID: "qa-squad"},
	{collectionID: "document-understanding-v1", providerID: "ruler", benchmarkID: "qa-hotpotqa"},
	{collectionID: "document-understanding-v1", providerID: "ruler", benchmarkID: "niah-single-essay"},
	{collectionID: "document-understanding-v1", providerID: "ruler", benchmarkID: "variable-tracking"},
	{collectionID: "fairness-bias-v1", providerID: "lm_evaluation_harness", benchmarkID: "winogender"},
	{collectionID: "fairness-bias-v1", providerID: "lm_evaluation_harness", benchmarkID: "crows_pairs_english"},
	{collectionID: "fairness-bias-v1", providerID: "lm_evaluation_harness", benchmarkID: "bbq"},
	{collectionID: "instruction-output-v1", providerID: "ifbench", benchmarkID: "ifbench"},
	{collectionID: "instruction-output-v1", providerID: "lm_evaluation_harness", benchmarkID: "jsonschema_bench_easy"},
	{collectionID: "instruction-output-v1", providerID: "followbench", benchmarkID: "followbench"},
	{collectionID: "knowledge-reasoning-v1", providerID: "lighteval", benchmarkID: "gpqa:diamond"},
	{collectionID: "knowledge-reasoning-v1", providerID: "inspect", benchmarkID: "inspect/hle"},
	{collectionID: "knowledge-reasoning-v1", providerID: "lighteval", benchmarkID: "math_500"},
	{collectionID: "multimodal-v1", providerID: "inspect", benchmarkID: "inspect/docvqa"},
	{collectionID: "safety-ethical-principles-v1", providerID: "lm_evaluation_harness", benchmarkID: "toxigen"},
	{collectionID: "safety-ethical-principles-v1", providerID: "lm_evaluation_harness", benchmarkID: "ethics_cm"},
	{collectionID: "safety-ethical-principles-v1", providerID: "lm_evaluation_harness", benchmarkID: "bigbench_hhh_alignment_multiple_choice"},
	{collectionID: "tool-use-v1", providerID: "inspect", benchmarkID: "inspect/bfcl", occurrence: 0},
	{collectionID: "tool-use-v1", providerID: "inspect", benchmarkID: "inspect/bfcl", occurrence: 1},
	{collectionID: "trustworthiness-v1", providerID: "inspect", benchmarkID: "inspect/cyberseceval-2-pi"},
	{collectionID: "trustworthiness-v1", providerID: "lm_evaluation_harness", benchmarkID: "truthfulqa_mc1"},
}

func TestCuratedBenchmarkMatrixComplete(t *testing.T) {
	if len(curatedBenchmarkMatrix) != 24 {
		t.Fatalf("curated matrix has %d rows, want 24", len(curatedBenchmarkMatrix))
	}
	collections := make(map[string]int)
	seen := make(map[string]struct{}, len(curatedBenchmarkMatrix))
	for _, testCase := range curatedBenchmarkMatrix {
		collections[testCase.collectionID]++
		key := curatedBenchmarkCaseKey(testCase)
		if _, exists := seen[key]; exists {
			t.Errorf("duplicate curated benchmark matrix entry %q", key)
		}
		seen[key] = struct{}{}
		if !supportedCuratedAdapter(adapterIDForProvider(testCase.providerID)) {
			t.Errorf("matrix uses unsupported provider %q", testCase.providerID)
		}
	}
	if len(collections) != 9 {
		t.Errorf("matrix covers %d collections, want 9", len(collections))
	}
}

func TestCuratedBenchmarkMatrixMatchesRepositoryConfigs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	collectionsByID, err := config.LoadCollectionConfigs(logger, testhelpers.NewValidator(t), "../../config")
	if err != nil {
		t.Fatalf("load repository collection configs: %v", err)
	}

	curated := make([]api.CollectionResource, 0, 9)
	for _, collection := range collectionsByID {
		if collection.CurationOrder > 0 {
			curated = append(curated, collection)
		}
	}
	if len(curated) != 9 {
		t.Fatalf("repository has %d curated collections, want 9", len(curated))
	}

	matched, err := matchCuratedBenchmarkMatrix(curated, true)
	if err != nil {
		t.Fatalf("curated matrix does not match repository config: %v", err)
	}
	if len(matched) != len(curatedBenchmarkMatrix) {
		t.Fatalf("matched %d benchmark rows, want %d", len(matched), len(curatedBenchmarkMatrix))
	}
}

func TestFilterCuratedCasesForHFToken(t *testing.T) {
	cases := []curatedBenchmarkCase{
		{collectionID: "knowledge-reasoning-v1", providerID: "inspect", benchmarkID: inspectHLEBenchmarkID},
		{collectionID: "multimodal-v1", providerID: "inspect", benchmarkID: "inspect/docvqa"},
		{collectionID: "tool-use-v1", providerID: "inspect", benchmarkID: bfclBenchmarkID},
	}

	t.Run("without token skips only HLE", func(t *testing.T) {
		runnable, skipped := filterCuratedCasesForHFToken(cases, false)
		if len(runnable) != 2 || runnable[0].benchmarkID != "inspect/docvqa" || runnable[1].benchmarkID != bfclBenchmarkID {
			t.Fatalf("runnable cases = %#v, want DocVQA and BFCL", runnable)
		}
		if len(skipped) != 1 || skipped[0].benchmarkID != inspectHLEBenchmarkID {
			t.Fatalf("skipped cases = %#v, want only HLE", skipped)
		}
	})

	t.Run("with token keeps HLE", func(t *testing.T) {
		runnable, skipped := filterCuratedCasesForHFToken(cases, true)
		if len(runnable) != len(cases) || len(skipped) != 0 {
			t.Fatalf("runnable=%d skipped=%d, want runnable=%d skipped=0", len(runnable), len(skipped), len(cases))
		}
	})
}

func TestInspectStagerEnvUsesOptionalSecretReference(t *testing.T) {
	if env := inspectStagerEnv("inspect", ""); len(env) != 0 {
		t.Fatalf("Inspect stager env without Secret = %#v, want no additional variables", env)
	}
	if env := inspectStagerEnv("lighteval", "hf-secret"); len(env) != 0 {
		t.Fatalf("non-Inspect stager env = %#v, want no additional variables", env)
	}
	env := inspectStagerEnv("inspect", "hf-secret")
	if len(env) != 1 || env[0].Name != "HF_TOKEN" || env[0].ValueFrom == nil || env[0].ValueFrom.SecretKeyRef == nil {
		t.Fatalf("Inspect stager env = %#v, want HF_TOKEN from Secret", env)
	}
	if got := env[0].ValueFrom.SecretKeyRef.Name; got != "hf-secret" {
		t.Errorf("Secret name = %q, want hf-secret", got)
	}
	if got := env[0].ValueFrom.SecretKeyRef.Key; got != "HF_TOKEN" {
		t.Errorf("Secret key = %q, want HF_TOKEN", got)
	}
}

// TestCuratedCollectionsAirgap runs one job for each curated benchmark row.
// Each adapter group stages only the inputs its rows use; jobs are serialized,
// and their Kubernetes Jobs/Pods and temporary PVCs are removed after logs and
// image provenance have been collected. EvalHub job records remain available.
func TestCuratedCollectionsAirgap(t *testing.T) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	rawConfig, err := loadingRules.Load()
	if err != nil {
		t.Skipf("no usable kubeconfig is available for automatic e2e discovery: %v", err)
	}
	contextName := rawConfig.CurrentContext
	if contextName == "" {
		t.Skip("kubeconfig has no current context; select the target cluster with kubectl config use-context")
	}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
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
		t.Skipf("context %q has no ready EvalHub API pod matching %q", contextName, evalHubAPIPodSelector)
	}
	for _, instance := range instances {
		instance := instance
		t.Run(instance.namespace+"/"+instance.serviceName, func(t *testing.T) {
			runCuratedCollectionsAirgap(t, ctx, contextName, kube, instance)
		})
	}
}

func runCuratedCollectionsAirgap(
	t *testing.T,
	parentCtx context.Context,
	contextName string,
	kube kubernetes.Interface,
	instance evalHubInstance,
) {
	t.Helper()
	manifest, err := discoverDeploymentManifest(parentCtx, kube, instance.namespace)
	if err != nil {
		t.Fatalf("discover dev-deploy selection in namespace %q: %v", instance.namespace, err)
	}
	if manifest.managed {
		requireDisconnectedDeployment(t, manifest)
	}
	connection, closeConnection := newDeploymentTestAPIConnection(t, parentCtx, contextName, kube, instance)
	defer closeConnection()

	collections, err := listCuratedCollections(connection.client)
	if err != nil {
		t.Fatalf("list curated collections: %v", err)
	}
	benchmarksByCase, err := matchCuratedBenchmarkMatrix(collections, manifest.managed)
	if err != nil {
		t.Fatalf("curated catalog does not match the expected disconnected test matrix: %v", err)
	}

	providers := listDeploymentTestProviders(t, parentCtx, connection.client)
	providersByID := make(map[string]api.ProviderResource, len(providers))
	for _, provider := range providers {
		providersByID[provider.Resource.ID] = provider
	}
	selectedCases := make([]curatedBenchmarkCase, 0, len(curatedBenchmarkMatrix))
	for _, testCase := range curatedBenchmarkMatrix {
		adapterID := adapterIDForProvider(testCase.providerID)
		if manifest.managed && !manifest.adapterSelected(adapterID) {
			continue
		}
		provider, ok := providersByID[testCase.providerID]
		if !ok || provider.Runtime == nil || provider.Runtime.K8s == nil || provider.Runtime.K8s.Image == "" {
			if manifest.managed {
				t.Fatalf("selected adapter %q has no Kubernetes provider image for provider %q", adapterID, testCase.providerID)
			}
			continue
		}
		if err := manifest.verifyProviderImage(adapterID, provider.Runtime.K8s.Image); err != nil {
			t.Fatalf("provider image does not match dev-deploy manifest: %v", err)
		}
		selectedCases = append(selectedCases, testCase)
	}
	if len(selectedCases) == 0 {
		t.Skip("none of the curated adapters selected by dev-deploy have benchmark cases in this matrix")
	}

	hfToken := strings.TrimSpace(os.Getenv("HF_TOKEN"))
	selectedCases, skippedCases := filterCuratedCasesForHFToken(selectedCases, hfToken != "")
	for _, testCase := range skippedCases {
		t.Logf("skipping %s/%s: set HF_TOKEN to stage this gated Hugging Face dataset", testCase.collectionID, testCase.benchmarkID)
	}
	if len(selectedCases) == 0 {
		t.Skip("all selected curated benchmark cases require credentials that are not configured")
	}

	policies, err := kube.NetworkingV1().NetworkPolicies(instance.namespace).List(parentCtx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list NetworkPolicies in namespace %q: %v", instance.namespace, err)
	}
	policyNames, restricted, reason := inspectAirgapPolicies(policies.Items)
	if !restricted {
		if manifest.managed && manifest.airgap {
			t.Fatalf("dev-deploy manifest says AIRGAP=true, but no restrictive evaluation-job egress policy applies: %s", reason)
		}
		t.Skipf("namespace %q is not configured for a disconnected evaluation test: %s", instance.namespace, reason)
	}
	t.Logf("detected restrictive evaluation-job egress policies: %s", strings.Join(policyNames, ", "))

	targetService, err := discoverMockTargetService(parentCtx, kube, instance.namespace)
	if err != nil {
		t.Fatalf("discover ready mock target service: %v", err)
	}
	if targetService == nil {
		if manifest.managed {
			t.Fatal("dev-deploy cluster is missing the required mock target service")
		}
		t.Skipf("no ready mock target Service is available in namespace %q", instance.namespace)
	}
	targetPort, err := servicePort(targetService, "http", 8000)
	if err != nil {
		t.Fatalf("mock target service %s/%s: %v", instance.namespace, targetService.Name, err)
	}
	judgeService, err := discoverCuratedMockService(parentCtx, kube, instance.namespace, "evalhub-kind-mock-judge")
	if err != nil {
		t.Fatalf("discover ready mock judge service: %v", err)
	}
	if judgeService == nil {
		if manifest.managed {
			t.Fatal("dev-deploy cluster is missing the required mock judge service")
		}
		t.Skipf("no ready mock judge Service is available in namespace %q", instance.namespace)
	}
	judgePort, err := servicePort(judgeService, "http", 8000)
	if err != nil {
		t.Fatalf("mock judge service %s/%s: %v", instance.namespace, judgeService.Name, err)
	}

	secretName := "eh-e2e-curated-" + mustRandomResourceSuffix(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: instance.namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "evalhub-test-e2e"}},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"api-key":       []byte(mockTargetAPIKey),
			"judge_api-key": []byte("judge-test-token"),
			"judge_url":     []byte(fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/v1", judgeService.Name, instance.namespace, judgePort)),
		},
	}
	if _, err := kube.CoreV1().Secrets(instance.namespace).Create(parentCtx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create temporary mock credentials Secret %s/%s: %v", instance.namespace, secretName, err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = kube.CoreV1().Secrets(instance.namespace).Delete(cleanupCtx, secretName, metav1.DeleteOptions{})
	}()

	hfTokenSecretName := ""
	if hfToken != "" && containsCuratedCase(selectedCases, "inspect", inspectHLEBenchmarkID) {
		hfTokenSecretName = "eh-e2e-hf-" + mustRandomResourceSuffix(t)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: hfTokenSecretName, Namespace: instance.namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "evalhub-test-e2e"}},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{"HF_TOKEN": []byte(hfToken)},
		}
		if _, err := kube.CoreV1().Secrets(instance.namespace).Create(parentCtx, secret, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create temporary Hugging Face staging Secret %s/%s: %v", instance.namespace, hfTokenSecretName, err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = kube.CoreV1().Secrets(instance.namespace).Delete(cleanupCtx, hfTokenSecretName, metav1.DeleteOptions{})
		}()
	}

	// Stage one narrow bundle per adapter, then run that adapter's cases before
	// moving to the next bundle. This bounds temporary PVC use and ensures each
	// data-backed benchmark request carries its own test_data_ref.
	for _, adapterID := range []string{"inspect", "lm-evaluation-harness", "lighteval", "ruler", "ifbench", "followbench"} {
		adapterCases := casesForAdapter(selectedCases, adapterID)
		if len(adapterCases) == 0 {
			continue
		}
		providerID := providerIDForAdapter(adapterID)
		provider := providersByID[providerID]
		logDeployedAdapter(t, manifest, adapterID)
		func() {
			var refs curatedAssetRefs
			if adapterRequiresStagedAssets(adapterID) {
				stageCtx, cancel := context.WithTimeout(parentCtx, 35*time.Minute)
				refs, err = stageCuratedAssets(stageCtx, kube, contextName, instance, adapterID, provider.Runtime.K8s.Image, provider.Runtime.K8s.ImagePullPolicy, hfTokenSecretName)
				cancel()
				if err != nil {
					t.Fatalf("stage curated %s assets: %v", adapterID, err)
				}
				defer refs.cleanup()
			}
			for _, testCase := range adapterCases {
				testCase := testCase
				benchmark, ok := benchmarksByCase[curatedBenchmarkCaseKey(testCase)]
				if !ok {
					t.Logf("skipping unavailable catalog case %s/%s#%d", testCase.collectionID, testCase.benchmarkID, testCase.occurrence)
					continue
				}
				t.Run(testCase.collectionID+"/"+testCase.benchmarkID+fmt.Sprintf("#%d", testCase.occurrence), func(t *testing.T) {
					testDataRef := refs.forCase(testCase)
					if adapterRequiresStagedAssets(adapterID) && testDataRef == nil {
						t.Fatal("asset staging returned no test_data_ref for a data-backed benchmark")
					}
					runCuratedBenchmarkCase(t, parentCtx, kube, instance, connection, manifest, testCase, benchmark, secretName, targetService, targetPort, testDataRef)
				})
			}
		}()
	}
}

type curatedAssetRefs struct {
	general *api.TestDataRef
	bfcl    *api.TestDataRef
	cleanup func()
}

func (refs curatedAssetRefs) forCase(testCase curatedBenchmarkCase) *api.TestDataRef {
	if testCase.providerID == "inspect" && testCase.benchmarkID == bfclBenchmarkID {
		return refs.bfcl
	}
	return refs.general
}

func stageCuratedAssets(
	ctx context.Context,
	kube kubernetes.Interface,
	contextName string,
	instance evalHubInstance,
	adapterID string,
	image string,
	imagePullPolicy string,
	hfTokenSecretName string,
) (curatedAssetRefs, error) {
	program, storage, err := curatedAssetStagingProgram(adapterID)
	if err != nil {
		return curatedAssetRefs{}, err
	}
	mockImage, imagePullSecrets, podSecurityContext, containerSecurityContext, err := discoverMockTargetImage(ctx, kube, instance.namespace)
	if err != nil {
		return curatedAssetRefs{}, fmt.Errorf("discover mock target security and image-pull settings for staging: %w", err)
	}
	claimName, cleanup, err := stagePythonAssetBundle(
		ctx,
		kube,
		instance,
		adapterID,
		image,
		imagePullPolicy,
		storage,
		program,
		inspectStagerEnv(adapterID, hfTokenSecretName),
		imagePullSecrets,
		podSecurityContext,
		containerSecurityContext,
	)
	if err != nil {
		return curatedAssetRefs{}, err
	}
	refs := curatedAssetRefs{
		general: &api.TestDataRef{PVC: &api.PVCTestDataRef{ClaimName: claimName}},
		cleanup: cleanup,
	}
	if adapterID == "inspect" {
		bfcl, cleanupBFCL, stageErr := stageBFCLAssets(ctx, kube, contextName, instance.namespace, mockImage, imagePullSecrets, podSecurityContext, containerSecurityContext)
		if stageErr != nil {
			cleanup()
			return curatedAssetRefs{}, fmt.Errorf("stage full BFCL cache: %w", stageErr)
		}
		refs.bfcl = bfcl.ref
		previousCleanup := refs.cleanup
		refs.cleanup = func() { cleanupBFCL(); previousCleanup() }
	}
	return refs, nil
}

func stagePythonAssetBundle(
	ctx context.Context,
	kube kubernetes.Interface,
	instance evalHubInstance,
	adapterID string,
	image string,
	imagePullPolicy string,
	storage string,
	program string,
	extraEnv []corev1.EnvVar,
	imagePullSecrets []corev1.LocalObjectReference,
	podSecurityContext *corev1.PodSecurityContext,
	containerSecurityContext *corev1.SecurityContext,
) (claimName string, cleanup func(), retErr error) {
	suffix, err := randomResourceSuffix()
	if err != nil {
		return "", nil, err
	}
	claimName = "evalhub-e2e-" + strings.ReplaceAll(adapterID, "-", "") + "-" + suffix
	podName := "evalhub-e2e-stage-" + suffix
	pvcCreated, podCreated := false, false
	cleanupResources := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if podCreated {
			_ = deletePodAndWait(cleanupCtx, kube, instance.namespace, podName)
		}
		if pvcCreated {
			_ = kube.CoreV1().PersistentVolumeClaims(instance.namespace).Delete(cleanupCtx, claimName, metav1.DeleteOptions{})
		}
	}
	defer func() {
		if retErr != nil {
			cleanupResources()
		}
	}()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: claimName, Namespace: instance.namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "evalhub-test-e2e", curatedAssetStagerLabel: adapterID}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(storage)}},
		},
	}
	if _, err := kube.CoreV1().PersistentVolumeClaims(instance.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return "", nil, fmt.Errorf("create asset PVC %s/%s: %w", instance.namespace, claimName, err)
	}
	pvcCreated = true
	automountToken := false
	deadline := int64((30 * time.Minute).Seconds())
	pullPolicy := corev1.PullIfNotPresent
	if imagePullPolicy == "always" {
		pullPolicy = corev1.PullAlways
	}
	python := "python"
	if adapterID == "lm-evaluation-harness" {
		python = "/opt/app-root/bin/python"
	}
	stagerPodSecurityContext := &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true)}
	if podSecurityContext != nil {
		stagerPodSecurityContext = podSecurityContext.DeepCopy()
	}
	groupID := int64(10001)
	if stagerPodSecurityContext.FSGroup != nil {
		groupID = *stagerPodSecurityContext.FSGroup
	} else if stagerPodSecurityContext.RunAsGroup != nil {
		groupID = *stagerPodSecurityContext.RunAsGroup
	} else if stagerPodSecurityContext.RunAsUser != nil {
		groupID = *stagerPodSecurityContext.RunAsUser
	}
	// Keep the adapter image's baked-in user so images such as LMES that use
	// UID 65532 can import their own packages. Reuse the cluster's admitted UID
	// only as the PVC group when there is no explicit volume group.
	stagerPodSecurityContext.RunAsUser = nil
	stagerPodSecurityContext.RunAsGroup = nil
	if stagerPodSecurityContext.FSGroup == nil {
		stagerPodSecurityContext.FSGroup = &groupID
	}
	var stagerContainerSecurityContext *corev1.SecurityContext
	if containerSecurityContext != nil {
		stagerContainerSecurityContext = containerSecurityContext.DeepCopy()
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: instance.namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "evalhub-test-e2e", curatedAssetStagerLabel: adapterID}},
		Spec: corev1.PodSpec{
			ServiceAccountName: instance.serviceAccount, AutomountServiceAccountToken: &automountToken,
			ImagePullSecrets: append([]corev1.LocalObjectReference(nil), imagePullSecrets...),
			RestartPolicy:    corev1.RestartPolicyNever, ActiveDeadlineSeconds: &deadline,
			SecurityContext: stagerPodSecurityContext,
			Containers: []corev1.Container{{
				Name: "asset-stager", Image: image, ImagePullPolicy: pullPolicy,
				Command: []string{python, "-c", program},
				Env: append([]corev1.EnvVar{
					{Name: "HOME", Value: "/test_data/home"}, {Name: "TMPDIR", Value: "/tmp"},
					{Name: "HF_HOME", Value: "/test_data"}, {Name: "HF_HUB_CACHE", Value: "/test_data/hub"},
					{Name: "HF_DATASETS_CACHE", Value: "/test_data/datasets"}, {Name: "XDG_CACHE_HOME", Value: "/test_data/cache"},
					{Name: "HF_HUB_OFFLINE", Value: "0"}, {Name: "HF_DATASETS_OFFLINE", Value: "0"},
					{Name: "HF_EVALUATE_OFFLINE", Value: "0"}, {Name: "TRANSFORMERS_OFFLINE", Value: "0"},
					{Name: "PYTHONDONTWRITEBYTECODE", Value: "1"},
				}, extraEnv...),
				VolumeMounts:    []corev1.VolumeMount{{Name: "assets", MountPath: "/test_data"}, {Name: "tmp", MountPath: "/tmp"}},
				SecurityContext: stagerContainerSecurityContext,
				Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("512Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("12Gi")}},
			}},
			Volumes: []corev1.Volume{
				{Name: "assets", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName}}},
				{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: resource.NewQuantity(2<<30, resource.BinarySI)}}},
			},
		},
	}
	if _, err := kube.CoreV1().Pods(instance.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", nil, fmt.Errorf("create %s asset-stager Pod: %w", adapterID, err)
	}
	podCreated = true
	if err := waitForPVCBound(ctx, kube, instance.namespace, claimName); err != nil {
		return "", nil, err
	}
	completed, logs, err := waitForCuratedStager(ctx, kube, instance.namespace, podName)
	if err != nil {
		return "", nil, fmt.Errorf("%s asset staging failed: %w\nstager log:\n%s", adapterID, err, logs)
	}
	if completed.Status.Phase != corev1.PodSucceeded || !strings.Contains(logs, "CURATED_ASSETS_READY") {
		return "", nil, fmt.Errorf("%s asset staging did not complete successfully (phase=%s)\nstager log:\n%s", adapterID, completed.Status.Phase, logs)
	}
	if err := deletePodAndWait(ctx, kube, instance.namespace, podName); err != nil {
		return "", nil, err
	}
	podCreated = false
	cleanup = func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_ = kube.CoreV1().PersistentVolumeClaims(instance.namespace).Delete(cleanupCtx, claimName, metav1.DeleteOptions{})
	}
	return claimName, cleanup, nil
}

func waitForCuratedStager(ctx context.Context, kube kubernetes.Interface, namespace, podName string) (*corev1.Pod, string, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		pod, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return nil, "", fmt.Errorf("get staging Pod/%s: %w", podName, err)
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			logs, logErr := readPodLogs(ctx, kube, namespace, podName, "asset-stager", 1200)
			if logErr != nil {
				return pod, "", logErr
			}
			return pod, logs, nil
		case corev1.PodFailed:
			logs, _ := readPodLogs(ctx, kube, namespace, podName, "asset-stager", 1200)
			return pod, logs, fmt.Errorf("staging pod failed: %s", pod.Status.Message)
		}
		select {
		case <-ctx.Done():
			logs, _ := readPodLogs(context.Background(), kube, namespace, podName, "asset-stager", 1200)
			return pod, logs, fmt.Errorf("waiting for staging pod: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func curatedAssetStagingProgram(adapterID string) (program, storage string, err error) {
	switch adapterID {
	case "inspect":
		return embeddedCuratedStager("inspect.py"), "24Gi", nil
	case "lm-evaluation-harness":
		return embeddedCuratedStager("lmes.py"), "20Gi", nil
	case "lighteval":
		return embeddedCuratedStager("lighteval.py"), "16Gi", nil
	case "ruler":
		return embeddedCuratedStager("ruler.py"), "4Gi", nil
	default:
		return "", "", fmt.Errorf("adapter %q has no staged asset program", adapterID)
	}
}

func embeddedCuratedStager(name string) string {
	program, err := fs.ReadFile(curatedAssetStagers, "stagers/"+name)
	if err != nil {
		panic(fmt.Sprintf("read embedded %s asset stager: %v", name, err))
	}
	return string(program)
}

func adapterRequiresStagedAssets(adapterID string) bool {
	switch adapterID {
	case "inspect", "lm-evaluation-harness", "lighteval", "ruler":
		return true
	default:
		return false
	}
}

func providerIDForAdapter(adapterID string) string {
	for _, provider := range selectedAdapterProviders {
		if provider.adapterID == adapterID {
			return provider.providerID
		}
	}
	return ""
}

func adapterIDForProvider(providerID string) string {
	for _, provider := range selectedAdapterProviders {
		if provider.providerID == providerID {
			return provider.adapterID
		}
	}
	return ""
}

func casesForAdapter(cases []curatedBenchmarkCase, adapterID string) []curatedBenchmarkCase {
	result := make([]curatedBenchmarkCase, 0)
	for _, testCase := range cases {
		if adapterIDForProvider(testCase.providerID) == adapterID {
			result = append(result, testCase)
		}
	}
	return result
}

func filterCuratedCasesForHFToken(cases []curatedBenchmarkCase, tokenAvailable bool) (runnable, skipped []curatedBenchmarkCase) {
	for _, testCase := range cases {
		if !tokenAvailable && testCase.providerID == "inspect" && testCase.benchmarkID == inspectHLEBenchmarkID {
			skipped = append(skipped, testCase)
			continue
		}
		runnable = append(runnable, testCase)
	}
	return runnable, skipped
}

func containsCuratedCase(cases []curatedBenchmarkCase, providerID, benchmarkID string) bool {
	for _, testCase := range cases {
		if testCase.providerID == providerID && testCase.benchmarkID == benchmarkID {
			return true
		}
	}
	return false
}

func inspectStagerEnv(adapterID, hfTokenSecretName string) []corev1.EnvVar {
	if adapterID != "inspect" || hfTokenSecretName == "" {
		return nil
	}
	return []corev1.EnvVar{{
		Name: "HF_TOKEN",
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: hfTokenSecretName},
			Key:                  "HF_TOKEN",
		}},
	}}
}

func curatedBenchmarkCaseKey(testCase curatedBenchmarkCase) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", testCase.collectionID, testCase.providerID, testCase.benchmarkID, testCase.occurrence)
}

func listCuratedCollections(client *evalhubclient.Client) ([]api.CollectionResource, error) {
	collections := make([]api.CollectionResource, 0)
	for offset := 0; ; {
		page, err := client.ListCollections(evalhubclient.WithLimit(100), evalhubclient.WithOffset(offset))
		if err != nil {
			return nil, err
		}
		for _, collection := range page.Items {
			if collection.CurationOrder > 0 {
				collections = append(collections, collection)
			}
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.TotalCount {
			return collections, nil
		}
	}
}

func matchCuratedBenchmarkMatrix(collections []api.CollectionResource, requireComplete bool) (map[string]api.CollectionBenchmarkConfig, error) {
	actual := make(map[string][]api.CollectionBenchmarkConfig)
	collectionIDs := make(map[string]struct{})
	for _, collection := range collections {
		id := collection.Resource.ID
		collectionIDs[id] = struct{}{}
		seen := make(map[string]int)
		for _, benchmark := range collection.Benchmarks {
			base := id + "\x00" + benchmark.ProviderID + "\x00" + benchmark.ID
			occurrence := seen[base]
			seen[base]++
			key := curatedBenchmarkCaseKey(curatedBenchmarkCase{collectionID: id, providerID: benchmark.ProviderID, benchmarkID: benchmark.ID, occurrence: occurrence})
			actual[key] = append(actual[key], benchmark)
		}
	}
	expectedCollectionIDs := make(map[string]struct{})
	for _, testCase := range curatedBenchmarkMatrix {
		expectedCollectionIDs[testCase.collectionID] = struct{}{}
	}
	var problems []string
	for id := range expectedCollectionIDs {
		if _, ok := collectionIDs[id]; !ok && requireComplete {
			problems = append(problems, "missing curated collection "+id)
		}
	}
	for id := range collectionIDs {
		if _, ok := expectedCollectionIDs[id]; !ok {
			problems = append(problems, "unexpected curated collection "+id)
		}
	}
	matched := make(map[string]api.CollectionBenchmarkConfig, len(curatedBenchmarkMatrix))
	for _, testCase := range curatedBenchmarkMatrix {
		key := curatedBenchmarkCaseKey(testCase)
		rows := actual[key]
		if len(rows) == 0 && !requireComplete {
			continue
		}
		if len(rows) != 1 {
			problems = append(problems, fmt.Sprintf("%s has %d matching catalog rows, want exactly 1", key, len(rows)))
			continue
		}
		matched[key] = rows[0]
		delete(actual, key)
	}
	for key, rows := range actual {
		problems = append(problems, fmt.Sprintf("unexpected catalog row %s (%d rows)", key, len(rows)))
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return matched, nil
}

func discoverCuratedMockService(ctx context.Context, kube kubernetes.Interface, namespace, app string) (*corev1.Service, error) {
	services, err := kube.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for i := range services.Items {
		service := &services.Items[i]
		if service.Spec.Selector["app"] != app {
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
		return nil, fmt.Errorf("Service/%s has no ready endpoints", service.Name)
	}
	return nil, nil
}

func runCuratedBenchmarkCase(
	t *testing.T,
	parentCtx context.Context,
	kube kubernetes.Interface,
	instance evalHubInstance,
	connection *apiConnection,
	manifest deploymentManifest,
	testCase curatedBenchmarkCase,
	collectionBenchmark api.CollectionBenchmarkConfig,
	secretName string,
	targetService *corev1.Service,
	targetPort int32,
	testDataRef *api.TestDataRef,
) {
	t.Helper()
	benchmark := collectionBenchmark.ToEvaluationBenchmark()
	parameters := benchmark.Parameters
	if parameters == nil {
		parameters = make(map[string]any)
	}
	benchmark.Parameters = parameters
	parameters["num_examples"] = 1
	switch testCase.providerID {
	case "inspect":
		if testCase.benchmarkID == bfclBenchmarkID {
			parameters["model_args"] = map[string]any{"responses_api": false}
		}
		if testCase.benchmarkID == "inspect/hle" || testCase.benchmarkID == "inspect/cyberseceval-2-pi" {
			parameters["judge_model"] = "kind-judge-model"
		}
	case "lm_evaluation_harness":
		parameters["tokenizer"] = "/test_data/tokenizer"
	case "lighteval":
		parameters["tokenizer"] = "/test_data/tokenizer"
	case "ruler":
		parameters["num_samples"] = 1
		parameters["context_lengths"] = []int{4096}
		parameters["tokenizer_path"] = "/test_data/tokenizer"
	case "ifbench":
		parameters["max_concurrent"] = 1
	case "followbench":
		parameters["judge_model"] = "kind-judge-model"
	}
	benchmark.TestDataRef = testDataRef
	jobName := "e2e-curated-" + mustRandomResourceSuffix(t)
	jobConfig := api.EvaluationJobConfig{
		Name: jobName,
		Model: &api.ModelRef{
			URL:  fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/v1", targetService.Name, instance.namespace, targetPort),
			Name: mockTargetModelName,
			Auth: &api.ModelAuth{SecretRef: secretName},
		},
		Benchmarks: []api.EvaluationBenchmarkConfig{benchmark},
	}
	jobCtx, cancelJob := context.WithTimeout(parentCtx, 12*time.Minute)
	defer cancelJob()
	jobClient := connection.client.WithContext(jobCtx)
	created, err := jobClient.CreateJob(jobConfig)
	if err != nil {
		t.Fatalf("submit disconnected benchmark job: %v", err)
	}
	jobID := created.Resource.ID
	if jobID == "" {
		t.Fatal("EvalHub accepted the benchmark job but returned no resource.id")
	}
	t.Logf("submitted disconnected benchmark job %s (collection=%s provider=%s benchmark=%s occurrence=%d)", jobID, testCase.collectionID, testCase.providerID, testCase.benchmarkID, testCase.occurrence)
	jobFinished := false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if !jobFinished {
			_ = connection.client.WithContext(cleanupCtx).CancelJob(jobID)
		}
		if err := deleteEvaluationKubernetesJobAndWait(cleanupCtx, kube, instance.namespace, jobID); err != nil {
			t.Errorf("clean up Kubernetes resources for EvalHub job %s: %v", jobID, err)
		}
	}()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		job, err := jobClient.GetJob(jobID)
		if err != nil {
			t.Fatalf("get EvalHub job %s: %v", jobID, err)
		}
		if job.Status != nil && job.Status.State.IsTerminalState() {
			jobFinished = true
			pod, podErr := findEvaluationAdapterPod(parentCtx, kube, instance.namespace, jobID)
			image, imageID := adapterImageDetails(pod)
			if manifest.managed {
				logDeployedAdapter(t, manifest, adapterIDForProvider(testCase.providerID))
				if podErr != nil {
					t.Fatalf("read adapter pod image provenance: %v", podErr)
				}
				if pod == nil {
					t.Fatal("completed benchmark has no observable adapter pod; cannot verify the deployed image")
				}
				if image != providerImageForCase(manifest, testCase) {
					t.Fatalf("adapter pod used image %q, deployment manifest records %q", image, providerImageForCase(manifest, testCase))
				}
				if imageID == "" {
					t.Fatalf("completed adapter pod has no resolved imageID: %+v", pod.Status.ContainerStatuses)
				}
			}
			t.Logf("adapter runtime: provider=%s image=%s imageID=%s", testCase.providerID, image, imageID)
			if podErr != nil {
				t.Logf("could not read adapter pod image provenance: %v", podErr)
			}
			if job.Status.State != api.OverallStateCompleted {
				logs := getBenchmarkLogs(parentCtx, connection, jobID, 40)
				t.Fatalf("disconnected job %s ended in state %q: %v\nadapter log tail:\n%s", jobID, job.Status.State, job.Status.Message, logs)
			}
			if len(job.Status.Benchmarks) != 1 {
				t.Fatalf("job %s completed with %d benchmark statuses, want 1: %+v", jobID, len(job.Status.Benchmarks), job.Status.Benchmarks)
			}
			status := job.Status.Benchmarks[0]
			if status.ProviderID != testCase.providerID || status.ID != testCase.benchmarkID || status.Status != api.StateCompleted {
				logs := getBenchmarkLogs(parentCtx, connection, jobID, 40)
				t.Fatalf("benchmark status is not completed: %+v\nadapter log tail:\n%s", status, logs)
			}
			return
		}
		select {
		case <-jobCtx.Done():
			logs := getBenchmarkLogs(parentCtx, connection, jobID, 40)
			t.Fatalf("timed out waiting for disconnected job %s: %v\nadapter log tail:\n%s", jobID, jobCtx.Err(), logs)
		case <-ticker.C:
		}
	}
}

func providerImageForCase(manifest deploymentManifest, testCase curatedBenchmarkCase) string {
	adapterID := adapterIDForProvider(testCase.providerID)
	return manifest.selectedAdapters[adapterID].image
}

func mustRandomResourceSuffix(t *testing.T) string {
	t.Helper()
	suffix, err := randomResourceSuffix()
	if err != nil {
		t.Fatalf("generate unique resource suffix: %v", err)
	}
	return suffix
}
