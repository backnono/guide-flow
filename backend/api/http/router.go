package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/guide-flow/guide-flow/backend/api/http/controller"
	"github.com/guide-flow/guide-flow/backend/api/http/response"
	"github.com/guide-flow/guide-flow/backend/application/lesson"
	"github.com/guide-flow/guide-flow/backend/application/retrieval"
	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

// NewHandler 负责组装 HTTP 层，不承载具体业务处理。
// 每个业务模块由自己的 Controller 注册路由，后续可以独立增加 User、File、Practice 等模块。
func NewHandler(service *lesson.Service, _ lesson.OcrAdapter, templateServices ...retrieval.TemplateService) http.Handler {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestIDMiddleware())
	router.MaxMultipartMemory = 10 << 20

	api := router.Group("/api")
	controller.NewLessonController(service).RegisterRoutes(api)
	if len(templateServices) > 0 && templateServices[0] != nil {
		controller.NewSolutionTemplateController(templateServices[0]).RegisterRoutes(api)
	}
	controller.NewHealthController().RegisterRoutes(router)

	router.NoRoute(
		func(ctx *gin.Context) {
			requestID, _ := ctx.Get(requestIDKey)
			requestIDText, _ := requestID.(string)
			ctx.JSON(http.StatusNotFound, response.ErrorResponse{
				Error: domain.ErrNotFound.Error(), Code: "route_not_found", Message: "请求路径不存在", RequestID: requestIDText,
			})
		},
	)
	return router
}
