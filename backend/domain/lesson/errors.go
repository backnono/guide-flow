package domain

import "errors"

var (
	ErrNotFound           = errors.New("lesson not found")
	ErrInvalidTransition  = errors.New("invalid lesson status transition")
	ErrNeedsConfirmation  = errors.New("question requires confirmation")
	ErrNotRetryable       = errors.New("lesson is not retryable")
	ErrRejected           = errors.New("lesson was rejected")
	ErrNotReady           = errors.New("lesson is not ready")
	ErrAlreadyExists      = errors.New("lesson already exists")
	ErrConcurrentUpdate   = errors.New("lesson was updated by another worker")
	ErrOCRProvider        = errors.New("ocr provider failed")
	ErrOCRUnrecognized    = errors.New("ocr could not recognize a question")
	ErrDemoOCROnly        = errors.New("demo OCR only supports the built-in sample")
	ErrOCRInvalidResponse = errors.New("ocr provider returned an invalid response")
	ErrQuestionInvalid    = errors.New("question text is invalid")
	ErrQuestionTooLong    = errors.New("question text is too long")
)
