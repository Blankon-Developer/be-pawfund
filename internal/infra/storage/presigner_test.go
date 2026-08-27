package storage

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type headerPresignerStub struct {
	url         *url.URL
	err         error
	method      string
	bucket      string
	objectKey   string
	ttl         time.Duration
	reqParams   url.Values
	extraHeader http.Header
}

func (s *headerPresignerStub) PresignHeader(
	_ context.Context,
	method string,
	bucket string,
	objectKey string,
	ttl time.Duration,
	reqParams url.Values,
	extraHeader http.Header,
) (*url.URL, error) {
	s.method = method
	s.bucket = bucket
	s.objectKey = objectKey
	s.ttl = ttl
	s.reqParams = reqParams
	s.extraHeader = extraHeader.Clone()
	return s.url, s.err
}

func TestNewPutPresigner(t *testing.T) {
	validConfig := PresignerConfig{
		Endpoint:  "https://storage.example.com",
		AccessKey: "access-key",
		SecretKey: "secret-key",
		Bucket:    "pawfund",
		Region:    "us-east-1",
		TTL:       15 * time.Minute,
	}

	tests := []struct {
		name      string
		mutate    func(*PresignerConfig)
		wantError string
	}{
		{name: "accepts valid configuration"},
		{name: "rejects relative endpoint", mutate: func(cfg *PresignerConfig) { cfg.Endpoint = "localhost:9000" }, wantError: "absolute HTTP(S) URL"},
		{name: "rejects endpoint path", mutate: func(cfg *PresignerConfig) { cfg.Endpoint = "https://storage.example.com/minio" }, wantError: "must not contain"},
		{name: "requires access key", mutate: func(cfg *PresignerConfig) { cfg.AccessKey = " " }, wantError: "access key is required"},
		{name: "requires secret key", mutate: func(cfg *PresignerConfig) { cfg.SecretKey = " " }, wantError: "secret key is required"},
		{name: "requires bucket", mutate: func(cfg *PresignerConfig) { cfg.Bucket = " " }, wantError: "bucket is required"},
		{name: "requires region", mutate: func(cfg *PresignerConfig) { cfg.Region = " " }, wantError: "region is required"},
		{name: "rejects short TTL", mutate: func(cfg *PresignerConfig) { cfg.TTL = time.Millisecond }, wantError: "between 1 second and 7 days"},
		{name: "rejects long TTL", mutate: func(cfg *PresignerConfig) { cfg.TTL = maxPresignTTL + time.Second }, wantError: "between 1 second and 7 days"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig
			if test.mutate != nil {
				test.mutate(&cfg)
			}
			_, err := NewPutPresigner(cfg)
			if test.wantError == "" && err != nil {
				t.Fatalf("NewPutPresigner() unexpected error: %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("NewPutPresigner() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestPutPresignerPresignPut(t *testing.T) {
	presignedURL, err := url.Parse("https://storage.example.com/pawfund/profiles/id.jpg?signature=value")
	if err != nil {
		t.Fatalf("parse fixture URL: %v", err)
	}
	client := &headerPresignerStub{url: presignedURL}
	presigner := &PutPresigner{client: client, bucket: "pawfund", ttl: 15 * time.Minute}

	got, err := presigner.PresignPut(t.Context(), "profiles/id.jpg", "image/jpeg", 123456)
	if err != nil {
		t.Fatalf("PresignPut() unexpected error: %v", err)
	}
	if got != presignedURL.String() {
		t.Errorf("URL = %q, want %q", got, presignedURL)
	}
	if client.method != http.MethodPut || client.bucket != "pawfund" || client.objectKey != "profiles/id.jpg" {
		t.Errorf("request target = %q/%q/%q", client.method, client.bucket, client.objectKey)
	}
	if client.ttl != 15*time.Minute || client.reqParams != nil {
		t.Errorf("presign options = ttl %v params %#v", client.ttl, client.reqParams)
	}
	wantHeaders := http.Header{"Content-Length": {"123456"}, "Content-Type": {"image/jpeg"}}
	if !reflect.DeepEqual(client.extraHeader, wantHeaders) {
		t.Errorf("signed headers = %#v, want %#v", client.extraHeader, wantHeaders)
	}
}

func TestPutPresignerPresignPutSignedHeaders(t *testing.T) {
	presigner, err := NewPutPresigner(PresignerConfig{
		Endpoint:  "http://localhost:9000",
		AccessKey: "access-key",
		SecretKey: "secret-key",
		Bucket:    "pawfund",
		Region:    "us-east-1",
		TTL:       15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewPutPresigner(): %v", err)
	}

	rawURL, err := presigner.PresignPut(t.Context(), "profiles/id.webp", "image/webp", 42)
	if err != nil {
		t.Fatalf("PresignPut(): %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Host != "localhost:9000" || parsed.Path != "/pawfund/profiles/id.webp" {
		t.Errorf("presigned target = %s", parsed)
	}
	if got := parsed.Query().Get("X-Amz-SignedHeaders"); got != "content-length;content-type;host" {
		t.Errorf("X-Amz-SignedHeaders = %q", got)
	}
	if got := parsed.Query().Get("X-Amz-Expires"); got != "900" {
		t.Errorf("X-Amz-Expires = %q", got)
	}
}

func TestPutPresignerPresignPutError(t *testing.T) {
	presignFailure := errors.New("presigner unavailable")
	presigner := &PutPresigner{
		client: &headerPresignerStub{err: presignFailure},
		bucket: "pawfund",
		ttl:    time.Minute,
	}

	_, err := presigner.PresignPut(context.Background(), "profiles/id.png", "image/png", 10)
	if !errors.Is(err, presignFailure) || !strings.Contains(err.Error(), "presign PUT") {
		t.Fatalf("PresignPut() error = %v, want wrapped %v", err, presignFailure)
	}
}
