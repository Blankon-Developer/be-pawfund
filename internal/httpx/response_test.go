package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestNewPagination(t *testing.T) {
	tests := []struct {
		name       string
		current    int64
		pageSize   int64
		totalItems int64
		want       Pagination
	}{
		{
			name:       "computes remaining pages",
			current:    2,
			pageSize:   25,
			totalItems: 47,
			want:       Pagination{Current: 2, PageSize: 25, TotalPages: 2, TotalItems: 47},
		},
		{
			name:       "returns zero pages when empty",
			current:    1,
			pageSize:   10,
			totalItems: 0,
			want:       Pagination{Current: 1, PageSize: 10, TotalPages: 0, TotalItems: 0},
		},
		{
			name:       "keeps current past the last page",
			current:    5,
			pageSize:   10,
			totalItems: 3,
			want:       Pagination{Current: 5, PageSize: 10, TotalPages: 1, TotalItems: 3},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NewPagination(test.current, test.pageSize, test.totalItems)
			if got != test.want {
				t.Errorf("NewPagination() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestWriteResponse(t *testing.T) {
	tests := []struct {
		name           string
		write          func(http.ResponseWriter) error
		wantHTTP       int
		wantStatus     ResponseStatus
		wantCode       string
		wantData       any
		wantPagination *Pagination
		wantErrors     FieldErrors
		wantRawKey     string
		omitRawKey     string
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
			omitRawKey: `"pagination"`,
		},
		{
			name: "writes paginated success envelope",
			write: func(w http.ResponseWriter) error {
				return WriteSuccessWithPagination(
					w,
					http.StatusOK,
					"CAMPAIGNS_RETRIEVED",
					"Campaigns retrieved successfully.",
					[]any{},
					NewPagination(2, 10, 47),
				)
			},
			wantHTTP:       http.StatusOK,
			wantStatus:     StatusSuccess,
			wantCode:       "CAMPAIGNS_RETRIEVED",
			wantData:       []any{},
			wantPagination: &Pagination{Current: 2, PageSize: 10, TotalPages: 5, TotalItems: 47},
			wantRawKey:     `"pagination"`,
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
			omitRawKey: `"pagination"`,
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

			body := response.Body.String()
			if test.wantRawKey != "" && !strings.Contains(body, test.wantRawKey) {
				t.Errorf("body %q does not contain %s", body, test.wantRawKey)
			}
			if test.omitRawKey != "" && strings.Contains(body, test.omitRawKey) {
				t.Errorf("body %q unexpectedly contains %s", body, test.omitRawKey)
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
			if !reflect.DeepEqual(got.Pagination, test.wantPagination) {
				t.Errorf("pagination = %#v, want %#v", got.Pagination, test.wantPagination)
			}
			if !reflect.DeepEqual(got.Errors, test.wantErrors) {
				t.Errorf("errors = %#v, want %#v", got.Errors, test.wantErrors)
			}
		})
	}
}
