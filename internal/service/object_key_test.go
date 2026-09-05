package service

import (
	"context"
	"errors"
	"testing"
)

type stubObjectPromoter struct {
	err          error
	calls        int
	sourceKey    string
	destKey      string
	discardCalls int
	discardedKey string
}

func (s *stubObjectPromoter) Promote(_ context.Context, sourceKey, destKey string) error {
	s.calls++
	s.sourceKey = sourceKey
	s.destKey = destKey
	return s.err
}

func (s *stubObjectPromoter) Discard(_ context.Context, objectKey string) {
	s.discardCalls++
	s.discardedKey = objectKey
}

func TestCanonicalImageObjectKey(t *testing.T) {
	validID := "0198a123-4567-7abc-8123-456789abcdef"

	tests := []struct {
		name      string
		key       string
		directory string
		want      string
		wantError error
	}{
		{
			name:      "maps staged profile jpeg",
			key:       " tmp/profiles/" + validID + ".jpg ",
			directory: ProfileImageDirectory,
			want:      "profiles/" + validID + ".jpg",
		},
		{
			name:      "maps staged campaign webp",
			key:       "tmp/campaigns/" + validID + ".webp",
			directory: CampaignImageDirectory,
			want:      "campaigns/" + validID + ".webp",
		},
		{
			name:      "rejects canonical profile key",
			key:       "profiles/" + validID + ".png",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
		{
			name:      "rejects campaign key for profile directory",
			key:       "tmp/campaigns/" + validID + ".png",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
		{
			name:      "rejects leading slash",
			key:       "/tmp/profiles/" + validID + ".jpg",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
		{
			name:      "rejects path traversal",
			key:       "tmp/profiles/../" + validID + ".jpg",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
		{
			name:      "rejects extra path segment",
			key:       "tmp/profiles/extra/" + validID + ".jpg",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
		{
			name:      "rejects non-uuid filename",
			key:       "tmp/profiles/cat.png",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
		{
			name:      "rejects unsupported extension",
			key:       "tmp/profiles/" + validID + ".gif",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
		{
			name:      "rejects blank key",
			key:       "  ",
			directory: ProfileImageDirectory,
			wantError: ErrInvalidImageObjectKey,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CanonicalImageObjectKey(test.key, test.directory)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("CanonicalImageObjectKey() error = %v, want %v", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalImageObjectKey() unexpected error: %v", err)
			}
			if got != test.want {
				t.Errorf("CanonicalImageObjectKey() = %q, want %q", got, test.want)
			}
		})
	}
}
