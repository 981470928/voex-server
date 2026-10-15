package filestore

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"voex-server/internal/config"
	"voex-server/internal/shared"
)

// Store keeps durable content in a private R2 bucket and stages uploads locally.
type Store struct {
	client  *s3.Client
	bucket  string
	tempDir string
}

func New(settings config.Storage) (*Store, error) {
	for _, setting := range []struct{ name, value string }{
		{"R2_ENDPOINT", settings.Endpoint},
		{"R2_BUCKET", settings.Bucket},
		{"R2_ACCESS_KEY_ID", settings.AccessKeyID},
		{"R2_SECRET_ACCESS_KEY", settings.SecretAccessKey},
	} {
		if strings.TrimSpace(setting.value) == "" {
			return nil, fmt.Errorf("missing %s", setting.name)
		}
	}
	endpoint, err := url.Parse(settings.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil ||
		!strings.HasSuffix(endpoint.Hostname(), ".r2.cloudflarestorage.com") ||
		endpoint.Port() != "" || (endpoint.Path != "" && endpoint.Path != "/") ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("R2_ENDPOINT must be the HTTPS S3 API endpoint from the R2 bucket settings")
	}
	if len(settings.Bucket) < 3 || len(settings.Bucket) > 63 ||
		strings.Trim(settings.Bucket, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" ||
		strings.HasPrefix(settings.Bucket, "-") || strings.HasSuffix(settings.Bucket, "-") {
		return nil, fmt.Errorf("R2_BUCKET must be a valid bucket name")
	}
	if !filepath.IsAbs(settings.TempDir) {
		return nil, fmt.Errorf("VOEX_TEMP_DIR must be an absolute path")
	}
	root := filepath.Clean(settings.TempDir)
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create upload temporary directory: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect upload temporary directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("VOEX_TEMP_DIR must be a directory, not a symbolic link")
	}
	// Each process owns a private directory, including on shared temporary volumes.
	tempDir, err := os.MkdirTemp(root, "voex-uploads-")
	if err != nil {
		return nil, fmt.Errorf("create private upload directory: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	client := s3.NewFromConfig(aws.Config{
		Region:                     "auto",
		Credentials:                credentials.NewStaticCredentialsProvider(settings.AccessKeyID, settings.SecretAccessKey, ""),
		HTTPClient:                 &http.Client{Transport: transport},
		RetryMaxAttempts:           3,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(strings.TrimSuffix(settings.Endpoint, "/"))
		options.UsePathStyle = true
	})
	return &Store{client: client, bucket: settings.Bucket, tempDir: tempDir}, nil
}

// Close removes an empty process directory; unfinished uploads are never swept here.
func (s *Store) Close() {
	_ = os.Remove(s.tempDir)
}

func (s *Store) putObject(ctx context.Context, key, filename, mimeType string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          file,
		ContentLength: aws.Int64(info.Size()),
		ContentType:   aws.String(mimeType),
		IfNoneMatch:   aws.String("*"),
	})
	return err
}

func (s *Store) removeAsset(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		logStorageError("rollback asset", err)
	}
}

func storageStatus(err error) int {
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) {
		return response.HTTPStatusCode()
	}
	return 0
}

// Log only SDK error codes/types, never signed requests or credential values.
func logStorageError(operation string, err error) {
	code := ""
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		code = apiError.ErrorCode()
	}
	log.Printf("[R2] %s failed (status=%d code=%q type=%T)", operation, storageStatus(err), code, err)
}

func storageFailure(operation string, err error) {
	logStorageError(operation, err)
	shared.Fail(http.StatusBadGateway, "文件存储服务暂不可用，请稍后重试")
}
