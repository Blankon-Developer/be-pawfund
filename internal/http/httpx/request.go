package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

var (
	ErrBodyTooLarge         = errors.New("request body is too large")
	ErrInvalidJSON          = errors.New("request body is not valid JSON")
	ErrUnsupportedMediaType = errors.New("content type is not application/json")
)

func ReadJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ErrUnsupportedMediaType
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return fmt.Errorf("%w: limit is %d bytes", ErrBodyTooLarge, maxBytesError.Limit)
		}
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return fmt.Errorf("%w: limit is %d bytes", ErrBodyTooLarge, maxBytesError.Limit)
		}
		if err == nil {
			return fmt.Errorf("%w: request body must contain one JSON object", ErrInvalidJSON)
		}
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	return nil
}
