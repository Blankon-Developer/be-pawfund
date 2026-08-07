package httpx

import (
	"errors"
	"log/slog"
	"net/http"
)

// Responder writes standard success/error JSON responses on behalf of a
// handler, logging a failure if the response itself could not be written.
// Embed it in a handler struct to avoid re-implementing this boilerplate
// for every endpoint.
type Responder struct {
	Logger *slog.Logger
}

func NewResponder(logger *slog.Logger) Responder {
	return Responder{Logger: logger}
}

// Success writes a success response.
func (r Responder) Success(w http.ResponseWriter, status int, code, message string, data any) {
	if err := WriteSuccess(w, status, code, message, data); err != nil {
		r.Logger.Error("write success response", "code", code, "error", err)
	}
}

// Error writes an error response.
func (r Responder) Error(w http.ResponseWriter, status int, code, message string, fieldErrors FieldErrors) {
	if err := WriteError(w, status, code, message, fieldErrors); err != nil {
		r.Logger.Error("write error response", "code", code, "error", err)
	}
}

// ValidationError writes a 422 response for one or more invalid fields.
func (r Responder) ValidationError(w http.ResponseWriter, fieldErrors FieldErrors) {
	r.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "One or more fields are invalid.", fieldErrors)
}

// InternalError writes a generic 500 response.
func (r Responder) InternalError(w http.ResponseWriter) {
	r.Error(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "An internal server error occurred.", nil)
}

// ReadError maps an error returned by ReadJSON to the matching error
// response. bodyLimitMessage should describe the endpoint's specific body
// size limit, e.g. "Request body exceeds the 1 MiB limit."
func (r Responder) ReadError(w http.ResponseWriter, err error, bodyLimitMessage string) {
	switch {
	case errors.Is(err, ErrUnsupportedMediaType):
		r.Error(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json.", nil)
	case errors.Is(err, ErrBodyTooLarge):
		r.Error(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", bodyLimitMessage, nil)
	default:
		r.Logger.Debug("invalid request body", "error", err)
		r.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Request body must contain one valid JSON object.", nil)
	}
}
