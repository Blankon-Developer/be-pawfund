package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
)

func TestRegisterFundraiserRequestNormalize(t *testing.T) {
	imageKey := " profiles/rescue.png "
	request := RegisterFundraiserRequest{
		Name:  " Animal Rescue ",
		Email: " RESCUE@EXAMPLE.COM ",
		ContactPerson: FundraiserContactPerson{
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
	valid := RegisterFundraiserRequest{
		Name:  "Animal Rescue",
		Email: "rescue@example.com",
		ContactPerson: FundraiserContactPerson{
			Name:  "Jane Doe",
			Phone: "+62 812 3456",
		},
		SocialURL: "https://example.com/rescue",
		Country:   "Indonesia",
		ZipCode:   "10110",
	}

	tests := []struct {
		name       string
		request    RegisterFundraiserRequest
		wantErrors httpx.FieldErrors
	}{
		{name: "accepts valid request", request: valid},
		{
			name:    "returns every required field error",
			request: RegisterFundraiserRequest{},
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
			request: RegisterFundraiserRequest{
				Name:  strings.Repeat("n", 256),
				Email: strings.Repeat("e", 256),
				ContactPerson: FundraiserContactPerson{
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
			request: func() RegisterFundraiserRequest {
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
			request: func() RegisterFundraiserRequest {
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
