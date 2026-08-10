package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type removeObjectClient interface {
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error
}

// ObjectDeleter removes profile image objects from the configured bucket.
type ObjectDeleter struct {
	client removeObjectClient
	bucket string
}

func NewObjectDeleter(cfg PresignerConfig) (*ObjectDeleter, error) {
	parsedEndpoint, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.AccessKey) == "" {
		return nil, fmt.Errorf("storage: access key is required")
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("storage: secret key is required")
	}
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("storage: bucket is required")
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		return nil, fmt.Errorf("storage: region is required")
	}

	client, err := minio.New(parsedEndpoint.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(strings.TrimSpace(cfg.AccessKey), strings.TrimSpace(cfg.SecretKey), ""),
		Secure: parsedEndpoint.Scheme == "https",
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: initialize MinIO client: %w", err)
	}

	return &ObjectDeleter{client: client, bucket: bucket}, nil
}

func (d *ObjectDeleter) Delete(ctx context.Context, objectKey string) error {
	key := strings.TrimSpace(objectKey)
	if key == "" {
		return fmt.Errorf("storage: object key is required")
	}
	if err := d.client.RemoveObject(ctx, d.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: delete object: %w", err)
	}
	return nil
}
