package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/eval-hub/eval-hub/internal/runtimeenv"
)

const (
	// S3 env vars
	envBucket           = "TEST_DATA_S3_BUCKET"
	envKey              = "TEST_DATA_S3_KEY"
	envS3Timeout        = "TEST_DATA_S3_TIMEOUT"
	regionOptionalKey   = "AWS_DEFAULT_REGION"
	endpointKey         = "AWS_S3_ENDPOINT"
	accessKeyIDKey      = "AWS_ACCESS_KEY_ID"
	awsAccessKeyEnvName = "AWS_SECRET_ACCESS_KEY"

	// Git env vars
	envGitURL     = "TEST_DATA_GIT_URL"
	envGitRef     = "TEST_DATA_GIT_REF"
	envGitSubPath = "TEST_DATA_GIT_SUBPATH"
	envGitTimeout = "TEST_DATA_GIT_TIMEOUT"

	defaultTimeout = 10 * time.Minute
)

// Paths and URL validation are package vars so unit tests can redirect mounts and
// exercise runGit against local file:// repos without writing under /.
var (
	scrtDir        = "/var/run/secrets/test-data"
	destDir        = runtimeenv.TestDataDir
	gitMetadataDir = runtimeenv.InitMetadataDir
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := run(); err != nil {
		logger.Error("eval-runtime-init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("eval-runtime-init completed")
}

func run() error {
	if strings.TrimSpace(os.Getenv(envGitURL)) != "" {
		return runGit()
	}
	return runS3()
}

// runS3 downloads test data from S3 into destDir.
func runS3() error {
	bucket := strings.TrimSpace(os.Getenv(envBucket))
	keyPrefix := strings.TrimSpace(os.Getenv(envKey))
	if bucket == "" || keyPrefix == "" {
		return fmt.Errorf("%s and %s are required", envBucket, envKey)
	}

	keyPrefix = strings.TrimPrefix(keyPrefix, "/")
	timeout := defaultTimeout
	if raw := strings.TrimSpace(os.Getenv(envS3Timeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", envS3Timeout, err)
		}
		if parsed <= 0 {
			return fmt.Errorf("invalid %s: must be a positive duration", envS3Timeout)
		}
		timeout = parsed
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	accessKey, err := readSecret(accessKeyIDKey)
	if err != nil {
		return fmt.Errorf("missing required secret %s: %w", accessKeyIDKey, err)
	}
	secretKey, err := readSecret(awsAccessKeyEnvName)
	if err != nil {
		return fmt.Errorf("missing required secret %s: %w", awsAccessKeyEnvName, err)
	}
	region, err := readSecret(regionOptionalKey)
	if err != nil {
		return fmt.Errorf("missing required secret %s: %w", regionOptionalKey, err)
	}
	endpoint, err := readSecret(endpointKey)
	if err != nil {
		return fmt.Errorf("missing required secret %s: %w", endpointKey, err)
	}

	cfg, err := loadAWSConfig(ctx, region, accessKey, secretKey)
	if err != nil {
		return err
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	})
	tm := transfermanager.New(client)

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	destRoot, err := os.OpenRoot(destDir)
	if err != nil {
		return fmt.Errorf("open dest root: %w", err)
	}
	defer func() { _ = destRoot.Close() }()

	slog.Info("starting download", "bucket", bucket, "key", keyPrefix)

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(keyPrefix),
	})

	found := false
	var fileCount int64
	var totalBytes int64
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil || *obj.Key == "" {
				continue
			}
			if strings.HasSuffix(*obj.Key, "/") {
				continue
			}
			found = true
			written, err := downloadObject(ctx, tm, destRoot, bucket, keyPrefix, *obj.Key)
			if err != nil {
				return err
			}
			fileCount++
			totalBytes += written
		}
	}

	if !found {
		return fmt.Errorf("no objects found for s3://%s/%s", bucket, keyPrefix)
	}
	slog.Info("download complete", "files", fileCount, "mb", totalBytes/(1024*1024))
	return nil
}

// copyDirFromRoot copies the contents of srcRoot into dst, skipping the .git directory.
// All reads are confined by os.Root; destination writes use a second Root under dst.
func copyDirFromRoot(srcRoot *os.Root, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstRoot.Close() }()

	return fs.WalkDir(srcRoot.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		// fs.WalkDir yields slash-separated paths; reject escapes before opening.
		rel := filepath.FromSlash(p)
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("repository contains path %q that escapes the clone root; refusing to copy", p)
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.IsDir() {
			return dstRoot.MkdirAll(rel, 0o750)
		}
		return copyFileBetweenRoots(srcRoot, dstRoot, rel)
	})
}

// copyFileBetweenRoots writes each file as 0600 (owner read/write only) and does not
// preserve the source mode, including the execute bit. That is intentional: evaluation
// test data is read by the adapter, not executed; OpenShift SCCs use static UIDs so
// owner-only perms remain readable by the job containers that share the pod UID.
func copyFileBetweenRoots(srcRoot, dstRoot *os.Root, rel string) error {
	in, err := srcRoot.Open(rel)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := dstRoot.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func loadAWSConfig(ctx context.Context, region, accessKey, secretKey string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	if accessKey != "" && secretKey != "" {
		provider := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")
		opts = append(opts, config.WithCredentialsProvider(provider))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load aws config: %w", err)
	}
	return cfg, nil
}

func downloadObject(ctx context.Context, tm *transfermanager.Client, destRoot *os.Root, bucket, prefix, key string) (int64, error) {
	rel, err := relativeDestPath(prefix, key)
	if err != nil {
		return 0, err
	}

	if dir := path.Dir(rel); dir != "." {
		if err := destRoot.MkdirAll(dir, 0o750); err != nil {
			return 0, fmt.Errorf("create dir for %q: %w", key, err)
		}
	}

	file, err := destRoot.Create(rel)
	if err != nil {
		return 0, fmt.Errorf("create file %q: %w", key, err)
	}
	defer func() { _ = file.Close() }()

	out, err := tm.DownloadObject(ctx, &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		WriterAt: file,
	})
	if err != nil {
		return 0, fmt.Errorf("download object %q: %w", key, err)
	}
	return aws.ToInt64(out.ContentLength), nil
}

func relativeDestPath(prefix, key string) (string, error) {
	rel := strings.TrimPrefix(key, prefix)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		rel = path.Base(key)
	}
	rel = filepath.FromSlash(rel)
	if rel == "." || rel == "/" {
		return "", errors.New("invalid object key for destination path")
	}
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("object key escapes destination directory: %q", key)
	}
	return filepath.ToSlash(rel), nil
}

// readSecret reads a key from the mounted secret dir. Keys must be a single path
// segment; reads go through os.Root so they cannot escape scrtDir. Returns an
// error if the key is invalid, missing, or empty after trimming.
func readSecret(key string) (string, error) {
	if key == "" || key == "." || key == "/" || !filepath.IsLocal(key) || filepath.Base(key) != key {
		return "", fmt.Errorf("secret key %q contains path separators and is not allowed", key)
	}
	root, err := os.OpenRoot(scrtDir)
	if err != nil {
		return "", fmt.Errorf("secret key %q not found in mounted secret: %w", key, err)
	}
	defer func() { _ = root.Close() }()
	content, err := root.ReadFile(key)
	if err != nil {
		return "", fmt.Errorf("secret key %q not found in mounted secret: %w", key, err)
	}
	val := strings.TrimSpace(string(content))
	if val == "" {
		return "", fmt.Errorf("secret key %q is present but empty", key)
	}
	return val, nil
}
