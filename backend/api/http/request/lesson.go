package request

// CreateLessonRequest 描述创建 Lesson 时的表单字段；图片本身由 multipart 文件字段承载。
type CreateLessonRequest struct {
	Grade          int    `form:"grade"`
	LearnerProfile string `form:"learnerProfile"`
}

type ConfirmQuestionRequest struct {
	Text string `json:"text" binding:"required"`
}

type SubmitPracticeRequest struct {
	Answer string `json:"answer"`
}
