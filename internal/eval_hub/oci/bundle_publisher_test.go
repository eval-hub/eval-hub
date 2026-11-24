package oci

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var cardRef = api.OCIArtifactReference{OCIDigest: digestOf([]byte("card")), OCIReference: "untrusted.example/other:tag"}
var benchmarkDigest = digestOf([]byte("benchmark"))

func completedJob() *api.EvaluationJobResource {
	job := &api.EvaluationJobResource{
		Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}, Benchmarks: []api.BenchmarkStatus{
			{BenchmarkIndex: 0, Status: api.StateCompleted}, {BenchmarkIndex: 2, Status: api.StateCompleted},
		}},
		Results: &api.EvaluationJobResults{Benchmarks: []api.BenchmarkResult{
			{BenchmarkIndex: 2, ID: "same-benchmark", ProviderID: "provider", Artifacts: map[string]any{"oci_digest": benchmarkDigest, "oci_reference": "https://callback.invalid/private:tag"}},
			{BenchmarkIndex: 0, ID: "same-benchmark", ProviderID: "provider", Artifacts: map[string]any{"oci_digest": benchmarkDigest}},
		}},
	}
	job.Resource.ID = "job-id"
	job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: "http://localhost:5000", OCIRepository: "repo"}}}
	return job
}

type registryMock struct {
	gets       []string
	puts       int
	index      ociclient.Index
	tag        string
	failDigest string
	err        error
}

func (r *registryMock) GetManifest(_ context.Context, digest string) (ociclient.Descriptor, error) {
	r.gets = append(r.gets, digest)
	if digest == r.failDigest {
		return ociclient.Descriptor{}, r.err
	}
	return ociclient.Descriptor{MediaType: ociclient.MediaTypeImageManifest, Size: 123, Digest: digest}, nil
}
func (r *registryMock) PushIndex(_ context.Context, tag string, index ociclient.Index) (*api.OCIArtifactReference, error) {
	r.puts++
	r.tag, r.index = tag, index
	if r.failDigest == "put" {
		return nil, r.err
	}
	body, err := json.Marshal(index)
	if err != nil {
		return nil, err
	}
	digest := digestOf(body)
	return &api.OCIArtifactReference{OCIDigest: digest, OCIReference: "localhost:5000/repo:" + tag + "@" + digest}, nil
}

func TestBundlePublisherDeterministicSelectionAndAnnotations(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStateCompleted, api.OverallStatePartiallyFailed} {
		for _, collection := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/collection=%t", state, collection), func(t *testing.T) {
				job := completedJob()
				job.Status.State = state
				if collection {
					job.Collection = &api.CollectionRef{}
				}
				// Stored membership is authoritative even without submitted run definitions.
				if state == api.OverallStatePartiallyFailed {
					job.Status.Benchmarks = append(job.Status.Benchmarks, api.BenchmarkStatus{BenchmarkIndex: 1, Status: api.StateFailed}, api.BenchmarkStatus{BenchmarkIndex: 3, Status: api.StateCancelled}, api.BenchmarkStatus{BenchmarkIndex: 4, Status: api.StateFailed})
					job.Results.Benchmarks = append(job.Results.Benchmarks, api.BenchmarkResult{BenchmarkIndex: 1, Artifacts: map[string]any{"oci_digest": "never-fetch-failed"}}, api.BenchmarkResult{BenchmarkIndex: 3, Artifacts: map[string]any{"oci_digest": "never-fetch-cancelled"}})
				}
				job.Exports.OCI.Coordinates.Annotations = map[string]string{"evaluation_job_id": "spoof", "evaluation_state": "spoof", "included_benchmark_count": "spoof", "skipped_benchmark_count": "spoof", "custom": "retained"}
				before, err := json.Marshal(job)
				if err != nil {
					t.Fatal(err)
				}
				registry := &registryMock{}
				first, err := NewBundlePublisher(registry).Publish(context.Background(), job, &cardRef)
				if err != nil {
					t.Fatal(err)
				}
				after, _ := json.Marshal(job)
				if !bytes.Equal(before, after) {
					t.Fatal("stored job mutated")
				}
				if registry.puts != 1 || !reflect.DeepEqual(registry.gets, []string{cardRef.OCIDigest, benchmarkDigest, benchmarkDigest}) {
					t.Fatalf("incorrect operations: %+v", registry)
				}
				index := registry.index
				if index.SchemaVersion != 2 || index.MediaType != ociclient.MediaTypeImageIndex || len(index.Manifests) != 3 {
					t.Fatalf("incorrect index: %+v", index)
				}
				if index.Manifests[0].Annotations["artifact_role"] != "evaluation-card" || index.Manifests[0].Digest != cardRef.OCIDigest {
					t.Fatal("card must be first and identified")
				}
				for i, original := range []string{"0", "2"} {
					d := index.Manifests[i+1]
					want := map[string]string{"artifact_role": "benchmark", "benchmark_index": original, "benchmark_id": "same-benchmark", "provider_id": "provider"}
					if d.Digest != benchmarkDigest || !reflect.DeepEqual(d.Annotations, want) {
						t.Fatalf("incorrect benchmark descriptor: %+v", d)
					}
				}
				skipped := "0"
				if state == api.OverallStatePartiallyFailed {
					skipped = "3"
				}
				want := map[string]string{"evaluation_job_id": "job-id", "evaluation_state": string(state), "included_benchmark_count": "2", "skipped_benchmark_count": skipped, "custom": "retained"}
				if !reflect.DeepEqual(index.Annotations, want) {
					t.Fatalf("incorrect annotations: %v", index.Annotations)
				}
				firstBytes, _ := json.Marshal(index)
				slices.Reverse(job.Results.Benchmarks)
				slices.Reverse(job.Status.Benchmarks)
				second, err := NewBundlePublisher(registry).Publish(context.Background(), job, &cardRef)
				if err != nil {
					t.Fatal(err)
				}
				secondBytes, _ := json.Marshal(registry.index)
				if !bytes.Equal(firstBytes, secondBytes) || !reflect.DeepEqual(first, second) {
					t.Fatal("callback order changed publication bytes or identity")
				}
			})
		}
	}
}

func TestBundlePublisherIneligibleJobs(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStatePending, api.OverallStateRunning, api.OverallStateFailed, api.OverallStateCancelled} {
		job := completedJob()
		job.Status.State = state
		registry := &registryMock{}
		ref, err := NewBundlePublisher(registry).Publish(context.Background(), job, nil)
		if err != nil || ref != nil || len(registry.gets) != 0 || registry.puts != 0 {
			t.Fatalf("ineligible %s published: %v %v", state, ref, err)
		}
	}
	for _, job := range []*api.EvaluationJobResource{nil, {}, {Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}}}} {
		if ref, err := NewBundlePublisher(nil).Publish(context.Background(), job, nil); ref != nil || err != nil {
			t.Fatalf("unconfigured job not skipped: %v %v", ref, err)
		}
	}
}

func TestBundlePublisherRejectsInvalidInputBeforeRegistryAccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*api.EvaluationJobResource)
		card   *api.OCIArtifactReference
	}{
		{name: "missing card"},
		{name: "invalid card", card: &api.OCIArtifactReference{OCIDigest: "sha256:bad"}},
		{name: "missing status", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Status.Benchmarks = nil }},
		{name: "running status", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Status.Benchmarks[1].Status = api.StateRunning }},
		{name: "pending status", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Status.Benchmarks[1].Status = api.StatePending }},
		{name: "unknown status", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Status.Benchmarks[1].Status = "unknown" }},
		{name: "missing artifacts", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Results.Benchmarks[0].Artifacts = nil }},
		{name: "nonstring digest", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Results.Benchmarks[0].Artifacts["oci_digest"] = 4 }},
		{name: "empty digest", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Results.Benchmarks[0].Artifacts["oci_digest"] = "" }},
		{name: "malformed digest", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Results.Benchmarks[0].Artifacts["oci_digest"] = "sha256:bad" }},
		{name: "bad prefix", card: &cardRef, mutate: func(j *api.EvaluationJobResource) {
			j.Exports.OCI.Coordinates.OCITag = strings.Repeat("a", 200) + "/bad"
		}},
		{name: "bad job ID", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Resource.ID = "../bad" }},
		{name: "empty job ID", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Resource.ID = "" }},
		{name: "oversized job ID", card: &cardRef, mutate: func(j *api.EvaluationJobResource) { j.Resource.ID = strings.Repeat("j", 129) }},
		{name: "no prefix room", card: &cardRef, mutate: func(j *api.EvaluationJobResource) {
			j.Resource.ID = strings.Repeat("j", 127)
			j.Exports.OCI.Coordinates.OCITag = "a"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := completedJob()
			if tc.mutate != nil {
				tc.mutate(job)
			}
			registry := &registryMock{}
			if _, err := NewBundlePublisher(registry).Publish(context.Background(), job, tc.card); err == nil {
				t.Fatal("invalid input accepted")
			}
			if registry.puts != 0 || len(registry.gets) != 0 {
				t.Fatal("registry accessed for invalid input")
			}
		})
	}
	if _, err := NewBundlePublisher(nil).Publish(context.Background(), completedJob(), &cardRef); err == nil {
		t.Fatal("nil registry accepted")
	}
}

func TestBundlePublisherPreservesWrappedErrors(t *testing.T) {
	laterDigest := digestOf([]byte("later benchmark"))
	for _, tc := range []struct {
		name, failed, operation string
		wantGets                []string
		wantPuts                int
	}{
		{name: "card", failed: cardRef.OCIDigest, operation: "verify evaluation card manifest", wantGets: []string{cardRef.OCIDigest}},
		{name: "first benchmark", failed: benchmarkDigest, operation: "verify benchmark 0 manifest", wantGets: []string{cardRef.OCIDigest, benchmarkDigest}},
		{name: "later benchmark", failed: laterDigest, operation: "verify benchmark 2 manifest", wantGets: []string{cardRef.OCIDigest, benchmarkDigest, laterDigest}},
		{name: "index publication", failed: "put", operation: "publish evaluation bundle", wantGets: []string{cardRef.OCIDigest, benchmarkDigest, laterDigest}, wantPuts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := completedJob()
			job.Results.Benchmarks[0].Artifacts["oci_digest"] = laterDigest
			cause := &ociclient.RegistryHTTPError{Operation: "auth request", StatusCode: 403, Message: "token denied"}
			registry := &registryMock{failDigest: tc.failed, err: cause}
			ref, err := NewBundlePublisher(registry).Publish(context.Background(), job, &cardRef)
			if ref != nil {
				t.Fatalf("returned reference after failure: %+v", ref)
			}
			var typed *ociclient.RegistryHTTPError
			if !errors.Is(err, cause) || !errors.As(err, &typed) || typed.StatusCode != 403 {
				t.Fatalf("lost typed error: %v", err)
			}
			var operationErr *OperationError
			if !errors.As(err, &operationErr) || operationErr.Operation != tc.operation || operationErr.Err != cause {
				t.Fatalf("lost operation and cause: %v", err)
			}
			if want := tc.operation + ": " + cause.Error(); err.Error() != want {
				t.Fatalf("error = %q, want %q", err.Error(), want)
			}
			if !slices.Equal(registry.gets, tc.wantGets) || registry.puts != tc.wantPuts {
				t.Fatalf("registry operations: gets=%v puts=%d, want gets=%v puts=%d", registry.gets, registry.puts, tc.wantGets, tc.wantPuts)
			}
		})
	}
}

func TestBundlePublisherFinalTags(t *testing.T) {
	for _, prefix := range []string{"", "custom", strings.Repeat("p", 200)} {
		job := completedJob()
		job.Exports.OCI.Coordinates.OCITag = prefix
		registry := &registryMock{}
		ref, err := NewBundlePublisher(registry).Publish(context.Background(), job, &cardRef)
		if err != nil {
			t.Fatal(err)
		}
		want, err := ociclient.EvaluationBundleManifestTag(job.Resource.ID, prefix)
		if err != nil {
			t.Fatal(err)
		}
		if registry.tag != want || !strings.Contains(ref.OCIReference, ":"+want+"@"+ref.OCIDigest) || !strings.HasSuffix(want, job.Resource.ID) || len(want) > 128 {
			t.Fatalf("incorrect published tag: %q %+v", registry.tag, ref)
		}
	}
}

func TestBundlePublisherUsesOnlyRecordedMembership(t *testing.T) {
	job := completedJob()
	job.Results = nil
	registry := &registryMock{}
	if _, err := NewBundlePublisher(registry).Publish(context.Background(), job, &cardRef); err != nil {
		t.Fatal(err)
	}
	if len(registry.index.Manifests) != 1 || registry.index.Annotations["included_benchmark_count"] != "0" {
		t.Fatal("independently reconciled requested runs")
	}
}

func TestBundlePublisherWithRegistryHTTPAndAuthenticatedTLS(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticatedTLS=%t", authenticated), func(t *testing.T) {
			config := `{"mediaType":"application/vnd.oci.empty.v1+json","size":2,"digest":"` + digestOf([]byte("{}")) + `"}`
			child := []byte(`{"schemaVersion":2,"mediaType":"` + ociclient.MediaTypeImageManifest + `","config":` + config + `,"layers":[]}`)
			childDigest := digestOf(child)
			var origin string
			var submitted []byte
			gets, puts, tokenCalls := 0, 0, 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					tokenCalls++
					user, pass, ok := r.BasicAuth()
					if !ok || user != "user" || pass != "password" || r.URL.Query().Get("scope") != "repository:configured/repo:pull,push" {
						t.Error("incorrect credential/token scope")
					}
					_, _ = io.WriteString(w, `{"token":"registry-token"}`)
					return
				}
				if authenticated && r.Header.Get("Authorization") != "Bearer registry-token" {
					w.Header().Set("WWW-Authenticate", `Bearer realm="`+origin+`/token"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/v2/configured/repo/manifests/"+childDigest {
					gets++
					w.Header().Set("Content-Type", ociclient.MediaTypeImageManifest)
					w.Header().Set("Docker-Content-Digest", childDigest)
					_, _ = w.Write(child)
					return
				}
				if r.Method == http.MethodPut && r.URL.Path == "/v2/configured/repo/manifests/evaluation-bundle-job-id" {
					puts++
					if r.Header.Get("Content-Type") != ociclient.MediaTypeImageIndex {
						t.Error("incorrect index content type")
					}
					var err error
					submitted, err = io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					w.Header().Set("Docker-Content-Digest", digestOf(submitted))
					w.WriteHeader(http.StatusCreated)
					return
				}
				t.Errorf("unexpected registry access: %s %s", r.Method, r.URL)
				w.WriteHeader(http.StatusNotFound)
			})
			var srv *httptest.Server
			if authenticated {
				srv = httptest.NewTLSServer(handler)
			} else {
				srv = httptest.NewServer(handler)
			}
			defer srv.Close()
			origin = srv.URL
			credentials := ociclient.Credentials{}
			if authenticated {
				credentials = ociclient.Credentials{Username: "user", Password: "password"}
			}
			client, err := ociclient.NewClient(origin, "configured/repo", credentials, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			job := completedJob()
			job.Exports.OCI.Coordinates.OCIHost = origin
			job.Exports.OCI.Coordinates.OCIRepository = "configured/repo"
			for i := range job.Results.Benchmarks {
				job.Results.Benchmarks[i].Artifacts["oci_digest"] = childDigest
			}
			card := api.OCIArtifactReference{OCIDigest: childDigest, OCIReference: "http://untrusted.invalid/other:tag"}
			ref, err := NewBundlePublisher(client).Publish(context.Background(), job, &card)
			if err != nil {
				t.Fatal(err)
			}
			if gets != 3 || puts != 1 || (authenticated && tokenCalls != 1) || (!authenticated && tokenCalls != 0) {
				t.Fatalf("incorrect operations: get=%d put=%d tokens=%d", gets, puts, tokenCalls)
			}
			if ref.OCIDigest != digestOf(submitted) || ref.OCIReference != strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")+"/configured/repo:evaluation-bundle-job-id@"+ref.OCIDigest {
				t.Fatalf("incorrect identity: %+v", ref)
			}
			var index ociclient.Index
			if err := json.Unmarshal(submitted, &index); err != nil {
				t.Fatal(err)
			}
			for _, d := range index.Manifests {
				if d.Size != int64(len(child)) || d.Digest != childDigest || d.MediaType != ociclient.MediaTypeImageManifest {
					t.Fatalf("incorrect verified descriptor: %+v", d)
				}
			}
		})
	}
}

// Detailed protocol failure cases belong to pkg/ociclient. Keep one real
// verification failure here to check that the publisher never publishes the index.
func TestBundlePublisherDigestMismatchNeverPutsIndex(t *testing.T) {
	child := []byte(`{"schemaVersion":2,"mediaType":"` + ociclient.MediaTypeImageManifest + `","config":{"mediaType":"application/vnd.oci.empty.v1+json","size":2,"digest":"` + digestOf([]byte("{}")) + `"},"layers":[]}`)
	cardDigest := digestOf(child)
	benchDigest := digestOf([]byte("different manifest"))
	var gets []string
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Method != http.MethodGet || (r.URL.Path != "/v2/repo/manifests/"+cardDigest && r.URL.Path != "/v2/repo/manifests/"+benchDigest) {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gets = append(gets, strings.TrimPrefix(r.URL.Path, "/v2/repo/manifests/"))
		w.Header().Set("Content-Type", ociclient.MediaTypeImageManifest)
		// The card verifies, but these same bytes do not match the benchmark digest.
		_, _ = w.Write(child)
	}))
	defer srv.Close()
	client, err := ociclient.NewClient(srv.URL, "repo", ociclient.Credentials{}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	job := completedJob()
	for i := range job.Results.Benchmarks {
		job.Results.Benchmarks[i].Artifacts["oci_digest"] = benchDigest
	}
	ref, err := NewBundlePublisher(client).Publish(context.Background(), job, &api.OCIArtifactReference{OCIDigest: cardDigest})
	if err == nil || ref != nil {
		t.Fatalf("accepted mismatched benchmark digest: %v %v", ref, err)
	}
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Operation != "verify benchmark 0 manifest" {
		t.Fatalf("unexpected verification error: %v", err)
	}
	if !strings.Contains(operationErr.Err.Error(), "manifest content digest does not match "+benchDigest) {
		t.Fatalf("expected digest mismatch: %v", err)
	}
	if !slices.Equal(gets, []string{cardDigest, benchDigest}) || puts != 0 {
		t.Fatalf("registry operations: gets=%v puts=%d, want card and benchmark GETs and no PUT", gets, puts)
	}
}
