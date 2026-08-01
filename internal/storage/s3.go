package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	appconfig "github.com/Karan0009/wordotron_api/internal/config"
)

// S3 stores objects in any S3-compatible service. MinIO is supported by
// pointing S3_ENDPOINT at the MinIO server and enabling path-style addressing.
type S3 struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
	baseURL  string
	log      *slog.Logger
}

var _ Storage = (*S3)(nil)

// NewS3 builds the client. Static credentials are used when supplied,
// otherwise the default AWS credential chain (env, shared config, IRSA, EC2
// role) applies, which is what you want on managed infrastructure.
func NewS3(ctx context.Context, cfg appconfig.Storage, log *slog.Logger) (*S3, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.S3Region),
	}
	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
		o.UsePathStyle = cfg.S3UsePathStyle
	})

	baseURL := cfg.PublicBaseURL
	if baseURL == "" && cfg.S3Endpoint != "" {
		baseURL = strings.TrimRight(cfg.S3Endpoint, "/") + "/" + cfg.S3Bucket
	}

	log.Info("using s3 object storage",
		slog.String("bucket", cfg.S3Bucket),
		slog.String("region", cfg.S3Region),
		slog.Bool("path_style", cfg.S3UsePathStyle),
	)

	return &S3{
		client:   client,
		uploader: manager.NewUploader(client),
		bucket:   cfg.S3Bucket,
		baseURL:  strings.TrimRight(baseURL, "/"),
		log:      log,
	}, nil
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (*Object, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   r,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	if _, err := s.uploader.Upload(ctx, input); err != nil {
		return nil, fmt.Errorf("storage: upload object: %w", err)
	}

	return &Object{
		Key:         key,
		URL:         s.URL(key),
		Size:        size,
		ContentType: contentType,
	}, nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: get object: %w", err)
	}
	return out.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("storage: delete object: %w", err)
	}
	return nil
}

func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}

	if _, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("storage: head object: %w", err)
	}
	return true, nil
}

func (s *S3) URL(key string) string {
	return s.baseURL + "/" + strings.TrimPrefix(key, "/")
}
