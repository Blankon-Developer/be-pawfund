package storage

import (
	"strings"
	"testing"
)

func TestNewPublicURLBuilder(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		wantError string
	}{
		{name: "accepts HTTP URL", baseURL: "http://localhost:9000/pawfund"},
		{name: "accepts HTTPS URL", baseURL: "https://cdn.example.com/profiles/"},
		{name: "rejects relative URL", baseURL: "/pawfund", wantError: "absolute HTTP or HTTPS"},
		{name: "rejects unsupported scheme", baseURL: "s3://pawfund", wantError: "absolute HTTP or HTTPS"},
		{name: "rejects query", baseURL: "https://cdn.example.com?token=x", wantError: "query or fragment"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewPublicURLBuilder(test.baseURL)
			if test.wantError == "" && err != nil {
				t.Fatalf("NewPublicURLBuilder() unexpected error: %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("NewPublicURLBuilder() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestPublicURLBuilderBuild(t *testing.T) {
	builder, err := NewPublicURLBuilder("https://cdn.example.com/pawfund/")
	if err != nil {
		t.Fatalf("create builder: %v", err)
	}

	tests := []struct {
		name      string
		objectKey *string
		want      *string
	}{
		{name: "returns nil for nil key"},
		{name: "returns nil for blank key", objectKey: stringPointer("   ")},
		{name: "appends nested key", objectKey: stringPointer("profiles/cat.png"), want: stringPointer("https://cdn.example.com/pawfund/profiles/cat.png")},
		{name: "escapes path segments", objectKey: stringPointer("profiles/cat photo#.png"), want: stringPointer("https://cdn.example.com/pawfund/profiles/cat%20photo%23.png")},
		{name: "escapes dot segments", objectKey: stringPointer("../cat.png"), want: stringPointer("https://cdn.example.com/pawfund/%2E%2E/cat.png")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := builder.Build(test.objectKey)
			if !equalStringPointers(got, test.want) {
				t.Errorf("Build() = %v, want %v", pointerValue(got), pointerValue(test.want))
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
