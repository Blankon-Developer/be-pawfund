package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var ErrObjectNotFound = errors.New("object not found")

type objectPromoterClient interface {
	StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
	CopyObject(ctx context.Context, dst minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error)
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error
}

type ObjectPromoter struct {
	client objectPromoterClient
	bucket string
}

func NewObjectPromoter(cfg PresignerConfig) (*ObjectPromoter, error) {
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

	return &ObjectPromoter{client: client, bucket: bucket}, nil
}

func (p *ObjectPromoter) Promote(ctx context.Context, sourceKey, destKey string) error {
	source := strings.TrimSpace(sourceKey)
	dest := strings.TrimSpace(destKey)
	if source == "" || dest == "" {
		return fmt.Errorf("storage: object key is required")
	}
	if source == dest {
		return fmt.Errorf("storage: source and destination must differ")
	}

	_, err := p.client.StatObject(ctx, p.bucket, dest, minio.StatObjectOptions{})
	if err == nil {
		return nil
	}
	if !isObjectNotFound(err) {
		return fmt.Errorf("storage: stat destination object: %w", err)
	}

	if _, err := p.client.CopyObject(ctx, minio.CopyDestOptions{
		Bucket: p.bucket,
		Object: dest,
	}, minio.CopySrcOptions{
		Bucket: p.bucket,
		Object: source,
	}); err != nil {
		if isObjectNotFound(err) {
			return fmt.Errorf("storage: copy object: %w", ErrObjectNotFound)
		}
		return fmt.Errorf("storage: copy object: %w", err)
	}

	return nil
}

func (p *ObjectPromoter) Discard(ctx context.Context, objectKey string) {
	key := strings.TrimSpace(objectKey)
	if key == "" {
		return
	}
	p.deleteBestEffort(ctx, key)
}

func (p *ObjectPromoter) deleteBestEffort(ctx context.Context, objectKey string) {
	if err := p.client.RemoveObject(ctx, p.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil && !isObjectNotFound(err) {
		slog.Warn("delete staging object", "object_key", objectKey, "error", err)
	}
}

func isObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.Code == "NotFound" || resp.StatusCode == http.StatusNotFound
}
