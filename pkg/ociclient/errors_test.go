package ociclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRegistryHTTPFailures(t *testing.T) {
	const message = "repository access denied; status 500 is unrelated diagnostic text"
	for _, tc := range []struct {
		name string
		call func(*Client) error
	}{
		{"blob head", func(c *Client) error { _, err := c.blobExists(context.Background(), "sha256:abc"); return err }},
		{"start blob upload", func(c *Client) error { _, err := c.startBlobUpload(context.Background()); return err }},
		{"monolithic completion", func(c *Client) error {
			return c.uploadBlobMonolithic(context.Background(), []byte("card"), "sha256:abc")
		}},
		{"patch blob chunk", func(c *Client) error {
			_, err := c.patchBlobChunk(context.Background(), c.registry+"/upload", 0, 3, []byte("card"))
			return err
		}},
		{"chunked completion", func(c *Client) error {
			return c.completeBlobUpload(context.Background(), c.registry+"/upload", "sha256:abc")
		}},
		{"put manifest", func(c *Client) error { return c.putManifest(context.Background(), "card", []byte("{}")) }},
		{"auth request", func(c *Client) error { return c.auth.createNewToken(context.Background(), c.registry+"/token") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, httpClient := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "monolithic completion" && r.Method == http.MethodPost {
					w.Header().Set("Location", "/upload")
					w.WriteHeader(http.StatusAccepted)
					return
				}
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, message)
			}))
			client, err := NewClient(srv.URL, "team/results", Credentials{}, httpClient)
			if err != nil {
				t.Fatal(err)
			}
			err = fmt.Errorf("export: %w", tc.call(client))
			var httpErr *RegistryHTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden {
				t.Fatalf("missing structured HTTP status: %v", err)
			}
			// HEAD responses have no body, even if the server writes one.
			wantMessage := message
			if tc.name == "blob head" {
				wantMessage = ""
			}
			if httpErr.Message != wantMessage || !strings.Contains(err.Error(), wantMessage) {
				t.Fatalf("response message = %q, want %q", httpErr.Message, wantMessage)
			}
		})
	}
}

func TestRegistryErrorResponseReadBound(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", MaxErrorResponseBytes+100))
	err := registryHTTPError("put manifest", &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(body)})
	var httpErr *RegistryHTTPError
	if !errors.As(err, &httpErr) || len(httpErr.Message) != MaxErrorResponseBytes || body.Len() != 100 {
		t.Fatalf("error response was not bounded: remaining bytes = %d", body.Len())
	}
}

func TestRegistryErrorResponseReadFailure(t *testing.T) {
	err := registryHTTPError("auth request", &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(failedResponseReader{})})
	var httpErr *RegistryHTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("status or read error lost: %v", err)
	}
}

type failedResponseReader struct{}

func (failedResponseReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
