package ociclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func childManifest(t *testing.T, mediaType string) []byte {
	t.Helper()
	body, err := json.Marshal(manifest{
		SchemaVersion: 2, MediaType: mediaType,
		Config: Descriptor{MediaType: MediaTypeArtifactConfig, Size: 2, Digest: blobDigest([]byte("{}"))},
		Layers: []Descriptor{{MediaType: "application/octet-stream", Size: 0, Digest: blobDigest(nil)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestGetManifestFormats(t *testing.T) {
	for _, mediaType := range []string{MediaTypeImageManifest, MediaTypeDockerManifest} {
		for _, source := range []string{"both", "header", "body"} {
			t.Run(mediaType+"/"+source, func(t *testing.T) {
				body := childManifest(t, mediaType)
				if source == "header" {
					body = bytes.Replace(body, []byte(`"mediaType":"`+mediaType+`",`), nil, 1)
				}
				digest := blobDigest(body)
				srv, httpClient := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != "/v2/team/results/manifests/"+digest {
						t.Errorf("unexpected request %s %s", r.Method, r.URL)
					}
					if r.Header.Get("Accept") != MediaTypeImageManifest+", "+MediaTypeDockerManifest {
						t.Error("missing explicit Accept formats")
					}
					if source != "body" {
						w.Header().Set("Content-Type", mediaType+"; charset=utf-8")
					} else {
						w.Header()["Content-Type"] = nil
					}
					w.Header().Set("Docker-Content-Digest", digest)
					_, _ = w.Write(body)
				}))
				client, err := NewClient(srv.URL, "team/results", Credentials{}, httpClient)
				if err != nil {
					t.Fatal(err)
				}
				d, err := client.GetManifest(context.Background(), digest)
				if err != nil {
					t.Fatal(err)
				}
				if d.Digest != digest || d.Size != int64(len(body)) || d.MediaType != mediaType {
					t.Fatalf("incorrect descriptor: %+v", d)
				}
			})
		}
	}
}

func TestGetManifestRejectsInvalidResponses(t *testing.T) {
	valid := string(childManifest(t, MediaTypeImageManifest))
	for _, tc := range []struct{ name, body, contentType, headerDigest, requestDigest string }{
		{name: "malformed JSON", body: `{`},
		{name: "null", body: `null`},
		{name: "schema", body: strings.Replace(valid, `"schemaVersion":2`, `"schemaVersion":1`, 1)},
		{name: "missing config", body: `{"schemaVersion":2,"mediaType":"` + MediaTypeImageManifest + `","layers":[]}`},
		{name: "missing layers", body: strings.Replace(valid, `"layers":`, `"ignored":`, 1)},
		{name: "null layers", body: strings.Replace(valid, `"layers":[`, `"layers":null,"ignored":[`, 1)},
		{name: "missing descriptor size", body: strings.Replace(valid, `"size":2,`, ``, 1)},
		{name: "negative descriptor size", body: strings.Replace(valid, `"size":2`, `"size":-1`, 1)},
		{name: "invalid descriptor digest", body: strings.Replace(valid, blobDigest(nil), "sha256:no", 1)},
		{name: "invalid descriptor media type", body: strings.Replace(valid, "application/octet-stream", "bad", 1)},
		{name: "invalid subject", body: strings.Replace(valid, `"schemaVersion":2`, `"subject":{},"schemaVersion":2`, 1)},
		{name: "unsupported media type", body: strings.Replace(valid, MediaTypeImageManifest, MediaTypeImageIndex, 1)},
		{name: "missing media type", body: strings.Replace(valid, `"mediaType":"`+MediaTypeImageManifest+`",`, ``, 1)},
		{name: "header conflict", body: valid, contentType: MediaTypeDockerManifest},
		{name: "unsupported header", body: valid, contentType: "application/json"},
		{name: "malformed header", body: valid, contentType: "invalid; broken"},
		{name: "header digest conflict", body: valid, headerDigest: blobDigest(nil)},
		{name: "body digest conflict", body: valid, requestDigest: blobDigest(nil)},
		{name: "trailing JSON", body: valid + `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			digest := blobDigest([]byte(tc.body))
			if tc.requestDigest != "" {
				digest = tc.requestDigest
			}
			srv, httpClient := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header()["Content-Type"] = nil
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				if tc.headerDigest != "" {
					w.Header().Set("Docker-Content-Digest", tc.headerDigest)
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			client, err := NewClient(srv.URL, "repo", Credentials{}, httpClient)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.GetManifest(context.Background(), digest); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

type manifestRoundTripper func(*http.Request) (*http.Response, error)

func (f manifestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type manifestErrorReader struct{ err error }

func (r manifestErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestGetManifestPreservesIOFailures(t *testing.T) {
	for _, failure := range []string{"transport", "interrupted response"} {
		t.Run(failure, func(t *testing.T) {
			cause := errors.New("registry connection interrupted")
			httpClient := &http.Client{Transport: manifestRoundTripper(func(*http.Request) (*http.Response, error) {
				if failure == "transport" {
					return nil, cause
				}
				return &http.Response{
					StatusCode:    http.StatusOK,
					Header:        http.Header{"Content-Type": {MediaTypeImageManifest}},
					ContentLength: -1,
					Body:          io.NopCloser(io.MultiReader(strings.NewReader(`{"schemaVersion":`), manifestErrorReader{cause})),
				}, nil
			})}
			client, err := NewClient("http://localhost:5000", "repo", Credentials{}, httpClient)
			if err != nil {
				t.Fatal(err)
			}
			d, err := client.GetManifest(context.Background(), blobDigest(nil))
			if !errors.Is(err, cause) || !reflect.DeepEqual(d, Descriptor{}) {
				t.Fatalf("expected original I/O error and no descriptor: %+v %v", d, err)
			}
			operation := "get manifest"
			if failure == "interrupted response" {
				operation = "read manifest"
			}
			if !strings.Contains(err.Error(), operation) {
				t.Fatalf("missing operation context: %v", err)
			}
		})
	}
}

func TestGetManifestReadBound(t *testing.T) {
	base := childManifest(t, MediaTypeImageManifest)
	for _, size := range []int{MaxManifestResponseBytes, MaxManifestResponseBytes + 1} {
		for _, contentLength := range []int64{-1, 1, int64(size)} {
			t.Run(fmt.Sprintf("%d/length%d", size, contentLength), func(t *testing.T) {
				body := append(bytes.Clone(base), bytes.Repeat([]byte(" "), size-len(base))...)
				// A custom transport exposes missing/misleading lengths without the
				// standard HTTP transport truncating or rejecting the response first.
				reader := bytes.NewReader(append(bytes.Clone(body), bytes.Repeat([]byte(" "), 100)...))
				actualReader := io.Reader(reader)
				if size == MaxManifestResponseBytes {
					actualReader = bytes.NewReader(body)
				}
				httpClient := &http.Client{Transport: manifestRoundTripper(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {MediaTypeImageManifest}}, ContentLength: contentLength, Body: io.NopCloser(actualReader)}, nil
				})}
				client, err := NewClient("http://localhost:5000", "repo", Credentials{}, httpClient)
				if err != nil {
					t.Fatal(err)
				}
				d, err := client.GetManifest(context.Background(), blobDigest(body))
				if size > MaxManifestResponseBytes {
					if err == nil || !strings.Contains(err.Error(), "exceeds") {
						t.Fatalf("expected bounded response error: %v", err)
					}
					if reader.Len() != size+100-(MaxManifestResponseBytes+1) {
						t.Fatal("read beyond limit plus one")
					}
				} else if contentLength == 1 {
					if err == nil {
						t.Fatal("accepted conflicting Content-Length")
					}
				} else if err != nil || d.Size != int64(size) {
					t.Fatalf("boundary rejected: %+v %v", d, err)
				}
			})
		}
	}
}

func TestGetManifestRejectsInvalidDigestBeforeRequest(t *testing.T) {
	client, err := NewClient("http://localhost:5000", "repo", Credentials{}, &http.Client{Transport: manifestRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network request"); return nil, nil })})
	if err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"", "sha256:no", "sha512:" + strings.Repeat("a", 128), "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("z", 64), "../tag"} {
		if _, err := client.GetManifest(context.Background(), digest); err == nil {
			t.Errorf("accepted %q", digest)
		}
	}
}

func TestChildDescriptorDigests(t *testing.T) {
	for _, digest := range []string{blobDigest(nil), "sha512:" + strings.Repeat("a", 128), "multihash+base58:QmRZxt2b1FVZPNqd8hsiykDL3TdBDeTSPX9Kv46HmX4Gx8"} {
		raw, err := json.Marshal(Descriptor{MediaType: "application/octet-stream", Size: 0, Digest: digest})
		if err != nil {
			t.Fatal(err)
		}
		if err := validateChildDescriptor(raw); err != nil {
			t.Errorf("valid digest %q rejected: %v", digest, err)
		}
	}
	for _, digest := range []string{"sha256:" + strings.Repeat("A", 64), "sha512:bad", "algorithm::digest", "algorithm:/path", "invalid++algorithm:abc"} {
		raw, err := json.Marshal(Descriptor{MediaType: "application/octet-stream", Size: 0, Digest: digest})
		if err != nil {
			t.Fatal(err)
		}
		if err := validateChildDescriptor(raw); err == nil {
			t.Errorf("invalid digest %q accepted", digest)
		}
	}
}

func TestIndexPublication(t *testing.T) {
	for _, responseDigest := range []string{"", "correct", "conflicting"} {
		t.Run(responseDigest, func(t *testing.T) {
			var submitted []byte
			srv, httpClient := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/v2/repo/manifests/job" || r.Header.Get("Content-Type") != MediaTypeImageIndex {
					t.Errorf("incorrect index request: %s %s %v", r.Method, r.URL, r.Header)
				}
				var err error
				submitted, err = io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if responseDigest == "correct" {
					w.Header().Set("Docker-Content-Digest", blobDigest(submitted))
				}
				if responseDigest == "conflicting" {
					w.Header().Set("Docker-Content-Digest", blobDigest(nil))
				}
				w.WriteHeader(http.StatusCreated)
			}))
			client, err := NewClient(srv.URL, "repo", Credentials{}, httpClient)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := client.PushIndex(context.Background(), "job", Index{SchemaVersion: 2, MediaType: MediaTypeImageIndex, Manifests: []Descriptor{{MediaType: MediaTypeImageManifest, Size: 2, Digest: blobDigest([]byte("{}"))}}})
			if responseDigest == "conflicting" {
				if err == nil {
					t.Fatal("accepted conflicting digest")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ref.OCIDigest != blobDigest(submitted) || ref.OCIReference != strings.TrimPrefix(srv.URL, "http://")+"/repo:job@"+ref.OCIDigest {
				t.Fatalf("incorrect identity: %+v", ref)
			}
		})
	}
}

func TestPushIndexRejectsInvalidInputBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tag    string
		mutate func(*Index)
	}{
		{name: "empty tag"},
		{name: "oversized tag", tag: strings.Repeat("a", 129)},
		{name: "invalid tag", tag: "../job"},
		{name: "wrong schema version", tag: "job", mutate: func(index *Index) { index.SchemaVersion = 1 }},
		{name: "wrong media type", tag: "job", mutate: func(index *Index) { index.MediaType = MediaTypeImageManifest }},
		{name: "missing manifests", tag: "job", mutate: func(index *Index) { index.Manifests = nil }},
		{name: "empty manifests", tag: "job", mutate: func(index *Index) { index.Manifests = []Descriptor{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient("http://localhost:5000", "repo", Credentials{}, &http.Client{Transport: manifestRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid index input caused a registry request")
				return nil, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			index := Index{SchemaVersion: 2, MediaType: MediaTypeImageIndex, Manifests: []Descriptor{{MediaType: MediaTypeImageManifest, Size: 2, Digest: blobDigest([]byte("{}"))}}}
			if tc.mutate != nil {
				tc.mutate(&index)
			}
			if ref, err := client.PushIndex(context.Background(), tc.tag, index); err == nil || ref != nil {
				t.Fatalf("invalid index accepted: %+v %v", ref, err)
			}
		})
	}
}

func TestManifestOperationsPreserveHTTPDiagnostics(t *testing.T) {
	for _, operation := range []string{"get", "put"} {
		for _, failure := range []string{"missing", "forbidden", "token"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				status := http.StatusForbidden
				if failure == "missing" {
					status = http.StatusNotFound
				}
				var origin string
				srv, httpClient := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if failure == "token" && r.URL.Path != "/token" {
						w.Header().Set("WWW-Authenticate", `Bearer realm="`+origin+`/token"`)
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					w.WriteHeader(status)
					_, _ = io.WriteString(w, "permission diagnostic")
				}))
				origin = srv.URL
				client, err := NewClient(srv.URL, "repo", Credentials{}, httpClient)
				if err != nil {
					t.Fatal(err)
				}
				if operation == "get" {
					_, err = client.GetManifest(context.Background(), blobDigest(nil))
				} else {
					_, err = client.PushIndex(context.Background(), "job", Index{SchemaVersion: 2, MediaType: MediaTypeImageIndex, Manifests: []Descriptor{{}}})
				}
				var httpErr *RegistryHTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != status || httpErr.Message != "permission diagnostic" {
					t.Fatalf("lost diagnostic: %v", err)
				}
				if failure == "token" && httpErr.Operation != "auth request" {
					t.Fatalf("lost token operation: %+v", httpErr)
				}
			})
		}
	}
}
