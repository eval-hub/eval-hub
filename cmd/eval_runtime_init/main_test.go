package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestRelativeDestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prefix  string
		key     string
		want    string
		wantErr string
	}{
		{
			name:   "nested under prefix",
			prefix: "datasets/run-1",
			key:    "datasets/run-1/examples.jsonl",
			want:   "examples.jsonl",
		},
		{
			name:   "nested subdirectory",
			prefix: "datasets/run-1",
			key:    "datasets/run-1/subdir/file.txt",
			want:   "subdir/file.txt",
		},
		{
			name:   "prefix only uses basename",
			prefix: "datasets/run-1",
			key:    "datasets/run-1",
			want:   "run-1",
		},
		{
			name:    "path traversal rejected",
			prefix:  "datasets/run-1",
			key:     "datasets/run-1/../../etc/passwd",
			wantErr: "escapes destination directory",
		},
		{
			name:    "dot only rejected",
			prefix:  "datasets",
			key:     "datasets/.",
			wantErr: "invalid object key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := relativeDestPath(tt.prefix, tt.key)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("relativeDestPath() = (%q, nil), want error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("relativeDestPath() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("relativeDestPath() = %v, want (%q, nil)", err, tt.want)
			}
			if got != tt.want {
				t.Fatalf("relativeDestPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDownloadObjectRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot() = %v", err)
	}
	defer destRoot.Close()

	_, err = downloadObject(context.Background(), nil, destRoot, "bucket", "datasets/run-1", "datasets/run-1/../../etc/passwd")
	if err == nil {
		t.Fatal("downloadObject() = nil, want relative path error")
	}
}

func TestDownloadObjectWritesNestedFile(t *testing.T) {
	t.Parallel()

	const objectKey = "data/nested/file.txt"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/bucket/"+objectKey {
			_, _ = io.Copy(w, strings.NewReader("hello"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("ak", "sk", "")),
	)
	if err != nil {
		t.Fatalf("LoadDefaultConfig() = %v", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(srv.URL)
		o.UsePathStyle = true
	})

	dir := t.TempDir()
	destRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot() = %v", err)
	}
	defer destRoot.Close()

	written, err := downloadObject(ctx, client, destRoot, "bucket", "data/", objectKey)
	if err != nil {
		t.Fatalf("downloadObject() = %v, want nil error", err)
	}
	if written != int64(len("hello")) {
		t.Fatalf("downloadObject() wrote %d bytes, want %d", written, len("hello"))
	}

	got, err := os.ReadFile(filepath.Join(dir, "nested", "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("file contents = %q, want %q", got, "hello")
	}
}
