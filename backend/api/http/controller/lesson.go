package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guide-flow/guide-flow/backend/api/http/request"
	"github.com/guide-flow/guide-flow/backend/api/http/response"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

const maxUploadSize = 10 << 20
const requestIDKey = "request_id"

type LessonController struct {
	service LessonService
	images  LessonImageReader
}

// LessonService 是 Controller 需要的最小服务接口，便于模块测试时注入 Mock。
type LessonService interface {
	CreateLessonWithIdempotency(context.Context, io.Reader, string, domain.LearnerProfile, string) (*domain.Lesson, error)
	GetLesson(context.Context, string) (*domain.Lesson, error)
	ConfirmQuestion(context.Context, string, string) (*domain.Lesson, error)
	RejectQuestion(context.Context, string) (*domain.Lesson, error)
	RetryLesson(context.Context, string) (*domain.Lesson, error)
	RequestTutorial(context.Context, string) (*domain.Lesson, error)
	RetryTutorial(context.Context, string) (*domain.Lesson, error)
	SubmitPractice(context.Context, string, string) (domain.PracticeResult, error)
}

type LessonImageReader interface {
	OpenLessonImage(context.Context, string) (io.ReadCloser, error)
}

func NewLessonController(service LessonService) *LessonController {
	reader, _ := service.(LessonImageReader)
	return &LessonController{service: service, images: reader}
}

// RegisterRoutes 只注册 Lesson 模块路由；新增业务模块时可以采用同样的 Controller 自注册方式。
func (c *LessonController) RegisterRoutes(api *gin.RouterGroup) {
	lessons := api.Group("/lessons")
	lessons.POST("", c.Create)
	lessons.GET("/:id", c.Get)
	lessons.GET("/:id/image", c.Image)
	lessons.POST("/:id/question", c.ConfirmQuestion) // 兼容旧接口
	lessons.POST("/:id/question/confirm", c.ConfirmQuestion)
	lessons.POST("/:id/question/reject", c.RejectQuestion)
	lessons.POST("/:id/retry", c.Retry)
	lessons.POST("/:id/tutorial", c.RequestTutorial)
	lessons.POST("/:id/tutorial/retry", c.RetryTutorial)
	lessons.POST("/:id/practice", c.SubmitPractice)
}

func (c *LessonController) Image(ctx *gin.Context) {
	if c.images == nil {
		ctx.Status(http.StatusNotFound)
		return
	}
	image, err := c.images.OpenLessonImage(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	defer image.Close()
	data, err := io.ReadAll(io.LimitReader(image, maxUploadSize))
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	// 对象存储不一定返回 MIME 类型，这里按文件头探测，保证浏览器能直接预览原题图。
	ctx.Header("Content-Type", http.DetectContentType(data))
	ctx.Header("Cache-Control", "private, max-age=300")
	ctx.Data(http.StatusOK, http.DetectContentType(data), data)
}

func (c *LessonController) Create(ctx *gin.Context) {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxUploadSize)
	f, header, err := ctx.Request.FormFile("image")
	if err != nil {
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		}
		writeErrorWithStatus(ctx, status, err, "")
		return
	}
	defer f.Close()
	form := request.CreateLessonRequest{Grade: 5}
	formProfile := domain.LearnerProfile{Grade: form.Grade}
	if rawProfile := ctx.PostForm("learnerProfile"); rawProfile != "" {
		form.LearnerProfile = rawProfile
		if err := json.Unmarshal([]byte(rawProfile), &formProfile); err != nil {
			writeErrorWithStatus(ctx, http.StatusBadRequest, errors.New("invalid learnerProfile"), "")
			return
		}
	} else {
		formProfile = domain.LearnerProfile{Grade: form.Grade}
		if ctx.PostForm("grade") == "6" {
			formProfile.Grade = 6
		}
	}
	l, err := c.service.CreateLessonWithIdempotency(ctx.Request.Context(), f, header.Filename, formProfile, ctx.GetHeader("Idempotency-Key"))
	if err != nil {
		writeServiceError(ctx, err, "")
		return
	}
	writeLesson(ctx, http.StatusAccepted, l)
}

func (c *LessonController) Get(ctx *gin.Context) {
	l, err := c.service.GetLesson(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	writeLesson(ctx, http.StatusOK, l)
}

func (c *LessonController) ConfirmQuestion(ctx *gin.Context) {
	var req request.ConfirmQuestionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeErrorWithStatus(ctx, http.StatusBadRequest, errors.New("invalid request body"), ctx.Param("id"))
		return
	}
	l, err := c.service.ConfirmQuestion(ctx.Request.Context(), ctx.Param("id"), req.Text)
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	writeLesson(ctx, http.StatusAccepted, l)
}

func (c *LessonController) RejectQuestion(ctx *gin.Context) {
	l, err := c.service.RejectQuestion(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	writeLesson(ctx, http.StatusAccepted, l)
}

func (c *LessonController) Retry(ctx *gin.Context) {
	l, err := c.service.RetryLesson(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	writeLesson(ctx, http.StatusAccepted, l)
}

func (c *LessonController) RequestTutorial(ctx *gin.Context) {
	l, err := c.service.RequestTutorial(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	writeLesson(ctx, http.StatusAccepted, l)
}

func (c *LessonController) RetryTutorial(ctx *gin.Context) {
	l, err := c.service.RetryTutorial(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	writeLesson(ctx, http.StatusAccepted, l)
}

func (c *LessonController) SubmitPractice(ctx *gin.Context) {
	var req request.SubmitPracticeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		writeErrorWithStatus(ctx, http.StatusBadRequest, errors.New("invalid request body"), ctx.Param("id"))
		return
	}
	result, err := c.service.SubmitPractice(ctx.Request.Context(), ctx.Param("id"), req.Answer)
	if err != nil {
		writeServiceError(ctx, err, ctx.Param("id"))
		return
	}
	ctx.JSON(http.StatusOK, response.NewPracticeResultResponse(result))
}

func writeLesson(ctx *gin.Context, status int, l *domain.Lesson) {
	ctx.Header("X-Lesson-ID", l.ID)
	ctx.JSON(status, response.NewLessonResponse(l))
}

func writeServiceError(ctx *gin.Context, err error, lessonID string) {
	writeErrorWithStatus(ctx, statusForError(err), err, lessonID)
}

func writeErrorWithStatus(ctx *gin.Context, status int, err error, lessonID string) {
	code, message, retryable := errorContract(err, status)
	requestID, _ := ctx.Get(requestIDKey)
	ctx.JSON(status, response.ErrorResponse{
		// 对外只返回稳定错误契约，避免把数据库、对象存储或供应商错误细节泄露给客户端。
		Error: message, Code: code, Message: message, RequestID: stringValue(requestID), LessonID: lessonID, Retryable: retryable,
	})
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidTransition), errors.Is(err, domain.ErrNeedsConfirmation), errors.Is(err, domain.ErrNotRetryable), errors.Is(err, domain.ErrRejected), errors.Is(err, domain.ErrNotReady), errors.Is(err, domain.ErrConcurrentUpdate), errors.Is(err, domain.ErrAlreadyExists), errors.Is(err, domain.ErrQuestionInvalid), errors.Is(err, domain.ErrQuestionTooLong), errors.Is(err, domain.ErrDemoOCROnly):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func errorContract(err error, status int) (string, string, bool) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return "lesson_not_found", "Lesson 不存在", false
	case errors.Is(err, domain.ErrInvalidTransition):
		return "invalid_transition", "当前状态不允许执行此操作", false
	case errors.Is(err, domain.ErrNeedsConfirmation):
		return "confirmation_required", "请先确认题目", false
	case errors.Is(err, domain.ErrNotRetryable):
		return "not_retryable", "当前任务不可重试", false
	case errors.Is(err, domain.ErrRejected):
		return "lesson_rejected", "题目已被拒绝", false
	case errors.Is(err, domain.ErrNotReady):
		return "lesson_not_ready", "课程尚未准备完成", true
	case errors.Is(err, domain.ErrConcurrentUpdate):
		return "concurrent_update", "任务刚刚发生变化，请重新查询后再试", true
	case errors.Is(err, domain.ErrAlreadyExists):
		return "already_exists", "资源已存在", false
	case errors.Is(err, domain.ErrQuestionInvalid):
		return "invalid_question", "题目文本不能为空", false
	case errors.Is(err, domain.ErrQuestionTooLong):
		return "question_too_long", "题目文本不能超过 2000 个字符", false
	case errors.Is(err, domain.ErrDemoOCROnly):
		return "ocr_demo_only", "当前仅支持示例题；配置真实 OCR 后才能识别上传的题目", false
	case status == http.StatusBadRequest:
		return "invalid_request", "请求参数不合法", false
	case status == http.StatusRequestEntityTooLarge:
		return "payload_too_large", "上传文件超过大小限制", false
	default:
		return "internal_error", "服务暂时不可用，请稍后重试", true
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
