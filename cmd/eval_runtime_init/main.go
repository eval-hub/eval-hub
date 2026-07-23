package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	envBucket         = "TEST_DATA_S3_BUCKET"
	envKey            = "TEST_DATA_S3_KEY"
	envTimeout        = "TEST_DATA_S3_TIMEOUT"
	secretDir         = "/var/run/secrets/test-data" // #nosec G101 -- K8s secret mount path
	destDir           = "/test_data"
	regionOptionalKey = "AWS_DEFAULT_REGION"
	endpointKey       = "AWS_S3_ENDPOINT"
	accessKeyIDKey    = "AWS_ACCESS_KEY_ID"
	secretAccessKey   = "AWS_SECRET_ACCESS_KEY" // #nosec G101 -- env var name, not a credential value
	defaultTimeout    = 10 * time.Minute
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
	bucket := strings.TrimSpace(os.Getenv(envBucket))
	keyPrefix := strings.TrimSpace(os.Getenv(envKey))
	if bucket == "" || keyPrefix == "" {
		return fmt.Errorf("%s and %s are required", envBucket, envKey)
	}

	keyPrefix = strings.TrimPrefix(keyPrefix, "/")
	timeout := defaultTimeout
	if raw := strings.TrimSpace(os.Getenv(envTimeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", envTimeout, err)
		}
		timeout = parsed
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	accessKey := readSecret(accessKeyIDKey)
	secretKey := readSecret(secretAccessKey)
	region := readSecret(regionOptionalKey)
	endpoint := readSecret(endpointKey)

	if accessKey == "" {
		return fmt.Errorf("missing required secret %s", accessKeyIDKey)
	}
	if secretKey == "" {
		return fmt.Errorf("missing required secret %s", secretAccessKey)
	}
	if region == "" {
		return fmt.Errorf("missing required secret %s", regionOptionalKey)
	}
	if endpoint == "" {
		return fmt.Errorf("missing required secret %s", endpointKey)
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
			written, err := downloadObject(ctx, client, destRoot, bucket, keyPrefix, *obj.Key)
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

func downloadObject(ctx context.Context, client *s3.Client, destRoot *os.Root, bucket, prefix, key string) (int64, error) {
	rel, err := relativeDestPath(prefix, key)
	if err != nil {
		return 0, err
	}

	if dir := path.Dir(rel); dir != "." {
		if err := destRoot.MkdirAll(dir, 0o750); err != nil {
			return 0, fmt.Errorf("create dir for %q: %w", key, err)
		}
	}

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("get object %q: %w", key, err)
	}
	defer func() { _ = resp.Body.Close() }()
	file, err := destRoot.Create(rel)
	if err != nil {
		return 0, fmt.Errorf("create file %q: %w", key, err)
	}
	defer func() { _ = file.Close() }()

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return 0, fmt.Errorf("write file %q: %w", key, err)
	}
	return written, nil
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

func readSecret(name string) string {
	if name == "" {
		return ""
	}
	content, err := os.ReadFile(filepath.Join(secretDir, name)) // #nosec G304 -- name is a fixed secret key
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}
