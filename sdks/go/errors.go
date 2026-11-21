package kibotos

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError represents an error response returned by the Kibotos v1 API.
type APIError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Status    int            `json:"status"`
	RequestID string         `json:"request_id"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder
	b.WriteString("kibotos: ")
	if e.Status > 0 {
		fmt.Fprintf(&b, "%d ", e.Status)
	}
	if e.Code != "" {
		b.WriteString(e.Code)
		if e.Message != "" {
			b.WriteString(": ")
		}
	}
	if e.Message != "" {
		b.WriteString(e.Message)
	} else if e.Code == "" {
		b.WriteString("API error")
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (request_id=%s)", e.RequestID)
	}
	return b.String()
}

type errorEnvelope struct {
	Error *APIError `json:"error"`
}

func parseAPIError(status int, header http.Header, body []byte) *APIError {
	reqID := header.Get("Request-Id")

	var env errorEnvelope
	if len(body) > 0 && json.Unmarshal(body, &env) == nil && env.Error != nil {
		apiErr := env.Error
		if apiErr.Status == 0 {
			apiErr.Status = status
		}
		if apiErr.RequestID == "" {
			apiErr.RequestID = reqID
		}
		if apiErr.Code == "" {
			apiErr.Code = "http_error"
		}
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(status)
		}
		return apiErr
	}

	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	} else if len(msg) > 512 {
		msg = msg[:512] + "..."
	}

	return &APIError{
		Code:      "http_error",
		Message:   msg,
		Status:    status,
		RequestID: reqID,
		Retryable: isRetryableStatus(status),
	}
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// IsNotFound reports whether err is a Kibotos 404 / not-found error.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status == http.StatusNotFound {
		return true
	}
	switch apiErr.Code {
	case "not_found",
		"workspace_not_found",
		"volume_not_found",
		"exposure_not_found",
		"template_not_found",
		"template_revision_not_found":
		return true
	default:
		return false
	}
}

// IsRetryable reports whether err is marked retryable by the Kibotos API or
// corresponds to a retryable HTTP status.
func IsRetryable(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Retryable || isRetryableStatus(apiErr.Status)
}

// IsRateLimited reports whether err is a 429 rate-limit error.
func IsRateLimited(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusTooManyRequests || apiErr.Code == "rate_limited"
}

// IsWaitTimeout reports whether err is a 408 wait_timeout error.
func IsWaitTimeout(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == "wait_timeout" || apiErr.Status == http.StatusRequestTimeout
}

// IsUnauthenticated reports whether err is a 401 unauthenticated error.
func IsUnauthenticated(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusUnauthorized || apiErr.Code == "unauthenticated"
}

// IsUnexpectedStatus reports whether err is a 409 unexpected_status error from a wait call.
func IsUnexpectedStatus(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == "unexpected_status"
}
