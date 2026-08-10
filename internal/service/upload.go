package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	MinProfileImageSize int64 = 1
	MaxProfileImageSize int64 = 5 << 20
)

var (
	ErrUnsupportedProfileImageType = errors.New("unsupported profile image content type")
	ErrInvalidProfileImageSize     = errors.New("invalid profile image size")
)

type ProfileImagePutPresigner interface {
	PresignPut(ctx context.Context, objectKey, contentType string, size int64) (string, error)
}

type PresignProfileImageInput struct {
	ContentType string
	Size        int64
}

type PresignProfileImageResult struct {
	ObjectKey string
	URL       string
}

type PresignCampaignImageInput = PresignProfileImageInput
type PresignCampaignImageResult = PresignProfileImageResult

type UploadService struct {
	presigner  ProfileImagePutPresigner
	generateID IDGenerator
}

func NewUploadService(presigner ProfileImagePutPresigner, generateID IDGenerator) *UploadService {
	if generateID == nil {
		generateID = uuid.NewV7
	}
	return &UploadService{presigner: presigner, generateID: generateID}
}

func (s *UploadService) PresignProfileImage(
	ctx context.Context,
	input PresignProfileImageInput,
) (PresignProfileImageResult, error) {
	return s.presignImage(ctx, input, "profiles", "profile")
}

func (s *UploadService) PresignCampaignImage(
	ctx context.Context,
	input PresignCampaignImageInput,
) (PresignCampaignImageResult, error) {
	return s.presignImage(ctx, input, "campaigns", "campaign")
}

func (s *UploadService) presignImage(
	ctx context.Context,
	input PresignProfileImageInput,
	directory string,
	imageKind string,
) (PresignProfileImageResult, error) {
	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	extension, err := profileImageExtension(contentType)
	if err != nil {
		return PresignProfileImageResult{}, err
	}
	if input.Size < MinProfileImageSize || input.Size > MaxProfileImageSize {
		return PresignProfileImageResult{}, ErrInvalidProfileImageSize
	}

	id, err := s.generateID()
	if err != nil {
		return PresignProfileImageResult{}, fmt.Errorf("service: generate %s image ID: %w", imageKind, err)
	}
	objectKey := directory + "/" + id.String() + extension
	presignedURL, err := s.presigner.PresignPut(ctx, objectKey, contentType, input.Size)
	if err != nil {
		return PresignProfileImageResult{}, fmt.Errorf("service: presign %s image upload: %w", imageKind, err)
	}

	return PresignProfileImageResult{ObjectKey: objectKey, URL: presignedURL}, nil
}

func profileImageExtension(contentType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", ErrUnsupportedProfileImageType
	}
}
