package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Blankon-Developer/be-pawfund/internal/infra/storage"
	"github.com/google/uuid"
)

const (
	stagingPrefix          = "tmp/"
	ProfileImageDirectory  = "profiles"
	CampaignImageDirectory = "campaigns"
)

var profileImageTypes = []struct {
	contentType string
	extension   string
}{
	{"image/jpeg", ".jpg"},
	{"image/png", ".png"},
	{"image/webp", ".webp"},
}

func stagingImageObjectKey(directory, id, extension string) string {
	return stagingPrefix + directory + "/" + id + extension
}

// CanonicalImageObjectKey requires expectedDirectory to be profiles or campaigns.
func CanonicalImageObjectKey(stagingKey, expectedDirectory string) (string, error) {
	key := strings.TrimSpace(stagingKey)
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return "", ErrInvalidImageObjectKey
	}

	prefix := stagingPrefix + expectedDirectory + "/"
	if !strings.HasPrefix(key, prefix) {
		return "", ErrInvalidImageObjectKey
	}

	rest := strings.TrimPrefix(key, prefix)
	if rest == "" || strings.Contains(rest, "/") {
		return "", ErrInvalidImageObjectKey
	}

	extension := profileImageExtensionFromName(rest)
	if extension == "" {
		return "", ErrInvalidImageObjectKey
	}
	idPart := strings.TrimSuffix(rest, extension)
	if _, err := uuid.Parse(idPart); err != nil {
		return "", ErrInvalidImageObjectKey
	}

	return expectedDirectory + "/" + rest, nil
}

func profileImageExtension(contentType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(contentType))
	for _, imageType := range profileImageTypes {
		if imageType.contentType == normalized {
			return imageType.extension, nil
		}
	}
	return "", ErrUnsupportedProfileImageType
}

func profileImageExtensionFromName(name string) string {
	for _, imageType := range profileImageTypes {
		if strings.HasSuffix(name, imageType.extension) {
			return imageType.extension
		}
	}
	return ""
}

func promoteImageObjectKey(
	ctx context.Context,
	promoter ObjectPromoter,
	stagingKey string,
	directory string,
) (string, error) {
	canonical, err := CanonicalImageObjectKey(stagingKey, directory)
	if err != nil {
		return "", err
	}
	if promoter == nil {
		return canonical, nil
	}

	if err := promoter.Promote(ctx, strings.TrimSpace(stagingKey), canonical); err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return "", ErrImageObjectNotFound
		}
		return "", fmt.Errorf("service: promote image object: %w", err)
	}
	return canonical, nil
}

func discardStagingImage(ctx context.Context, promoter ObjectPromoter, stagingKey *string) {
	if promoter == nil || stagingKey == nil {
		return
	}
	key := strings.TrimSpace(*stagingKey)
	if key == "" {
		return
	}

	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	promoter.Discard(cleanupContext, key)
}

func promoteOptionalImageObjectKey(
	ctx context.Context,
	promoter ObjectPromoter,
	stagingKey *string,
	directory string,
) (*string, error) {
	normalized := normalizeOptionalString(stagingKey)
	if normalized == nil {
		return nil, nil
	}

	canonical, err := promoteImageObjectKey(ctx, promoter, *normalized, directory)
	if err != nil {
		return nil, err
	}
	return &canonical, nil
}
