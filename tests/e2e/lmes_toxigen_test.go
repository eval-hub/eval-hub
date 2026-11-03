package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

const (
	lmesProviderID       = "lm_evaluation_harness"
	toxigenBenchmarkID   = "toxigen"
	toxigenTokenizerRepo = "sshleifer/tiny-gpt2"
	toxigenPVCNamePrefix = "evalhub-e2e-lmes-toxigen-"
	toxigenPodNamePrefix = "evalhub-e2e-lmes-stager-"
)

func TestLMESToxiGenAirgap(t *testing.T) {
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
			runLMESToxiGenAirgapCase(t, ctx, contextName, kube, instance)
		})
	}
}

func runLMESToxiGenAirgapCase(
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

	provider, err := findLMESToxiGenProvider(parentCtx, connection.client)
	if err != nil {
		t.Fatalf("read providers from EvalHub API in namespace %q: %v", instance.namespace, err)
	}
	if provider == nil || provider.Runtime == nil || provider.Runtime.K8s == nil || provider.Runtime.K8s.Image == "" {
		t.Skipf("EvalHub %s/%s has no Kubernetes LMES provider exposing %q; this test does not apply", instance.namespace, instance.serviceName, toxigenBenchmarkID)
	}
	providerImage := provider.Runtime.K8s.Image

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

	stageCtx, cancelStage := context.WithTimeout(parentCtx, 20*time.Minute)
	claimName, cleanupAssets, err := stageLMESToxiGenAssets(
		stageCtx, kube, instance, providerImage, provider.Runtime.K8s.ImagePullPolicy,
	)
	cancelStage()
	if err != nil {
		t.Fatalf("automatically stage ToxiGen dataset and tokenizer for the disconnected test: %v", err)
	}
	defer cleanupAssets()
	t.Logf("staged the ToxiGen dataset cache and tokenizer on temporary PVC %s/%s", instance.namespace, claimName)

	secretName, err := randomResourceSuffix()
	if err != nil {
		t.Fatalf("generate a unique name for the temporary mock credential Secret: %v", err)
	}
	secretName = "eh-e2e-lmes-" + secretName
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: instance.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "evalhub-test-e2e",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"api-key": []byte(mockTargetAPIKey)},
	}
	if _, err := kube.CoreV1().Secrets(instance.namespace).Create(parentCtx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create temporary mock credential Secret %s/%s: %v", instance.namespace, secretName, err)
	}

	jobCtx, cancelJob := context.WithTimeout(parentCtx, jobTimeout)
	defer cancelJob()
	jobClient := connection.client.WithContext(jobCtx)
	jobID := ""
	jobFinished := false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if jobID != "" {
			if !jobFinished {
				_ = connection.client.WithContext(cleanupCtx).CancelJob(jobID)
			}
			if err := deleteEvaluationKubernetesJobAndWait(cleanupCtx, kube, instance.namespace, jobID); err != nil {
				t.Logf("could not remove Kubernetes Job and Pods for EvalHub job %s; temporary PVC cleanup may be delayed: %v", jobID, err)
			}
		}
		_ = kube.CoreV1().Secrets(instance.namespace).Delete(cleanupCtx, secretName, metav1.DeleteOptions{})
	}()

	jobConfig := toxigenEvaluationConfig(secretName, targetService.Name, instance.namespace, targetPort, claimName)
	created, err := jobClient.CreateJob(jobConfig)
	if err != nil {
		t.Fatalf("submit one-example disconnected ToxiGen evaluation: %v", err)
	}
	jobID = created.Resource.ID
	if jobID == "" {
		t.Fatal("EvalHub accepted the ToxiGen job but returned no resource.id")
	}
	t.Logf("submitted disconnected ToxiGen e2e job %s; its EvalHub record is retained for inspection", jobID)

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
				logText := getBenchmarkLogs(parentCtx, connection, jobID, 100)
				pod, _, podErr := findLMESToxiGenJobPod(parentCtx, kube, instance.namespace, jobID)
				adapterImage, adapterImageID := adapterImageDetails(pod)
				t.Fatalf("disconnected ToxiGen job %s finished in state %q; message=%v\nadapter image=%q imageID=%q pod inspection error=%v\nadapter log tail:\n%s",
					jobID, job.Status.State, job.Status.Message, adapterImage, adapterImageID, podErr, logText)
			}
			if len(job.Status.Benchmarks) != 1 {
				t.Fatalf("disconnected ToxiGen job %s completed with %d benchmark statuses, want 1: %+v",
					jobID, len(job.Status.Benchmarks), job.Status.Benchmarks)
			}
			status := job.Status.Benchmarks[0]
			if status.ProviderID != lmesProviderID || status.ID != toxigenBenchmarkID || status.Status != api.StateCompleted {
				logText := getBenchmarkLogs(parentCtx, connection, jobID, 100)
				t.Fatalf("ToxiGen benchmark status is not completed: %+v\nadapter log tail:\n%s", status, logText)
			}

			pod, adapterLogs, err := findLMESToxiGenJobPod(parentCtx, kube, instance.namespace, jobID)
			if err != nil {
				t.Fatalf("inspect completed ToxiGen evaluation pod: %v", err)
			}
			if pod == nil {
				t.Fatal("ToxiGen job completed but its evaluation pod could not be found")
			}
			adapterImage, adapterImageID := adapterImageDetails(pod)
			if adapterImage != providerImage {
				t.Fatalf("ToxiGen pod used adapter image %q, provider config specifies %q", adapterImage, providerImage)
			}
			if !strings.Contains(adapterLogs, "HF offline mode (auto-detected from staged tokenizer + dataset under /test_data)") {
				t.Fatalf("ToxiGen job completed without confirming LMES offline mode in adapter logs:\n%s", adapterLogs)
			}
			if !strings.Contains(adapterLogs, "Offline: loading DatasetDict bundle at /test_data/skg--toxigen-data--annotated") {
				t.Fatalf("ToxiGen job completed without confirming it loaded the staged offline DatasetDict bundle:\n%s", adapterLogs)
			}
			if adapterImageID == "" {
				t.Fatalf("completed ToxiGen pod has no adapter imageID: %+v", pod.Status.ContainerStatuses)
			}
			t.Logf("LMES adapter image: %s (%s)", providerImage, adapterImageID)
			t.Log("ToxiGen benchmark completed successfully (score is not asserted; this verifies staged asset consumption and offline execution)")
			return
		}

		select {
		case <-jobCtx.Done():
			logText := getBenchmarkLogs(parentCtx, connection, jobID, 100)
			t.Fatalf("disconnected ToxiGen job %s did not finish within %s: %v\nadapter log tail:\n%s",
				jobID, jobTimeout, jobCtx.Err(), logText)
		case <-poll.C:
		}
	}
}

func findLMESToxiGenProvider(ctx context.Context, client *evalhubclient.Client) (*api.ProviderResource, error) {
	for offset := 0; ; {
		page, err := client.ListProviders(evalhubclient.WithLimit(100), evalhubclient.WithOffset(offset))
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			provider := &page.Items[i]
			if provider.Resource.ID != lmesProviderID {
				continue
			}
			for _, benchmark := range provider.Benchmarks {
				if benchmark.ID == toxigenBenchmarkID {
					return provider, nil
				}
			}
			return nil, nil
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.TotalCount {
			return nil, nil
		}
	}
}

func toxigenEvaluationConfig(secretName, targetService, namespace string, targetPort int32, claimName string) api.EvaluationJobConfig {
	return api.EvaluationJobConfig{
		Name: "e2e-lmes-toxigen-" + secretName,
		Model: &api.ModelRef{
			URL:  fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/v1", targetService, namespace, targetPort),
			Name: mockTargetModelName,
			Auth: &api.ModelAuth{SecretRef: secretName},
		},
		Benchmarks: []api.EvaluationBenchmarkConfig{{
			Ref:        api.Ref{ID: toxigenBenchmarkID},
			ProviderID: lmesProviderID,
			Parameters: map[string]any{
				"num_examples": 1,
				"num_few_shot": 0,
				"tokenizer":    "/test_data/tokenizer",
			},
			TestDataRef: &api.TestDataRef{
				PVC: &api.PVCTestDataRef{ClaimName: claimName},
			},
		}},
	}
}

func stageLMESToxiGenAssets(
	ctx context.Context,
	kube kubernetes.Interface,
	instance evalHubInstance,
	image string,
	imagePullPolicy string,
) (claimName string, cleanup func(), retErr error) {
	suffix, err := randomResourceSuffix()
	if err != nil {
		return "", nil, fmt.Errorf("generate unique ToxiGen staging resource names: %w", err)
	}
	claimName = toxigenPVCNamePrefix + suffix
	podName := toxigenPodNamePrefix + suffix

	pvcCreated := false
	podCreated := false
	cleanupResources := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: instance.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "evalhub-test-e2e",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("3Gi")},
			},
		},
	}
	if _, err := kube.CoreV1().PersistentVolumeClaims(instance.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return "", nil, fmt.Errorf("create temporary ToxiGen PVC %s/%s: %w", instance.namespace, claimName, err)
	}
	pvcCreated = true

	automountServiceAccountToken := false
	runAsNonRoot := true
	pullPolicy := corev1.PullIfNotPresent
	if imagePullPolicy == "always" {
		pullPolicy = corev1.PullAlways
	}
	uid := int64(65532)
	activeDeadline := int64((20 * time.Minute).Seconds())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: instance.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "evalhub-test-e2e",
				"app":                          "evalhub-e2e-asset-stager",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:           instance.serviceAccount,
			AutomountServiceAccountToken: &automountServiceAccountToken,
			RestartPolicy:                corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:        &activeDeadline,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRoot,
				RunAsUser:    &uid,
				RunAsGroup:   &uid,
				FSGroup:      &uid,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{{
				Name:            "asset-stager",
				Image:           image,
				ImagePullPolicy: pullPolicy,
				Command:         []string{"/opt/app-root/bin/python", "-c", toxigenAssetPreparationProgram()},
				Env: []corev1.EnvVar{
					{Name: "HOME", Value: "/test_data/home"},
					{Name: "TMPDIR", Value: "/tmp"},
					{Name: "XDG_CACHE_HOME", Value: "/test_data/cache"},
					{Name: "HF_HOME", Value: "/test_data"},
					{Name: "HF_HUB_CACHE", Value: "/test_data/hub"},
					{Name: "HF_DATASETS_CACHE", Value: "/test_data/datasets"},
					{Name: "HF_HUB_OFFLINE", Value: "0"},
					{Name: "HF_DATASETS_OFFLINE", Value: "0"},
					{Name: "HF_EVALUATE_OFFLINE", Value: "0"},
					{Name: "TRANSFORMERS_OFFLINE", Value: "0"},
					{Name: "PYTHONDONTWRITEBYTECODE", Value: "1"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "toxigen-assets",
						MountPath: "/test_data",
					},
					{
						Name:      "tmp",
						MountPath: "/tmp",
					},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("250m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPointer(false),
					ReadOnlyRootFilesystem:   boolPointer(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "toxigen-assets",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName},
					},
				},
				{
					Name:         "tmp",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
			},
		},
	}
	if _, err := kube.CoreV1().Pods(instance.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", nil, fmt.Errorf("create ToxiGen asset staging pod %s/%s: %w", instance.namespace, podName, err)
	}
	podCreated = true

	stagerCtx, cancelStager := context.WithTimeout(ctx, 20*time.Minute)
	defer cancelStager()
	pod, logs, err := waitForStagerCompletion(stagerCtx, kube, instance.namespace, podName)
	if err != nil {
		return "", nil, fmt.Errorf("ToxiGen asset staging pod failed: %w\nstager logs:\n%s", err, logs)
	}
	if pod == nil || pod.Status.Phase != corev1.PodSucceeded {
		return "", nil, fmt.Errorf("ToxiGen asset staging pod did not complete successfully (pod=%+v)\nstager logs:\n%s", pod, logs)
	}
	if !strings.Contains(logs, "TOXIGEN_ASSETS_READY") {
		return "", nil, fmt.Errorf("ToxiGen stager completed without its readiness marker\nstager logs:\n%s", logs)
	}
	if err := deletePodAndWait(ctx, kube, instance.namespace, podName); err != nil {
		return "", nil, fmt.Errorf("delete completed ToxiGen stager pod %s/%s: %w", instance.namespace, podName, err)
	}
	podCreated = false
	if err := waitForPVCBound(ctx, kube, instance.namespace, claimName); err != nil {
		return "", nil, fmt.Errorf("ToxiGen asset PVC was not bound: %w", err)
	}

	pvcForJob, err := kube.CoreV1().PersistentVolumeClaims(instance.namespace).Get(ctx, claimName, metav1.GetOptions{})
	if err != nil {
		return "", nil, fmt.Errorf("verify staged ToxiGen PVC %s/%s: %w", instance.namespace, claimName, err)
	}
	if pvcForJob.Status.Phase != corev1.ClaimBound {
		return "", nil, fmt.Errorf("ToxiGen PVC %s/%s is %q, want Bound", instance.namespace, claimName, pvcForJob.Status.Phase)
	}

	cleanup = func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = kube.CoreV1().PersistentVolumeClaims(instance.namespace).Delete(cleanupCtx, claimName, metav1.DeleteOptions{})
	}
	return claimName, cleanup, nil
}

func toxigenAssetPreparationProgram() string {
	program := strings.TrimSpace(`
import os
from datasets import load_dataset, load_from_disk
from huggingface_hub import snapshot_download
from transformers import AutoTokenizer

root = "/test_data"
dataset_cache = os.path.join(root, "datasets")
dataset_bundle = os.path.join(root, "skg--toxigen-data--annotated")
tokenizer_dir = os.path.join(root, "tokenizer")
os.makedirs(dataset_cache, exist_ok=True)
os.makedirs(os.path.dirname(tokenizer_dir), exist_ok=True)

print("Downloading and preparing skg/toxigen-data (annotated) in the LMES image cache", flush=True)
dataset = load_dataset("skg/toxigen-data", "annotated", cache_dir=dataset_cache)
if "test" not in dataset or len(dataset["test"]) == 0:
    raise RuntimeError("ToxiGen dataset did not provide a non-empty test split")

# The LMES ToxiGen task resolves this exact HF_HOME bundle path when offline.
# Save every split so the staged DatasetDict matches the task's normal dataset shape.
dataset.save_to_disk(dataset_bundle)

print("Downloading the public smoke-test tokenizer", flush=True)
snapshot_download(repo_id="TOXIGEN_TOKENIZER_REPO", local_dir=tokenizer_dir)
AutoTokenizer.from_pretrained(tokenizer_dir, local_files_only=True)

# Prove the staged DatasetDict and tokenizer are usable from local disk before
# attaching them to an evaluation job whose pod is egress-restricted.
for key in ("HF_HUB_OFFLINE", "HF_DATASETS_OFFLINE", "HF_EVALUATE_OFFLINE", "TRANSFORMERS_OFFLINE"):
    os.environ[key] = "1"
offline_dataset = load_from_disk(dataset_bundle)
if "test" not in offline_dataset or len(offline_dataset["test"]) == 0:
    raise RuntimeError("ToxiGen DatasetDict bundle could not be reopened from disk")
AutoTokenizer.from_pretrained(tokenizer_dir, local_files_only=True)
print("TOXIGEN_ASSETS_READY", flush=True)
`)
	return strings.ReplaceAll(program, "TOXIGEN_TOKENIZER_REPO", toxigenTokenizerRepo)
}

func waitForStagerCompletion(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
	podName string,
) (*corev1.Pod, string, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		pod, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return nil, "", fmt.Errorf("get ToxiGen stager pod %s/%s: %w", namespace, podName, err)
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			logs, logErr := readPodLogs(ctx, kube, namespace, podName, "asset-stager", 1000)
			if logErr != nil {
				return pod, "", fmt.Errorf("read successful ToxiGen stager logs: %w", logErr)
			}
			return pod, logs, nil
		case corev1.PodFailed:
			logs, _ := readPodLogs(ctx, kube, namespace, podName, "asset-stager", 1000)
			return pod, logs, fmt.Errorf("pod phase is Failed: %s", pod.Status.Message)
		}
		select {
		case <-ctx.Done():
			logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			logs, _ := readPodLogs(logCtx, kube, namespace, podName, "asset-stager", 1000)
			cancel()
			return pod, logs, fmt.Errorf("waiting for staging pod completion: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func deleteEvaluationKubernetesJobAndWait(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
	jobID string,
) error {
	selector := "app=evalhub,component=evaluation-job,job_id=" + jobID
	propagation := metav1.DeletePropagationForeground
	jobs, err := kube.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("list Kubernetes Jobs for EvalHub job %s: %w", jobID, err)
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if err := kube.BatchV1().Jobs(namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete Kubernetes Job/%s for EvalHub job %s: %w", job.Name, jobID, err)
		}
	}

	// Explicitly delete matching Pods too, so the PVC can be released promptly
	// while foreground garbage collection removes the owning Job.
	pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("list evaluation Pods for EvalHub job %s: %w", jobID, err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if err := kube.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete evaluation Pod/%s for EvalHub job %s: %w", pod.Name, jobID, err)
		}
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		jobs, err := kube.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return fmt.Errorf("check Kubernetes Job cleanup for EvalHub job %s: %w", jobID, err)
		}
		pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return fmt.Errorf("check evaluation Pod cleanup for EvalHub job %s: %w", jobID, err)
		}
		if len(jobs.Items) == 0 && len(pods.Items) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Kubernetes Job/Pod cleanup for EvalHub job %s did not finish: %d Jobs and %d Pods remain: %w",
				jobID, len(jobs.Items), len(pods.Items), ctx.Err())
		case <-ticker.C:
		}
	}
}

func readPodLogs(ctx context.Context, kube kubernetes.Interface, namespace, podName, containerName string, tailLines int64) (string, error) {
	tail := tailLines
	request := kube.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tail,
	})
	body, err := request.Do(ctx).Raw()
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func findLMESToxiGenJobPod(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
	jobID string,
) (*corev1.Pod, string, error) {
	pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job_id=" + jobID,
	})
	if err != nil {
		return nil, "", err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, container := range pod.Spec.Containers {
			if container.Name != "adapter" {
				continue
			}
			logs, logErr := readPodLogs(ctx, kube, namespace, pod.Name, "adapter", 1000)
			if logErr != nil {
				return pod, "", fmt.Errorf("read adapter logs from Pod/%s: %w", pod.Name, logErr)
			}
			return pod, logs, nil
		}
	}
	return nil, "", nil
}

func adapterImageDetails(pod *corev1.Pod) (image, imageID string) {
	if pod == nil {
		return "", ""
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == "adapter" {
			image = container.Image
			break
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "adapter" {
			imageID = status.ImageID
			break
		}
	}
	return image, imageID
}

func boolPointer(value bool) *bool {
	return &value
}
