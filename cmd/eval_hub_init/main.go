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
	secretDir         = "/var/run/secrets/test-data"
	destDir           = "/test_data"
	regionOptionalKey = "AWS_DEFAULT_REGION"
	endpointKey       = "AWS_S3_ENDPOINT"
	accessKeyIDKey    = "AWS_ACCESS_KEY_ID"
	secretAccessKey   = "AWS_SECRET_ACCESS_KEY"
	defaultTimeout    = 10 * time.Minute
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := run(); err != nil {
		logger.Error("eval-hub-init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("eval-hub-init completed")
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

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

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
			written, err := downloadObject(ctx, client, bucket, keyPrefix, *obj.Key)
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

func downloadObject(ctx context.Context, client *s3.Client, bucket, prefix, key string) (int64, error) {
	rel := strings.TrimPrefix(key, prefix)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		rel = path.Base(key)
	}
	if rel == "." || rel == "/" {
		return 0, errors.New("invalid object key for destination path")
	}

	destPath := filepath.Join(destDir, filepath.FromSlash(rel))
	if !strings.HasPrefix(destPath, destDir+string(os.PathSeparator)) && destPath != destDir {
		return 0, fmt.Errorf("invalid destination path resolved for %q", key)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, fmt.Errorf("create dir for %q: %w", destPath, err)
	}

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("get object %q: %w", key, err)
	}
	defer resp.Body.Close()

	file, err := os.Create(destPath)
	if err != nil {
		return 0, fmt.Errorf("create file %q: %w", destPath, err)
	}
	defer file.Close()

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return 0, fmt.Errorf("write file %q: %w", destPath, err)
	}
	return written, nil
}

func readSecret(name string) string {
	if name == "" {
		return ""
	}
	content, err := os.ReadFile(filepath.Join(secretDir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}
