package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
)

func TestRegisterFundraiserRequestNormalize(t *testing.T) {
	imageKey := " profiles/rescue.png "
	request := registerFundraiserRequest{
		Name:  " Animal Rescue ",
		Email: " RESCUE@EXAMPLE.COM ",
		ContactPerson: fundraiserContactPerson{
			Name:  " Jane Doe ",
			Phone: " +62 812 3456 ",
		},
		SocialURL:      " https://example.com/rescue ",
		Country:        " Indonesia ",
		ZipCode:        " 10110 ",
		ImageObjectKey: &imageKey,
	}

	request.normalize()

	if request.Name != "Animal Rescue" || request.Email != "rescue@example.com" {
		t.Errorf("normalized identity = %q/%q", request.Name, request.Email)
	}
	if request.ContactPerson.Name != "Jane Doe" || request.ContactPerson.Phone != "+62 812 3456" {
		t.Errorf("normalized contact person = %#v", request.ContactPerson)
	}
	if request.SocialURL != "https://example.com/rescue" || request.Country != "Indonesia" || request.ZipCode != "10110" {
		t.Errorf("normalized fundraiser fields = %#v", request)
	}
	if request.ImageObjectKey == nil || *request.ImageObjectKey != "profiles/rescue.png" {
		t.Errorf("normalized image key = %#v", request.ImageObjectKey)
	}

	blankImage := "   "
	request.ImageObjectKey = &blankImage
	request.normalize()
	if request.ImageObjectKey != nil {
		t.Errorf("blank image key = %#v, want nil", request.ImageObjectKey)
	}
}

func TestRegisterFundraiserRequestValidate(t *testing.T) {
	valid := registerFundraiserRequest{
		Name:  "Animal Rescue",
		Email: "rescue@example.com",
		ContactPerson: fundraiserContactPerson{
			Name:  "Jane Doe",
			Phone: "+62 812 3456",
		},
		SocialURL: "https://example.com/rescue",
		Country:   "Indonesia",
		ZipCode:   "10110",
	}

	tests := []struct {
		name       string
		request    registerFundraiserRequest
		wantErrors httpx.FieldErrors
	}{
		{name: "accepts valid request", request: valid},
		{
			name:    "returns every required field error",
			request: registerFundraiserRequest{},
			wantErrors: httpx.FieldErrors{
				"name":                {"name is required!"},
				"email":               {"email is required!"},
				"contactPerson.name":  {"contactPerson.name is required!"},
				"contactPerson.phone": {"contactPerson.phone is required!"},
				"socialUrl":           {"socialUrl is required!"},
				"country":             {"country is required!"},
				"zipCode":             {"zipCode is required!"},
			},
		},
		{
			name: "validates formats and schema limits",
			request: registerFundraiserRequest{
				Name:  strings.Repeat("n", 256),
				Email: strings.Repeat("e", 256),
				ContactPerson: fundraiserContactPerson{
					Name:  strings.Repeat("n", 256),
					Phone: strings.Repeat("p", 256),
				},
				SocialURL: "ftp://example.com/profile",
				Country:   strings.Repeat("c", 256),
				ZipCode:   strings.Repeat("z", 21),
				ImageObjectKey: func() *string {
					value := strings.Repeat("i", 1025)
					return &value
				}(),
			},
			wantErrors: httpx.FieldErrors{
				"name":                {"name must not exceed 255 characters!"},
				"email":               {"email must not exceed 255 characters!", "email format is not valid!"},
				"contactPerson.name":  {"contactPerson.name must not exceed 255 characters!"},
				"contactPerson.phone": {"contactPerson.phone must not exceed 255 characters!"},
				"socialUrl":           {"socialUrl must be an absolute HTTP or HTTPS URL!"},
				"country":             {"country must not exceed 255 characters!"},
				"zipCode":             {"zipCode must not exceed 20 characters!"},
				"imageObjectKey":      {"imageObjectKey must not exceed 1024 bytes!"},
			},
		},
		{
			name: "rejects relative social URL",
			request: func() registerFundraiserRequest {
				request := valid
				request.SocialURL = "/rescue"
				return request
			}(),
			wantErrors: httpx.FieldErrors{
				"socialUrl": {"socialUrl must be an absolute HTTP or HTTPS URL!"},
			},
		},
		{
			name: "rejects social URL over length limit",
			request: func() registerFundraiserRequest {
				request := valid
				request.SocialURL = "https://example.com/" + strings.Repeat("x", 2029)
				return request
			}(),
			wantErrors: httpx.FieldErrors{
				"socialUrl": {"socialUrl must not exceed 2048 characters!"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.request.validate(); !reflect.DeepEqual(got, test.wantErrors) {
				t.Errorf("validate() = %#v, want %#v", got, test.wantErrors)
			}
		})
	}
}

func TestReplaceFundraiserProfileRequestNormalizeAndValidate(t *testing.T) {
	var request replaceFundraiserProfileRequest
	if err := json.Unmarshal([]byte(`{
		"name":" Animal Rescue ",
		"email":" RESCUE@EXAMPLE.COM ",
		"imageObjectKey":null,
		"contactPerson":{"name":" Jane Doe ","phone":" +62 812 3456 "},
		"socialUrl":" https://example.com/rescue ",
		"country":" Indonesia ",
		"zipCode":" 10110 "
	}`), &request); err != nil {
		t.Fatalf("decode replacement request: %v", err)
	}

	request.normalize()
	if fieldErrors := request.validate(); fieldErrors != nil {
		t.Fatalf("validate() = %#v, want nil", fieldErrors)
	}
	profile := request.toProfileReplacement()
	if profile.Name != "Animal Rescue" || profile.Email != "rescue@example.com" || profile.ContactName != "Jane Doe" || profile.ContactPhone != "+62 812 3456" || profile.SocialURL != "https://example.com/rescue" || profile.Country != "Indonesia" || profile.ZipCode != "10110" {
		t.Errorf("replacement = %#v", profile)
	}
	if !profile.ImageObjectKey.Set || profile.ImageObjectKey.Value != nil {
		t.Errorf("image update = %#v", profile.ImageObjectKey)
	}
}

func TestReplaceFundraiserProfileRequestValidate(t *testing.T) {
	parse := func(t *testing.T, body string) replaceFundraiserProfileRequest {
		t.Helper()
		var request replaceFundraiserProfileRequest
		if err := json.Unmarshal([]byte(body), &request); err != nil {
			t.Fatalf("decode replacement request: %v", err)
		}
		request.normalize()
		return request
	}

	validBody := `{"name":"Animal Rescue","email":"rescue@example.com","contactPerson":{"name":"Jane Doe","phone":"+62 812 3456"},"socialUrl":"https://example.com/rescue","country":"Indonesia","zipCode":"10110"}`
	tests := []struct {
		name       string
		body       string
		wantErrors httpx.FieldErrors
	}{
		{name: "allows an omitted image to preserve it", body: validBody},
		{
			name: "requires every profile field",
			body: `{}`,
			wantErrors: httpx.FieldErrors{
				"name": {"name is required!"}, "email": {"email is required!"}, "contactPerson.name": {"contactPerson.name is required!"}, "contactPerson.phone": {"contactPerson.phone is required!"}, "socialUrl": {"socialUrl is required!"}, "country": {"country is required!"}, "zipCode": {"zipCode is required!"},
			},
		},
		{
			name: "rejects empty image key when supplied",
			body: strings.TrimSuffix(validBody, "}") + `,"imageObjectKey":" "}`,
			wantErrors: httpx.FieldErrors{
				"imageObjectKey": {"imageObjectKey must not be empty!"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parse(t, test.body).validate(); !reflect.DeepEqual(got, test.wantErrors) {
				t.Errorf("validate() = %#v, want %#v", got, test.wantErrors)
			}
		})
	}
}
