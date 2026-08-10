package api

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/httpx"
)

func TestCreateCampaignRequestNormalizeAndValidate(t *testing.T) {
	request := createCampaignRequest{
		Title:            " Emergency Rescue ",
		ShortDescription: " Help rescued animals ",
		Story:            " A long rescue story. ",
		GoalAmount:       10_000_000_000,
		EndAt:            " 2030-08-10T12:00:00+07:00 ",
		ImageObjectKey:   " campaigns/rescue.png ",
		Country:          " Indonesia ",
		ZipCode:          " 10110 ",
	}

	request.normalize()
	fieldErrors, endAt := request.validate()
	if fieldErrors != nil {
		t.Fatalf("validate() = %#v, want nil", fieldErrors)
	}
	if request.Title != "Emergency Rescue" || request.ShortDescription != "Help rescued animals" || request.Story != "A long rescue story." {
		t.Errorf("normalized content = %#v", request)
	}
	if request.ImageObjectKey != "campaigns/rescue.png" || request.Country != "Indonesia" || request.ZipCode != "10110" {
		t.Errorf("normalized metadata = %#v", request)
	}
	wantEndAt := time.Date(2030, 8, 10, 5, 0, 0, 0, time.UTC)
	if !endAt.Equal(wantEndAt) || endAt.Location() != time.UTC {
		t.Errorf("endAt = %v, want %v UTC", endAt, wantEndAt)
	}
}

func TestCreateCampaignRequestValidate(t *testing.T) {
	valid := createCampaignRequest{
		Title:            "Emergency Rescue",
		ShortDescription: "Help rescued animals",
		Story:            "A long rescue story.",
		GoalAmount:       10_000_000_000,
		EndAt:            "2030-08-10T05:00:00Z",
		ImageObjectKey:   "campaigns/rescue.png",
		Country:          "Indonesia",
		ZipCode:          "10110",
	}
	tests := []struct {
		name       string
		request    createCampaignRequest
		wantErrors httpx.FieldErrors
	}{
		{name: "accepts a valid request", request: valid},
		{
			name:    "returns every required field error",
			request: createCampaignRequest{},
			wantErrors: httpx.FieldErrors{
				"title":            {"title is required!"},
				"shortDescription": {"shortDescription is required!"},
				"story":            {"story is required!"},
				"goalAmount":       {"goalAmount must be greater than 0!"},
				"endAt":            {"endAt is required!"},
				"imageObjectKey":   {"imageObjectKey is required!"},
				"country":          {"country is required!"},
				"zipCode":          {"zipCode is required!"},
			},
		},
		{
			name: "validates formats and database limits",
			request: createCampaignRequest{
				Title:            strings.Repeat("t", 256),
				ShortDescription: strings.Repeat("s", 256),
				Story:            "story",
				GoalAmount:       -1,
				EndAt:            "tomorrow",
				ImageObjectKey:   strings.Repeat("i", 1025),
				Country:          strings.Repeat("c", 256),
				ZipCode:          strings.Repeat("z", 21),
			},
			wantErrors: httpx.FieldErrors{
				"title":            {"title must not exceed 255 characters!"},
				"shortDescription": {"shortDescription must not exceed 255 characters!"},
				"goalAmount":       {"goalAmount must be greater than 0!"},
				"endAt":            {"endAt must be a valid RFC3339 timestamp!"},
				"imageObjectKey":   {"imageObjectKey must not exceed 1024 bytes!"},
				"country":          {"country must not exceed 255 characters!"},
				"zipCode":          {"zipCode must not exceed 20 characters!"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := test.request.validate()
			if !reflect.DeepEqual(got, test.wantErrors) {
				t.Errorf("validate() = %#v, want %#v", got, test.wantErrors)
			}
		})
	}
}
