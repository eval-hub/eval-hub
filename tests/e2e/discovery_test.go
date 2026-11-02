package e2e

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestInspectAirgapPolicies(t *testing.T) {
	jobSelector := metav1.LabelSelector{MatchLabels: map[string]string{
		"app":       "evalhub",
		"component": "evaluation-job",
	}}
	tests := []struct {
		name       string
		policies   []networkingv1.NetworkPolicy
		wantActive bool
	}{
		{
			name: "no applicable policy",
			policies: []networkingv1.NetworkPolicy{{
				ObjectMeta: metav1.ObjectMeta{Name: "api-ingress"},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "eval-hub"}},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				},
			}},
		},
		{
			name: "egress deny all",
			policies: []networkingv1.NetworkPolicy{{
				ObjectMeta: metav1.ObjectMeta{Name: "deny-evaluation-egress"},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: jobSelector,
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				},
			}},
			wantActive: true,
		},
		{
			name: "namespace and DNS allowlists",
			policies: []networkingv1.NetworkPolicy{{
				ObjectMeta: metav1.ObjectMeta{Name: "evalhub-dev-airgap-egress"},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: jobSelector,
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress: []networkingv1.NetworkPolicyEgressRule{{
						To: []networkingv1.NetworkPolicyPeer{{
							NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "system"}},
						}},
					}},
				},
			}},
			wantActive: true,
		},
		{
			name: "unrestricted egress rule",
			policies: []networkingv1.NetworkPolicy{{
				ObjectMeta: metav1.ObjectMeta{Name: "allow-all"},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: jobSelector,
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress:      []networkingv1.NetworkPolicyEgressRule{{}},
				},
			}},
		},
		{
			name: "IP block egress",
			policies: []networkingv1.NetworkPolicy{{
				ObjectMeta: metav1.ObjectMeta{Name: "allow-ip"},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: jobSelector,
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress: []networkingv1.NetworkPolicyEgressRule{{
						To: []networkingv1.NetworkPolicyPeer{{
							IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"},
						}},
					}},
				},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got, _ := inspectAirgapPolicies(test.policies)
			if got != test.wantActive {
				t.Fatalf("inspectAirgapPolicies() active=%t, want %t", got, test.wantActive)
			}
		})
	}
}

func TestDiscoverEvalHubInstancesPrefersClusterIPService(t *testing.T) {
	podLabels := map[string]string{"app": "eval-hub", "component": "api", "instance": "evalhub-local"}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "evalhub-local-api-0", Namespace: "system", Labels: podLabels},
		Spec:       corev1.PodSpec{ServiceAccountName: "evalhub-local-service"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue,
			}},
		},
	}
	clusterIP := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "evalhub-local", Namespace: "system"},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "eval-hub", "component": "api", "instance": "evalhub-local"},
			Ports:    []corev1.ServicePort{{Name: "https", Port: 8443, Protocol: corev1.ProtocolTCP}},
		},
	}
	nodePort := clusterIP.DeepCopy()
	nodePort.Name = "evalhub-local-nodeport"
	nodePort.Spec.Type = corev1.ServiceTypeNodePort

	kube := fake.NewSimpleClientset(pod, clusterIP, nodePort)
	instances, err := discoverEvalHubInstances(context.Background(), kube)
	if err != nil {
		t.Fatalf("discoverEvalHubInstances() error = %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("discovered %d instances, want 1: %+v", len(instances), instances)
	}
	got := instances[0]
	if got.namespace != "system" || got.serviceName != "evalhub-local" || got.servicePort != 8443 {
		t.Fatalf("discovered instance = %+v, want system/evalhub-local:8443", got)
	}
	if got.serviceAccount != "evalhub-local-service" {
		t.Fatalf("service account = %q, want evalhub-local-service", got.serviceAccount)
	}
}

func TestDiscoverBFCLAssetsFromPVCLabel(t *testing.T) {
	client := collectionAPIClient(`{"items":[],"limit":100,"total_count":0}`)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bfcl-cache",
			Namespace: "system",
			Labels:    map[string]string{bfclAssetLabel: "true"},
			Annotations: map[string]string{
				bfclSubPathAnnotation: "staged",
			},
		},
	}
	kube := fake.NewSimpleClientset(pvc)
	assets, err := discoverBFCLAssets(context.Background(), kube, client, "system")
	if err != nil {
		t.Fatalf("discoverBFCLAssets() error = %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("discovered %d assets, want 1", len(assets))
	}
	asset := assets[0]
	if asset.claimName != "bfcl-cache" || asset.subPath != "staged" || asset.ref == nil ||
		asset.ref.PVC == nil || asset.ref.PVC.ClaimName != "bfcl-cache" {
		t.Fatalf("discovered asset = %+v, want PVC bfcl-cache with subPath staged", asset)
	}
}

func TestDiscoverBFCLAssetsFromCollectionConfig(t *testing.T) {
	client := collectionAPIClient(`{"items":[{"resource":{"id":"tool-use-v1"},"benchmarks":[{"id":"inspect/bfcl","provider_id":"inspect","test_data_ref":{"pvc":{"claim_name":"bfcl-cache","sub_path":"staged"}}}]}],"limit":100,"total_count":1}`)

	kube := fake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "bfcl-cache", Namespace: "system"},
	})
	assets, err := discoverBFCLAssets(context.Background(), kube, client, "system")
	if err != nil {
		t.Fatalf("discoverBFCLAssets() error = %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("discovered %d assets, want 1", len(assets))
	}
	asset := assets[0]
	if asset.claimName != "bfcl-cache" || asset.subPath != "staged" || asset.source != "collection tool-use-v1" {
		t.Fatalf("discovered asset = %+v, want collection tool-use-v1 PVC bfcl-cache/staged", asset)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func collectionAPIClient(responseBody string) *evalhubclient.Client {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(responseBody)),
				Request:    request,
			}, nil
		}),
	}
	return evalhubclient.NewClient("http://evalhub.test").WithHTTPClient(httpClient).WithMaxRetries(0)
}

func TestWriteBFCLArchive(t *testing.T) {
	dataDir := t.TempDir()
	files := map[string]string{
		"BFCL_v4_simple_python.json":                 "{\"question\": \"example\"}\n",
		"possible_answer/BFCL_v4_simple_python.json": "{\"answer\": \"example\"}\n",
		"func_source_code/example.py":                "def example():\n    return 1\n",
	}
	for relative, contents := range files {
		filePath := filepath.Join(dataDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatalf("create source directory for %s: %v", relative, err)
		}
		if err := os.WriteFile(filePath, []byte(contents), 0o644); err != nil {
			t.Fatalf("write source file %s: %v", relative, err)
		}
	}

	var archiveBytes bytes.Buffer
	if err := writeBFCLArchive(&archiveBytes, dataDir); err != nil {
		t.Fatalf("writeBFCLArchive() error = %v", err)
	}

	archive := tar.NewReader(&archiveBytes)
	got := make(map[string]string, len(files))
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read archive entry: %v", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		contents, err := io.ReadAll(archive)
		if err != nil {
			t.Fatalf("read archive file %s: %v", header.Name, err)
		}
		got[header.Name] = string(contents)
	}

	if len(got) != len(files) {
		t.Fatalf("archive contains %d files, want %d: %v", len(got), len(files), got)
	}
	for relative, want := range files {
		if value, ok := got["BFCL/"+filepath.ToSlash(relative)]; !ok || value != want {
			t.Errorf("archive entry BFCL/%s = %q, present=%t; want %q", relative, value, ok, want)
		}
	}
}

func TestFindLMESToxiGenProvider(t *testing.T) {
	client := collectionAPIClient(`{"items":[{"resource":{"id":"lm_evaluation_harness"},"runtime":{"k8s":{"image":"registry.example/lmes:latest"}},"benchmarks":[{"id":"toxigen"}]}],"limit":100,"total_count":1}`)

	provider, err := findLMESToxiGenProvider(context.Background(), client)
	if err != nil {
		t.Fatalf("findLMESToxiGenProvider() error = %v", err)
	}
	if provider == nil || provider.Resource.ID != lmesProviderID ||
		provider.Runtime == nil || provider.Runtime.K8s == nil ||
		provider.Runtime.K8s.Image != "registry.example/lmes:latest" {
		t.Fatalf("provider = %+v, want LMES provider with Kubernetes image", provider)
	}
}

func TestFindLMESToxiGenProviderSkipsProviderWithoutTask(t *testing.T) {
	client := collectionAPIClient(`{"items":[{"resource":{"id":"lm_evaluation_harness"},"runtime":{"k8s":{"image":"registry.example/lmes:latest"}},"benchmarks":[{"id":"ethics_cm"}]}],"limit":100,"total_count":1}`)

	provider, err := findLMESToxiGenProvider(context.Background(), client)
	if err != nil {
		t.Fatalf("findLMESToxiGenProvider() error = %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %+v, want nil when ToxiGen is not listed", provider)
	}
}

func TestToxiGenEvaluationConfigReferencesOfflineAssets(t *testing.T) {
	config := toxigenEvaluationConfig(
		"target-secret",
		"target",
		"system",
		8000,
		"toxigen-assets",
	)
	if config.Model == nil || config.Model.Auth == nil ||
		config.Model.Auth.SecretRef != "target-secret" ||
		config.Model.URL != "http://target.system.svc.cluster.local:8000/v1" {
		t.Fatalf("model configuration = %+v", config.Model)
	}
	if len(config.Benchmarks) != 1 {
		t.Fatalf("job has %d benchmarks, want 1", len(config.Benchmarks))
	}
	benchmark := config.Benchmarks[0]
	if benchmark.ID != toxigenBenchmarkID || benchmark.ProviderID != lmesProviderID {
		t.Fatalf("benchmark = %+v, want LMES ToxiGen", benchmark)
	}
	if benchmark.Parameters["num_examples"] != 1 ||
		benchmark.Parameters["num_few_shot"] != 0 ||
		benchmark.Parameters["tokenizer"] != "/test_data/tokenizer" {
		t.Fatalf("benchmark parameters = %+v", benchmark.Parameters)
	}
	if benchmark.TestDataRef == nil || benchmark.TestDataRef.PVC == nil ||
		benchmark.TestDataRef.PVC.ClaimName != "toxigen-assets" {
		t.Fatalf("test_data_ref = %+v, want PVC toxigen-assets", benchmark.TestDataRef)
	}
}

func TestToxiGenAssetPreparationProgramUsesOfflineDatasetAndTokenizer(t *testing.T) {
	program := toxigenAssetPreparationProgram()
	for _, required := range []string{
		"skg/toxigen-data",
		"annotated",
		"skg--toxigen-data--annotated",
		"HF_DATASETS_OFFLINE",
		"load_from_disk(dataset_bundle)",
		"dataset.save_to_disk(dataset_bundle)",
		"AutoTokenizer.from_pretrained",
		"TOXIGEN_ASSETS_READY",
	} {
		if !strings.Contains(program, required) {
			t.Errorf("staging program does not contain %q", required)
		}
	}
}

func TestAdapterImageDetails(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "sidecar", Image: "sidecar:latest"},
			{Name: "adapter", Image: "adapter:v1"},
		}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "sidecar", ImageID: "sidecar@sha256:abc"},
			{Name: "adapter", ImageID: "adapter@sha256:def"},
		}},
	}
	image, imageID := adapterImageDetails(pod)
	if image != "adapter:v1" || imageID != "adapter@sha256:def" {
		t.Fatalf("adapterImageDetails() = (%q, %q), want adapter image and image ID", image, imageID)
	}
}

func TestDeleteEvaluationKubernetesJobAndWait(t *testing.T) {
	jobID := "lmes-job-id"
	selectorLabels := map[string]string{
		"app":       "evalhub",
		"component": "evaluation-job",
		"job_id":    jobID,
	}
	unrelatedLabels := map[string]string{
		"app":       "evalhub",
		"component": "evaluation-job",
		"job_id":    "another-job-id",
	}
	kube := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "lmes-job", Namespace: "system", Labels: selectorLabels}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "lmes-pod", Namespace: "system", Labels: selectorLabels}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "other-job", Namespace: "system", Labels: unrelatedLabels}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "system", Labels: unrelatedLabels}},
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := deleteEvaluationKubernetesJobAndWait(ctx, kube, "system", jobID); err != nil {
		t.Fatalf("deleteEvaluationKubernetesJobAndWait() error = %v", err)
	}

	jobs, err := kube.BatchV1().Jobs("system").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 1 || jobs.Items[0].Name != "other-job" {
		t.Fatalf("remaining Jobs = %+v, want only other-job", jobs.Items)
	}
	pods, err := kube.CoreV1().Pods("system").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list Pods: %v", err)
	}
	if len(pods.Items) != 1 || pods.Items[0].Name != "other-pod" {
		t.Fatalf("remaining Pods = %+v, want only other-pod", pods.Items)
	}
}
