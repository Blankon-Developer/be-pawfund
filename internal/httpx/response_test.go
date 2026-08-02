package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestWriteResponse(t *testing.T) {
	tests := []struct {
		name       string
		write      func(http.ResponseWriter) error
		wantHTTP   int
		wantStatus ResponseStatus
		wantCode   string
		wantData   any
		wantErrors FieldErrors
	}{
		{
			name: "writes success envelope",
			write: func(w http.ResponseWriter) error {
				return WriteSuccess(w, http.StatusCreated, "CREATED", "Created.", map[string]any{"id": "one"})
			},
			wantHTTP:   http.StatusCreated,
			wantStatus: StatusSuccess,
			wantCode:   "CREATED",
			wantData:   map[string]any{"id": "one"},
		},
		{
			name: "writes error envelope",
			write: func(w http.ResponseWriter) error {
				return WriteError(
					w,
					http.StatusUnprocessableEntity,
					"VALIDATION_ERROR",
					"Invalid.",
					FieldErrors{"email": {"email is required!"}},
				)
			},
			wantHTTP:   http.StatusUnprocessableEntity,
			wantStatus: StatusError,
			wantCode:   "VALIDATION_ERROR",
			wantErrors: FieldErrors{"email": {"email is required!"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			if err := test.write(response); err != nil {
				t.Fatalf("write response: %v", err)
			}

			if response.Code != test.wantHTTP {
				t.Errorf("HTTP status = %d, want %d", response.Code, test.wantHTTP)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}

			var got Response
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.Status != test.wantStatus || got.Code != test.wantCode {
				t.Errorf("response status/code = %q/%q, want %q/%q", got.Status, got.Code, test.wantStatus, test.wantCode)
			}
			if !reflect.DeepEqual(got.Data, test.wantData) {
				t.Errorf("data = %#v, want %#v", got.Data, test.wantData)
			}
			if !reflect.DeepEqual(got.Errors, test.wantErrors) {
				t.Errorf("errors = %#v, want %#v", got.Errors, test.wantErrors)
			}
		})
	}
}
