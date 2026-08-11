package api

import (
	"encoding/json"
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

func TestReplaceSupporterProfileRequestNormalizeAndValidate(t *testing.T) {
	var request replaceSupporterProfileRequest
	if err := json.Unmarshal([]byte(`{
		"name":" Cat Lover ",
		"email":" CAT@EXAMPLE.COM ",
		"imageObjectKey":" profiles/cat.png "
	}`), &request); err != nil {
		t.Fatalf("decode replacement request: %v", err)
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		t.Fatalf("validate() = %#v, want nil", fieldErrors)
	}
	profile := request.toProfileReplacement()
	if profile.Name != "Cat Lover" || profile.Email != "cat@example.com" {
		t.Errorf("replacement profile = %#v", profile)
	}
	if !profile.ImageObjectKey.Set || profile.ImageObjectKey.Value == nil || *profile.ImageObjectKey.Value != "profiles/cat.png" {
		t.Errorf("image replacement = %#v", profile.ImageObjectKey)
	}
}

func TestReplaceSupporterProfileRequestImageModesAndValidation(t *testing.T) {
	parse := func(t *testing.T, body string) replaceSupporterProfileRequest {
		t.Helper()
		var request replaceSupporterProfileRequest
		if err := json.Unmarshal([]byte(body), &request); err != nil {
			t.Fatalf("decode replacement request: %v", err)
		}
		request.normalize()
		return request
	}

	tests := []struct {
		name           string
		body           string
		wantSet        bool
		wantImage      *string
		wantFieldError httpx.FieldErrors
	}{
		{
			name:      "preserves image when omitted",
			body:      `{"name":"Cat Lover","email":"cat@example.com"}`,
			wantImage: nil,
		},
		{
			name:    "clears image when null",
			body:    `{"name":"Cat Lover","email":"cat@example.com","imageObjectKey":null}`,
			wantSet: true,
		},
		{
			name:      "rejects blank image key when set",
			body:      `{"name":"Cat Lover","email":"cat@example.com","imageObjectKey":" "}`,
			wantSet:   true,
			wantImage: stringPointer(""),
			wantFieldError: httpx.FieldErrors{
				"imageObjectKey": {"imageObjectKey must not be empty!"},
			},
		},
		{
			name: "returns all required field errors",
			body: `{}`,
			wantFieldError: httpx.FieldErrors{
				"name":  {"name is required!"},
				"email": {"email is required!"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := parse(t, test.body)
			if request.ImageObjectKey.set != test.wantSet || !equalStringPointers(request.ImageObjectKey.value, test.wantImage) {
				t.Errorf("image update = %#v, want set=%v value=%v", request.ImageObjectKey, test.wantSet, pointerValue(test.wantImage))
			}
			if got := request.validate(); !reflect.DeepEqual(got, test.wantFieldError) {
				t.Errorf("validate() = %#v, want %#v", got, test.wantFieldError)
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
