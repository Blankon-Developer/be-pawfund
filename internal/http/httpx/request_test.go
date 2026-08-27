package httpx

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadJSON(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		maxBytes    int64
		want        string
		wantError   error
	}{
		{
			name:        "decodes one JSON object",
			contentType: "application/json; charset=utf-8",
			body:        `{"name":"Pawfund"}`,
			maxBytes:    1024,
			want:        "Pawfund",
		},
		{
			name:      "requires content type",
			body:      `{"name":"Pawfund"}`,
			maxBytes:  1024,
			wantError: ErrUnsupportedMediaType,
		},
		{
			name:        "rejects malformed JSON",
			contentType: "application/json",
			body:        `{"name":`,
			maxBytes:    1024,
			wantError:   ErrInvalidJSON,
		},
		{
			name:        "rejects unknown fields",
			contentType: "application/json",
			body:        `{"unknown":true}`,
			maxBytes:    1024,
			wantError:   ErrInvalidJSON,
		},
		{
			name:        "rejects multiple JSON values",
			contentType: "application/json",
			body:        `{"name":"one"} {"name":"two"}`,
			maxBytes:    1024,
			wantError:   ErrInvalidJSON,
		},
		{
			name:        "rejects a large body",
			contentType: "application/json",
			body:        `{"name":"too large"}`,
			maxBytes:    8,
			wantError:   ErrBodyTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			var payload struct {
				Name string `json:"name"`
			}

			err := ReadJSON(response, request, &payload, test.maxBytes)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ReadJSON() error = %v, want %v", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadJSON() unexpected error: %v", err)
			}
			if payload.Name != test.want {
				t.Errorf("decoded name = %q, want %q", payload.Name, test.want)
			}
		})
	}
}
