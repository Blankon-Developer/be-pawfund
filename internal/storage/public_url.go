package storage

import (
	"fmt"
	"net/url"
	"strings"
)

type PublicURLBuilder struct {
	baseURL string
}

func NewPublicURLBuilder(rawBaseURL string) (*PublicURLBuilder, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil {
		return nil, fmt.Errorf("storage: parse public base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("storage: public base URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("storage: public base URL cannot contain a query or fragment")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""

	return &PublicURLBuilder{baseURL: strings.TrimRight(parsed.String(), "/")}, nil
}

func (b *PublicURLBuilder) Build(objectKey *string) *string {
	if objectKey == nil {
		return nil
	}

	key := strings.TrimSpace(*objectKey)
	if key == "" {
		return nil
	}

	segments := strings.Split(key, "/")
	for i, segment := range segments {
		switch segment {
		case ".":
			segments[i] = "%2E"
		case "..":
			segments[i] = "%2E%2E"
		default:
			segments[i] = url.PathEscape(segment)
		}
	}

	publicURL := b.baseURL + "/" + strings.Join(segments, "/")
	return &publicURL
}
