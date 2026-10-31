// Package failure owns fixed public application outcomes, independent of transports.
package failure

import "errors"

type Failure struct {
	Code, Message   string
	Retryable       bool
	CurrentRevision *int64
	recoveredPanic  bool
}

func (e *Failure) Error() string { return e.Message }

var messages = map[string]string{
	"invalid_request":           "The request is invalid",
	"unsupported_api_version":   "The API version is unsupported",
	"unsupported_operation":     "The operation is unsupported",
	"unauthorized":              "Authorization is required",
	"forbidden":                 "This request is forbidden",
	"not_found":                 "The resource was not found",
	"revision_conflict":         "The record changed; read its current state before retrying",
	"idempotency_conflict":      "The idempotency key was used for another request",
	"message_identity_conflict": "The message identity was used for different content",
	"terminal_resolution":       "The message already has a terminal resolution",
	"payload_too_large":         "The request is too large",
	"unsupported_media_type":    "The media type is unsupported",
	"internal_error":            "The workspace could not be read",
	"service_unavailable":       "The workspace is unavailable",
	"response_too_large":        "The workspace response is too large",
}

func New(code string) *Failure {
	message, ok := messages[code]
	if !ok {
		code = "internal_error"
		message = messages[code]
	}
	return &Failure{Code: code, Message: message, Retryable: code == "service_unavailable" || code == "response_too_large"}
}

// RecoveredPanic preserves only a fixed internal classification, never the
// recovered value or a stack. Public DTO mapping still exposes internal_error.
func RecoveredPanic() *Failure {
	f := New("internal_error")
	f.recoveredPanic = true
	return f
}
func IsRecoveredPanic(err error) bool {
	var f *Failure
	return errors.As(err, &f) && f.Code == "internal_error" && f.recoveredPanic
}

// Safe ignores caller-provided text and only retains valid conflict metadata.
func Safe(f *Failure) *Failure {
	result := New(f.Code)
	result.recoveredPanic = result.Code == "internal_error" && f.recoveredPanic
	if result.Code == "revision_conflict" && f.CurrentRevision != nil && *f.CurrentRevision > 0 && *f.CurrentRevision <= 9007199254740991 {
		revision := *f.CurrentRevision
		result.CurrentRevision = &revision
	}
	if result.Code == "revision_conflict" && result.CurrentRevision == nil {
		return New("internal_error")
	}
	return result
}
