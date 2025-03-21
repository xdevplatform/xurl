package errors

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ErrTypeHTTP          = "HTTP Error"
	ErrTypeIO            = "IO Error"
	ErrTypeInvalidMethod = "Invalid Method"
	ErrTypeAPI           = "API Error"
	ErrTypeJSON          = "JSON Error"
	ErrTypeAuth          = "Auth Error"
	ErrTypeTokenStore    = "Token Store Error"
)

type Error struct {
	Type    string
	Message string
	cause   error
}

func (e *Error) Error() string {
	var js json.RawMessage
	if json.Unmarshal([]byte(e.Message), &js) == nil {
		return string(js)
	}

	if e.cause != nil {
		return fmt.Sprintf("%s: %s (cause: %s)", e.Type, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *Error) Unwrap() error {
	return e.cause
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Type == t.Type
}

func NewError(errorType, message string, cause error) *Error {
	return &Error{
		Type:    errorType,
		Message: message,
		cause:   cause,
	}
}

func NewHTTPError(cause error) *Error {
	return NewError(ErrTypeHTTP, cause.Error(), cause)
}

func NewIOError(cause error) *Error {
	return NewError(ErrTypeIO, cause.Error(), cause)
}

func NewInvalidMethodError(method string) *Error {
	return NewError(ErrTypeInvalidMethod, fmt.Sprintf("Invalid HTTP method: %s", method), nil)
}

func NewAPIError(data json.RawMessage) *Error {
	return NewError(ErrTypeAPI, string(data), nil)
}

func NewJSONError(cause error) *Error {
	return NewError(ErrTypeJSON, cause.Error(), cause)
}

func NewAuthError(message string, cause error) *Error {
	return NewError(ErrTypeAuth, message, cause)
}

func NewTokenStoreError(message string) *Error {
	return NewError(ErrTypeTokenStore, message, nil)
}

func IsErrorType(err error, errorType string) bool {
	var e *Error
	if ok := errors.As(err, &e); ok {
		return e.Type == errorType
	}
	return false
}

func IsHTTPError(err error) bool { return IsErrorType(err, ErrTypeHTTP) }
func IsIOError(err error) bool   { return IsErrorType(err, ErrTypeIO) }
func IsAPIError(err error) bool  { return IsErrorType(err, ErrTypeAPI) }
func IsJSONError(err error) bool { return IsErrorType(err, ErrTypeJSON) }
func IsAuthError(err error) bool { return IsErrorType(err, ErrTypeAuth) }
