package errors

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

const (
	UnknownCode    = http.StatusInternalServerError
	UnknownReason  = ""
	UnknownMessage = "internal error"
)

// MetadataKeyRetryAfter 是 429 类错误在 Metadata 中携带重试秒数的保留键。
// response.ErrorFrom 会把它落到 Retry-After 响应头（RFC 6585）。
const MetadataKeyRetryAfter = "retry_after_seconds"

type Status struct {
	Code     int32             `json:"code"`
	Reason   string            `json:"reason,omitempty"`
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ApplicationError is the standard error type used to control HTTP responses.
//
// Code is expected to be an HTTP status code (e.g. 400/401/403/404/409/500).
type ApplicationError struct {
	Status
	cause error
}

// Error is kept for backwards compatibility within this package.
type Error = ApplicationError

func (e *ApplicationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause == nil {
		return fmt.Sprintf("error: code=%d reason=%q message=%q metadata=%v", e.Code, e.Reason, e.Message, e.Metadata)
	}
	return fmt.Sprintf("error: code=%d reason=%q message=%q metadata=%v cause=%v", e.Code, e.Reason, e.Message, e.Metadata, e.cause)
}

// Unwrap provides compatibility for Go 1.13 error chains.
func (e *ApplicationError) Unwrap() error { return e.cause }

// Is matches each error in the chain with the target value.
func (e *ApplicationError) Is(err error) bool {
	if se := new(ApplicationError); errors.As(err, &se) {
		return se.Code == e.Code && se.Reason == e.Reason
	}
	return false
}

// WithCause attaches the underlying cause of the error.
func (e *ApplicationError) WithCause(cause error) *ApplicationError {
	err := Clone(e)
	err.cause = cause
	return err
}

// WithMetadata deep-copies the given metadata map.
func (e *ApplicationError) WithMetadata(md map[string]string) *ApplicationError {
	err := Clone(e)
	if md == nil {
		err.Metadata = nil
		return err
	}
	err.Metadata = make(map[string]string, len(md))
	for k, v := range md {
		err.Metadata[k] = v
	}
	return err
}

// WithRetryAfter 返回携带 Retry-After 秒数元数据的克隆（仅接受正整数）。
func (e *ApplicationError) WithRetryAfter(seconds int) *ApplicationError {
	if seconds <= 0 {
		return e
	}
	return e.WithMetadata(map[string]string{MetadataKeyRetryAfter: strconv.Itoa(seconds)})
}

// RetryAfter 从错误链中读取 Retry-After 秒数；不存在或非法时返回 false。
func RetryAfter(err error) (int, bool) {
	var appErr *ApplicationError
	if !errors.As(err, &appErr) || appErr == nil {
		return 0, false
	}
	raw, ok := appErr.Metadata[MetadataKeyRetryAfter]
	if !ok {
		return 0, false
	}
	seconds, parseErr := strconv.Atoi(raw)
	if parseErr != nil || seconds <= 0 {
		return 0, false
	}
	return seconds, true
}

// New returns an error object for the code, message.
func New(code int, reason, message string) *ApplicationError {
	return &ApplicationError{
		Status: Status{
			Code:    int32(code),
			Message: message,
			Reason:  reason,
		},
	}
}

// Newf New(code fmt.Sprintf(format, a...))
func Newf(code int, reason, format string, a ...any) *ApplicationError {
	return New(code, reason, fmt.Sprintf(format, a...))
}

// Errorf returns an error object for the code, message and error info.
func Errorf(code int, reason, format string, a ...any) error {
	return New(code, reason, fmt.Sprintf(format, a...))
}

// Code returns the http code for an error.
// It supports wrapped errors.
func Code(err error) int {
	if err == nil {
		return http.StatusOK
	}
	return int(FromError(err).Code)
}

// Reason returns the reason for a particular error.
// It supports wrapped errors.
func Reason(err error) string {
	if err == nil {
		return UnknownReason
	}
	return FromError(err).Reason
}

// Message returns the message for a particular error.
// It supports wrapped errors.
func Message(err error) string {
	if err == nil {
		return ""
	}
	return FromError(err).Message
}

// Clone deep clone error to a new error.
func Clone(err *ApplicationError) *ApplicationError {
	if err == nil {
		return nil
	}
	var metadata map[string]string
	if err.Metadata != nil {
		metadata = make(map[string]string, len(err.Metadata))
		for k, v := range err.Metadata {
			metadata[k] = v
		}
	}
	return &ApplicationError{
		cause: err.cause,
		Status: Status{
			Code:     err.Code,
			Reason:   err.Reason,
			Message:  err.Message,
			Metadata: metadata,
		},
	}
}

// FromError tries to convert an error to *ApplicationError.
// It supports wrapped errors.
func FromError(err error) *ApplicationError {
	if err == nil {
		return nil
	}
	if se := new(ApplicationError); errors.As(err, &se) {
		return se
	}

	// Fall back to a generic internal error.
	return New(UnknownCode, UnknownReason, UnknownMessage).WithCause(err)
}
