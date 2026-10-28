package response

type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	LessonID  string `json:"lessonId,omitempty"`
	Retryable bool   `json:"retryable"`
}
