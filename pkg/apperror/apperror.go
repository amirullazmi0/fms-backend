// Package apperror carries a user-safe message plus the HTTP status to answer with.
// Usecases return these for business rule failures; handlers map them straight to
// response.Error. Any other error is treated as an internal failure.
package apperror

import (
	"errors"
	"net/http"
)

type AppError struct {
	Status  int
	Message string
}

func (e *AppError) Error() string { return e.Message }

func New(status int, message string) *AppError {
	return &AppError{Status: status, Message: message}
}

func BadRequest(message string) *AppError { return New(http.StatusBadRequest, message) }
func NotFound(message string) *AppError   { return New(http.StatusNotFound, message) }
func Conflict(message string) *AppError   { return New(http.StatusConflict, message) }
func Forbidden(message string) *AppError  { return New(http.StatusForbidden, message) }
func Unauthorized(message string) *AppError {
	return New(http.StatusUnauthorized, message)
}

// As returns the AppError inside err, if any.
func As(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
