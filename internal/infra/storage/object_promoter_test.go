package storage

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

type objectPromoterClientStub struct {
	statInfo     minio.ObjectInfo
	statErr      error
	statCalls    int
	statObject   string
	copyErr      error
	copyCalls    int
	copyDest     string
	copySource   string
	removeErr    error
	removeCalls  int
	removeObject string
}

func (s *objectPromoterClientStub) StatObject(
	_ context.Context,
	_, objectName string,
	_ minio.StatObjectOptions,
) (minio.ObjectInfo, error) {
	s.statCalls++
	s.statObject = objectName
	return s.statInfo, s.statErr
}

func (s *objectPromoterClientStub) CopyObject(
	_ context.Context,
	dst minio.CopyDestOptions,
	src minio.CopySrcOptions,
) (minio.UploadInfo, error) {
	s.copyCalls++
	s.copyDest = dst.Object
	s.copySource = src.Object
	return minio.UploadInfo{}, s.copyErr
}

func (s *objectPromoterClientStub) RemoveObject(
	_ context.Context,
	_, objectName string,
	_ minio.RemoveObjectOptions,
) error {
	s.removeCalls++
	s.removeObject = objectName
	return s.removeErr
}

func TestNewObjectPromoter(t *testing.T) {
	validConfig := PresignerConfig{
		Endpoint:  "https://storage.example.com",
		AccessKey: "access-key",
		SecretKey: "secret-key",
		Bucket:    "pawfund",
		Region:    "us-east-1",
	}

	tests := []struct {
		name      string
		mutate    func(*PresignerConfig)
		wantError string
	}{
		{name: "accepts valid config"},
		{name: "requires access key", mutate: func(cfg *PresignerConfig) { cfg.AccessKey = " " }, wantError: "access key is required"},
		{name: "requires secret key", mutate: func(cfg *PresignerConfig) { cfg.SecretKey = " " }, wantError: "secret key is required"},
		{name: "requires bucket", mutate: func(cfg *PresignerConfig) { cfg.Bucket = " " }, wantError: "bucket is required"},
		{name: "requires region", mutate: func(cfg *PresignerConfig) { cfg.Region = " " }, wantError: "region is required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig
			if test.mutate != nil {
				test.mutate(&cfg)
			}
			_, err := NewObjectPromoter(cfg)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("NewObjectPromoter() unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("NewObjectPromoter() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestObjectPromoterPromote(t *testing.T) {
	source := "tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"
	dest := "profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"
	missing := minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound}
	copyFailure := errors.New("copy failed")

	tests := []struct {
		name          string
		source        string
		dest          string
		statErr       error
		copyErr       error
		removeErr     error
		wantError     error
		wantErrorText string
		wantCopy      bool
		wantRemove    bool
	}{
		{
			name:     "copies missing destination without deleting source",
			source:   source,
			dest:     dest,
			statErr:  missing,
			wantCopy: true,
		},
		{
			name:   "skips copy when destination already exists",
			source: source,
			dest:   dest,
		},
		{
			name:      "maps missing source",
			source:    source,
			dest:      dest,
			statErr:   missing,
			copyErr:   missing,
			wantError: ErrObjectNotFound,
			wantCopy:  true,
		},
		{
			name:          "wraps copy failure",
			source:        source,
			dest:          dest,
			statErr:       missing,
			copyErr:       copyFailure,
			wantError:     copyFailure,
			wantErrorText: "copy object",
			wantCopy:      true,
		},
		{
			name:          "requires keys",
			wantErrorText: "object key is required",
		},
		{
			name:          "rejects identical keys",
			source:        dest,
			dest:          dest,
			wantErrorText: "must differ",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &objectPromoterClientStub{
				statErr:   test.statErr,
				copyErr:   test.copyErr,
				removeErr: test.removeErr,
			}
			promoter := &ObjectPromoter{client: client, bucket: "pawfund"}
			err := promoter.Promote(t.Context(), test.source, test.dest)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Promote() error = %v, want %v", err, test.wantError)
				}
			} else if test.wantErrorText != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrorText) {
					t.Fatalf("Promote() error = %v, want containing %q", err, test.wantErrorText)
				}
			} else if err != nil {
				t.Fatalf("Promote() unexpected error: %v", err)
			}
			if (client.copyCalls > 0) != test.wantCopy {
				t.Errorf("copy calls = %d, want called = %v", client.copyCalls, test.wantCopy)
			}
			if test.wantCopy && (client.copySource != source || client.copyDest != dest) {
				t.Errorf("copy %q → %q", client.copySource, client.copyDest)
			}
			if (client.removeCalls > 0) != test.wantRemove {
				t.Errorf("remove calls = %d, want called = %v", client.removeCalls, test.wantRemove)
			}
			if test.wantRemove && client.removeObject != source {
				t.Errorf("removed object = %q, want %q", client.removeObject, source)
			}
		})
	}
}

func TestObjectPromoterDiscard(t *testing.T) {
	source := "tmp/profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"
	deleteFailure := errors.New("delete failed")

	tests := []struct {
		name       string
		objectKey  string
		removeErr  error
		wantRemove bool
	}{
		{name: "deletes staging object", objectKey: source, wantRemove: true},
		{name: "ignores delete failure", objectKey: source, removeErr: deleteFailure, wantRemove: true},
		{name: "skips blank key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &objectPromoterClientStub{removeErr: test.removeErr}
			promoter := &ObjectPromoter{client: client, bucket: "pawfund"}
			promoter.Discard(t.Context(), test.objectKey)
			if (client.removeCalls > 0) != test.wantRemove {
				t.Errorf("remove calls = %d, want called = %v", client.removeCalls, test.wantRemove)
			}
			if test.wantRemove && client.removeObject != source {
				t.Errorf("removed object = %q, want %q", client.removeObject, source)
			}
		})
	}
}
