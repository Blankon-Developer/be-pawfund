package handler

import (
	"reflect"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
)

func TestPresignProfileImageRequestNormalize(t *testing.T) {
	request := presignProfileImageRequest{ContentType: " IMAGE/JPEG ", Size: 10}
	request.normalize()
	if request.ContentType != "image/jpeg" || request.Size != 10 {
		t.Errorf("normalized request = %#v", request)
	}
}

func TestPresignProfileImageRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request presignProfileImageRequest
		want    httpx.FieldErrors
	}{
		{name: "accepts JPEG", request: presignProfileImageRequest{ContentType: "image/jpeg", Size: 1}},
		{name: "accepts PNG at maximum size", request: presignProfileImageRequest{ContentType: "image/png", Size: service.MaxProfileImageSize}},
		{name: "accepts WebP", request: presignProfileImageRequest{ContentType: "image/webp", Size: 123}},
		{
			name: "requires both fields",
			want: httpx.FieldErrors{
				"contentType": {"contentType is required!"},
				"size":        {"size must be between 1 and 5242880 bytes!"},
			},
		},
		{
			name:    "rejects unsupported content type",
			request: presignProfileImageRequest{ContentType: "image/gif", Size: 1},
			want: httpx.FieldErrors{
				"contentType": {"contentType must be image/jpeg, image/png, or image/webp!"},
			},
		},
		{
			name:    "rejects oversized image",
			request: presignProfileImageRequest{ContentType: "image/jpeg", Size: service.MaxProfileImageSize + 1},
			want: httpx.FieldErrors{
				"size": {"size must be between 1 and 5242880 bytes!"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.request.validate(); !reflect.DeepEqual(got, test.want) {
				t.Errorf("validate() = %#v, want %#v", got, test.want)
			}
		})
	}
}
