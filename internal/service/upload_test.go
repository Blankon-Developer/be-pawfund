package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type profileImagePresignerStub struct {
	url         string
	err         error
	calls       int
	objectKey   string
	contentType string
	size        int64
}

func (s *profileImagePresignerStub) PresignPut(
	_ context.Context,
	objectKey string,
	contentType string,
	size int64,
) (string, error) {
	s.calls++
	s.objectKey = objectKey
	s.contentType = contentType
	s.size = size
	return s.url, s.err
}

func TestProfileImageExtension(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
		wantError   bool
	}{
		{contentType: "image/jpeg", want: ".jpg"},
		{contentType: " image/png ", want: ".png"},
		{contentType: "IMAGE/WEBP", want: ".webp"},
		{contentType: "image/gif", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.contentType, func(t *testing.T) {
			got, err := profileImageExtension(test.contentType)
			if test.wantError {
				if !errors.Is(err, ErrUnsupportedProfileImageType) {
					t.Fatalf("profileImageExtension() error = %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("profileImageExtension() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestUploadServicePresignProfileImage(t *testing.T) {
	generatedID := uuid.MustParse("0198a123-4567-7abc-8123-456789abcdef")
	presigner := &profileImagePresignerStub{url: "https://storage.example.com/presigned"}
	uploadService := NewUploadService(presigner, func() (uuid.UUID, error) {
		return generatedID, nil
	})

	result, err := uploadService.PresignProfileImage(t.Context(), PresignProfileImageInput{
		ContentType: " IMAGE/JPEG ",
		Size:        123456,
	})
	if err != nil {
		t.Fatalf("PresignProfileImage() unexpected error: %v", err)
	}
	wantKey := "profiles/0198a123-4567-7abc-8123-456789abcdef.jpg"
	if result.ObjectKey != wantKey || result.URL != presigner.url {
		t.Errorf("result = %#v", result)
	}
	if presigner.calls != 1 || presigner.objectKey != wantKey || presigner.contentType != "image/jpeg" || presigner.size != 123456 {
		t.Errorf("presigner input = calls:%d key:%q type:%q size:%d", presigner.calls, presigner.objectKey, presigner.contentType, presigner.size)
	}
}

func TestUploadServiceUsesUUIDV7ByDefault(t *testing.T) {
	presigner := &profileImagePresignerStub{url: "https://storage.example.com/presigned"}
	uploadService := NewUploadService(presigner, nil)

	result, err := uploadService.PresignProfileImage(t.Context(), PresignProfileImageInput{
		ContentType: "image/png",
		Size:        1,
	})
	if err != nil {
		t.Fatalf("PresignProfileImage() unexpected error: %v", err)
	}
	rawID := strings.TrimSuffix(strings.TrimPrefix(result.ObjectKey, "profiles/"), ".png")
	id, err := uuid.Parse(rawID)
	if err != nil {
		t.Fatalf("object key UUID = %q: %v", rawID, err)
	}
	if id.Version() != 7 {
		t.Errorf("UUID version = %d, want 7", id.Version())
	}
}

func TestUploadServicePresignProfileImageErrors(t *testing.T) {
	idFailure := errors.New("UUID source failed")
	presignFailure := errors.New("storage failed")

	tests := []struct {
		name        string
		input       PresignProfileImageInput
		generateErr error
		presignErr  error
		wantError   error
		wantContext string
		wantCalls   int
	}{
		{
			name:      "rejects unsupported type",
			input:     PresignProfileImageInput{ContentType: "image/gif", Size: 1},
			wantError: ErrUnsupportedProfileImageType,
		},
		{
			name:      "rejects zero size",
			input:     PresignProfileImageInput{ContentType: "image/png", Size: 0},
			wantError: ErrInvalidProfileImageSize,
		},
		{
			name:      "rejects oversized image",
			input:     PresignProfileImageInput{ContentType: "image/png", Size: MaxProfileImageSize + 1},
			wantError: ErrInvalidProfileImageSize,
		},
		{
			name:        "wraps UUID failure",
			input:       PresignProfileImageInput{ContentType: "image/webp", Size: MaxProfileImageSize},
			generateErr: idFailure,
			wantError:   idFailure,
			wantContext: "generate profile image ID",
		},
		{
			name:        "wraps presigner failure",
			input:       PresignProfileImageInput{ContentType: "image/jpeg", Size: 1},
			presignErr:  presignFailure,
			wantError:   presignFailure,
			wantContext: "presign profile image upload",
			wantCalls:   1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			presigner := &profileImagePresignerStub{err: test.presignErr}
			uploadService := NewUploadService(presigner, func() (uuid.UUID, error) {
				return uuid.Nil, test.generateErr
			})
			_, err := uploadService.PresignProfileImage(t.Context(), test.input)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("PresignProfileImage() error = %v, want %v", err, test.wantError)
			}
			if test.wantContext != "" && !strings.Contains(err.Error(), test.wantContext) {
				t.Errorf("error = %q, want context %q", err, test.wantContext)
			}
			if presigner.calls != test.wantCalls {
				t.Errorf("presigner calls = %d, want %d", presigner.calls, test.wantCalls)
			}
		})
	}
}
