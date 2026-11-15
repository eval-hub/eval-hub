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
	for _, failed := range []string{cardRef.OCIDigest, benchmarkDigest, "put"} {
		cause := &ociclient.RegistryHTTPError{Operation: "auth request", StatusCode: 403, Message: "token denied"}
		registry := &registryMock{failDigest: failed, err: cause}
		_, err := NewBundlePublisher(registry).Publish(context.Background(), completedJob(), &cardRef)
		var typed *ociclient.RegistryHTTPError
		if !errors.Is(err, cause) || !errors.As(err, &typed) || typed.StatusCode != 403 {
			t.Fatalf("lost typed error: %v", err)
		}
		if failed != "put" && registry.puts != 0 {
			t.Fatal("published despite child failure")
		}
		if failed == cardRef.OCIDigest && !strings.Contains(err.Error(), "evaluation card") {
			t.Fatal("missing card operation")
		}
		if failed == benchmarkDigest && !strings.Contains(err.Error(), "benchmark 0") {
			t.Fatal("missing benchmark operation")
		}
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
				if r.Method == http.MethodPut && r.URL.Path == "/v2/configured/repo/manifests/job-id" {
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
			if ref.OCIDigest != digestOf(submitted) || ref.OCIReference != strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")+"/configured/repo:job-id@"+ref.OCIDigest {
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

func TestBundlePublisherChildVerificationFailuresNeverPutIndex(t *testing.T) {
	for _, role := range []string{"card", "benchmark"} {
		for _, failure := range []string{"missing", "forbidden", "token denied", "malformed", "digest mismatch", "oversize", "media type conflict", "digest header conflict"} {
			t.Run(role+"/"+failure, func(t *testing.T) {
				child := []byte(`{"schemaVersion":2,"mediaType":"` + ociclient.MediaTypeImageManifest + `","config":{"mediaType":"application/vnd.oci.empty.v1+json","size":2,"digest":"` + digestOf([]byte("{}")) + `"},"layers":[]}`)
				cardBody := append(bytes.Clone(child), ' ')
				broken := bytes.Clone(child)
				if failure == "malformed" {
					broken = []byte(`{`)
				}
				if failure == "oversize" {
					broken = append(broken, bytes.Repeat([]byte(" "), ociclient.MaxManifestResponseBytes+1-len(broken))...)
				}
				brokenDigest := digestOf(broken)
				if failure == "digest mismatch" {
					brokenDigest = digestOf([]byte("wrong"))
				}
				cardDigest, benchDigest := digestOf(cardBody), digestOf(child)
				if role == "card" {
					cardDigest = brokenDigest
				} else {
					benchDigest = brokenDigest
				}
				puts := 0
				var origin string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPut {
						puts++
						w.WriteHeader(http.StatusCreated)
						return
					}
					if r.URL.Path == "/token" {
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, "token denied")
						return
					}
					if r.URL.Path == "/v2" {
						w.Header().Set("WWW-Authenticate", `Bearer realm="`+origin+`/token"`)
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					if r.URL.Path != "/v2/repo/manifests/"+cardDigest && r.URL.Path != "/v2/repo/manifests/"+benchDigest {
						t.Errorf("unexpected request: %s", r.URL)
						w.WriteHeader(http.StatusNotFound)
						return
					}
					w.Header().Set("Content-Type", ociclient.MediaTypeImageManifest)
					if strings.HasSuffix(r.URL.Path, brokenDigest) {
						switch failure {
						case "missing":
							w.WriteHeader(http.StatusNotFound)
							_, _ = io.WriteString(w, "child not found")
							return
						case "forbidden":
							w.WriteHeader(http.StatusForbidden)
							_, _ = io.WriteString(w, "pull denied")
							return
						case "token denied":
							w.WriteHeader(http.StatusUnauthorized)
							return
						case "media type conflict":
							w.Header().Set("Content-Type", ociclient.MediaTypeDockerManifest)
						case "digest header conflict":
							w.Header().Set("Docker-Content-Digest", digestOf(nil))
						}
						_, _ = w.Write(broken)
						return
					}
					_, _ = w.Write(cardBody)
				}))
				defer srv.Close()
				origin = srv.URL
				client, err := ociclient.NewClient(origin, "repo", ociclient.Credentials{}, srv.Client())
				if err != nil {
					t.Fatal(err)
				}
				job := completedJob()
				for i := range job.Results.Benchmarks {
					job.Results.Benchmarks[i].Artifacts["oci_digest"] = benchDigest
				}
				ref, err := NewBundlePublisher(client).Publish(context.Background(), job, &api.OCIArtifactReference{OCIDigest: cardDigest})
				if err == nil || ref != nil {
					t.Fatalf("accepted invalid %s: %v %v", role, ref, err)
				}
				if puts != 0 {
					t.Fatal("index published after failed child verification")
				}
				if failure == "missing" || failure == "forbidden" || failure == "token denied" {
					var httpErr *ociclient.RegistryHTTPError
					if !errors.As(err, &httpErr) {
						t.Fatalf("typed diagnostic lost: %v", err)
					}
					wantStatus := map[string]int{"missing": 404, "forbidden": 403, "token denied": 401}[failure]
					if httpErr.StatusCode != wantStatus || httpErr.Message == "" {
						t.Fatalf("lost HTTP details: %+v", httpErr)
					}
					if failure == "token denied" && httpErr.Operation != "auth request" {
						t.Fatalf("lost token operation: %+v", httpErr)
					}
				}
			})
		}
	}
}
