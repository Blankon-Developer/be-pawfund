package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
)

func TestRegisterSupporterRequestNormalize(t *testing.T) {
	blankImage := "   "
	image := " profiles/cat.png "

	tests := []struct {
		name      string
		request   registerSupporterRequest
		wantName  string
		wantEmail string
		wantImage *string
	}{
		{
			name:      "normalizes text fields",
			request:   registerSupporterRequest{Name: " Cat Lover ", Email: " CAT@EXAMPLE.COM ", ImageObjectKey: &image},
			wantName:  "Cat Lover",
			wantEmail: "cat@example.com",
			wantImage: stringPointer("profiles/cat.png"),
		},
		{
			name:      "normalizes blank image to nil",
			request:   registerSupporterRequest{Name: "Cat", Email: "cat@example.com", ImageObjectKey: &blankImage},
			wantName:  "Cat",
			wantEmail: "cat@example.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.request.normalize()
			if test.request.Name != test.wantName || test.request.Email != test.wantEmail {
				t.Errorf("normalized request = %#v", test.request)
			}
			if !equalStringPointers(test.request.ImageObjectKey, test.wantImage) {
				t.Errorf("image object key = %v, want %v", pointerValue(test.request.ImageObjectKey), pointerValue(test.wantImage))
			}
		})
	}
}

func TestRegisterSupporterRequestValidate(t *testing.T) {
	longImageKey := strings.Repeat("k", maxImageObjectKeyBytes+1)

	tests := []struct {
		name    string
		request registerSupporterRequest
		want    httpx.FieldErrors
	}{
		{
			name:    "accepts valid request",
			request: registerSupporterRequest{Name: "Cat Lover", Email: "cat@example.com"},
		},
		{
			name:    "returns all required field errors",
			request: registerSupporterRequest{},
			want: httpx.FieldErrors{
				"name":  {"name is required!"},
				"email": {"email is required!"},
			},
		},
		{
			name: "returns all applicable email errors",
			request: registerSupporterRequest{
				Name:  "Cat Lover",
				Email: strings.Repeat("x", maxEmailCharacters+1),
			},
			want: httpx.FieldErrors{
				"email": {
					"email must not exceed 255 characters!",
					"email format is not valid!",
				},
			},
		},
		{
			name: "validates name and image limits",
			request: registerSupporterRequest{
				Name:           strings.Repeat("n", maxNameCharacters+1),
				Email:          "cat@example.com",
				ImageObjectKey: &longImageKey,
			},
			want: httpx.FieldErrors{
				"name":           {"name must not exceed 255 characters!"},
				"imageObjectKey": {"imageObjectKey must not exceed 1024 bytes!"},
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

func stringPointer(value string) *string {
	return &value
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func pointerValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
