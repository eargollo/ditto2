package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ListResponse is the standard paginated list envelope.
type ListResponse[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ErrorBody is the standard error envelope.
type ErrorBody struct {
	Error APIError `json:"error"`
}

// APIError holds a machine-readable code and a human message.
type APIError struct {
	Code     string        `json:"code"`
	Message  string        `json:"message"`
	Failures []interface{} `json:"failures,omitempty"`
}

// writeJSON serialises v as JSON with status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode", "error", err)
	}
}

// writeError writes a standard error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorBody{
		Error: APIError{Code: code, Message: message},
	})
}
