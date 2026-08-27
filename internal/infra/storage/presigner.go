package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const maxPresignTTL = 7 * 24 * time.Hour

type PresignerConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	TTL       time.Duration
}

type headerPresigner interface {
	PresignHeader(
		ctx context.Context,
		method string,
		bucketName string,
		objectName string,
		expires time.Duration,
		reqParams url.Values,
		extraHeaders http.Header,
	) (*url.URL, error)
}

type PutPresigner struct {
	client headerPresigner
	bucket string
	ttl    time.Duration
}

func NewPutPresigner(cfg PresignerConfig) (*PutPresigner, error) {
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
	if cfg.TTL < time.Second || cfg.TTL > maxPresignTTL {
		return nil, fmt.Errorf("storage: presign TTL must be between 1 second and 7 days")
	}

	client, err := minio.New(parsedEndpoint.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(strings.TrimSpace(cfg.AccessKey), strings.TrimSpace(cfg.SecretKey), ""),
		Secure: parsedEndpoint.Scheme == "https",
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: initialize MinIO client: %w", err)
	}

	return &PutPresigner{client: client, bucket: bucket, ttl: cfg.TTL}, nil
}

func (p *PutPresigner) PresignPut(
	ctx context.Context,
	objectKey string,
	contentType string,
	size int64,
) (string, error) {
	headers := make(http.Header)
	headers.Set("Content-Type", contentType)
	headers.Set("Content-Length", strconv.FormatInt(size, 10))

	presignedURL, err := p.client.PresignHeader(
		ctx,
		http.MethodPut,
		p.bucket,
		objectKey,
		p.ttl,
		nil,
		headers,
	)
	if err != nil {
		return "", fmt.Errorf("storage: presign PUT: %w", err)
	}
	return presignedURL.String(), nil
}

func parseEndpoint(rawEndpoint string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawEndpoint))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("storage: endpoint must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("storage: endpoint must not contain credentials, path, query, or fragment")
	}
	return parsed, nil
}
