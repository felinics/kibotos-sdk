package cli

import (
	"errors"
	"fmt"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"

	"github.com/felinics/kibotos-sdk/cli/internal/auth"
)

const (
	// ExitOK indicates successful command execution.
	ExitOK = 0
	// ExitError indicates an API or runtime error.
	ExitError = 1
	// ExitUsage indicates a CLI usage, argument, or flag validation error.
	ExitUsage = 2
	// ExitUnauthenticated indicates missing, invalid, or expired authentication credentials.
	ExitUnauthenticated = 3
)

// CLIError wraps an error with an explicit CLI exit code and machine-readable code.
type CLIError struct {
	ExitCode int
	Code     string
	Message  string
	Hint     string
	Err      error
}

func (e *CLIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

func usageError(format string, args ...any) *CLIError {
	return &CLIError{
		ExitCode: ExitUsage,
		Code:     "invalid_usage",
		Message:  fmt.Sprintf(format, args...),
	}
}

func wrapUsageError(err error) *CLIError {
	if err == nil {
		return nil
	}
	var ce *CLIError
	if errors.As(err, &ce) {
		return ce
	}
	return &CLIError{
		ExitCode: ExitUsage,
		Code:     "invalid_usage",
		Message:  err.Error(),
		Err:      err,
	}
}

func unauthenticatedError(msg, hint string) *CLIError {
	return &CLIError{
		ExitCode: ExitUnauthenticated,
		Code:     "unauthenticated",
		Message:  msg,
		Hint:     hint,
	}
}

type jsonErrorEnvelope struct {
	Error jsonErrorDetail `json:"error"`
}

type jsonErrorDetail struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	ExitCode  int            `json:"exit_code"`
	Status    int            `json:"status,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Retryable bool           `json:"retryable,omitempty"`
	Hint      string         `json:"hint,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// classifyError converts any error into an exit code and structured detail payload.
func classifyError(err error) (int, jsonErrorDetail) {
	if err == nil {
		return ExitOK, jsonErrorDetail{}
	}

	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		code := cliErr.Code
		if code == "" {
			code = "error"
		}
		return cliErr.ExitCode, jsonErrorDetail{
			Code:     code,
			Message:  cliErr.Error(),
			ExitCode: cliErr.ExitCode,
			Hint:     cliErr.Hint,
		}
	}

	var apiErr *kibotos.APIError
	if errors.As(err, &apiErr) {
		exitCode := ExitError
		hint := ""
		if kibotos.IsUnauthenticated(apiErr) {
			exitCode = ExitUnauthenticated
			hint = "Run `kibo login` or set KIBOTOS_API_KEY to authenticate."
		}
		code := apiErr.Code
		if code == "" {
			code = "api_error"
		}
		return exitCode, jsonErrorDetail{
			Code:      code,
			Message:   apiErr.Message,
			ExitCode:  exitCode,
			Status:    apiErr.Status,
			RequestID: apiErr.RequestID,
			Retryable: apiErr.Retryable,
			Hint:      hint,
			Details:   apiErr.Details,
		}
	}

	var devErr *auth.DeviceAuthError
	if errors.As(err, &devErr) {
		code := devErr.Code
		if code == "" {
			code = "device_auth_error"
		}
		return ExitError, jsonErrorDetail{
			Code:     code,
			Message:  devErr.Error(),
			ExitCode: ExitError,
			Status:   devErr.Status,
		}
	}

	if isCobraUsageError(err) {
		return ExitUsage, jsonErrorDetail{
			Code:     "invalid_usage",
			Message:  err.Error(),
			ExitCode: ExitUsage,
		}
	}

	return ExitError, jsonErrorDetail{
		Code:     "error",
		Message:  err.Error(),
		ExitCode: ExitError,
	}
}

func isCobraUsageError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	patterns := []string{
		"unknown command ",
		"unknown flag:",
		"unknown shorthand flag:",
		"flag needs an argument:",
		"invalid argument ",
		"required flag(s) ",
		"accepts ",
		"requires at least ",
		"requires at most ",
		"accepts at most ",
		"accepts between ",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
