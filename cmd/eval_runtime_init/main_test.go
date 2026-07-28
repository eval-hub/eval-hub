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
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
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
	defer func() { _ = destRoot.Close() }()

	_, err = downloadObject(context.Background(), transfermanager.New(nil), destRoot, "bucket", "datasets/run-1", "datasets/run-1/../../etc/passwd")
	if err == nil {
		t.Fatal("downloadObject() = nil, want relative path error")
	}
}

func TestLoadAWSConfig(t *testing.T) {
	t.Parallel()

	cfg, err := loadAWSConfig(context.Background(), "us-east-1", "access-key", "secret-key")
	if err != nil {
		t.Fatalf("loadAWSConfig() = %v, want nil error", err)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("loadAWSConfig() region = %q, want %q", cfg.Region, "us-east-1")
	}
}

func TestDownloadObjectFlatFile(t *testing.T) {
	t.Parallel()

	const objectKey = "data/file.txt"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/bucket/"+objectKey {
			_, _ = io.Copy(w, strings.NewReader("flat"))
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
	tm := transfermanager.New(client)

	dir := t.TempDir()
	destRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot() = %v", err)
	}
	defer func() { _ = destRoot.Close() }()

	// prefix == "data/" and key == "data/file.txt" → rel == "file.txt", no subdirectory created
	written, err := downloadObject(ctx, tm, destRoot, "bucket", "data/", objectKey)
	if err != nil {
		t.Fatalf("downloadObject() = %v, want nil error", err)
	}
	if written != int64(len("flat")) {
		t.Fatalf("downloadObject() wrote %d bytes, want %d", written, len("flat"))
	}

	got, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	if string(got) != "flat" {
		t.Fatalf("file contents = %q, want %q", got, "flat")
	}
}

func TestDownloadObjectS3Error(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("ak", "sk", "")),
		config.WithRetryMaxAttempts(1),
	)
	if err != nil {
		t.Fatalf("LoadDefaultConfig() = %v", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(srv.URL)
		o.UsePathStyle = true
	})
	tm := transfermanager.New(client)

	dir := t.TempDir()
	destRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot() = %v", err)
	}
	defer func() { _ = destRoot.Close() }()

	_, err = downloadObject(ctx, tm, destRoot, "bucket", "data/", "data/file.txt")
	if err == nil {
		t.Fatal("downloadObject() = nil, want error on S3 failure")
	}
	if !strings.Contains(err.Error(), "download object") {
		t.Fatalf("downloadObject() error = %v, want substring %q", err, "download object")
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
	tm := transfermanager.New(client)

	dir := t.TempDir()
	destRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot() = %v", err)
	}
	defer func() { _ = destRoot.Close() }()

	written, err := downloadObject(ctx, tm, destRoot, "bucket", "data/", objectKey)
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
