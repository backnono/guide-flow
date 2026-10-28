package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/guide-flow/guide-flow/backend/api/http/request"
	"github.com/guide-flow/guide-flow/backend/application/retrieval"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

type SolutionTemplateController struct{ service retrieval.TemplateService }

func NewSolutionTemplateController(service retrieval.TemplateService) *SolutionTemplateController {
	return &SolutionTemplateController{service: service}
}

func (c *SolutionTemplateController) RegisterRoutes(api *gin.RouterGroup) {
	templates := api.Group("/solution-templates")
	templates.POST("", c.Create)
	templates.POST("/:id/publish", c.Publish)
}

func (c *SolutionTemplateController) Create(ctx *gin.Context) {
	var req request.CreateSolutionTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "模板请求格式不合法"})
		return
	}
	template := solutiondomain.Template{
		ID:              req.ID,
		Version:         req.Version,
		Name:            req.Name,
		Topics:          req.Topics,
		KnowledgePoints: req.KnowledgePoints,
		Formula:         req.Formula,
		Applicability:   req.Applicability,
		VariableSchema:  req.VariableSchema,
		Steps:           req.Steps,
		CommonMistakes:  req.CommonMistakes,
	}
	if err := c.service.Create(ctx.Request.Context(), template); err != nil {
		writeTemplateError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"id": template.ID, "version": template.Version, "active": false})
}

func (c *SolutionTemplateController) Publish(ctx *gin.Context) {
	var req request.PublishSolutionTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		writeTemplateError(ctx, retrieval.ErrTemplateInvalid)
		return
	}
	if err := c.service.Publish(ctx.Request.Context(), ctx.Param("id"), req.Version); err != nil {
		writeTemplateError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"id": ctx.Param("id"), "version": req.Version, "active": true})
}

func writeTemplateError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "template_store_error"
	message := "模板服务暂时不可用"
	if errors.Is(err, retrieval.ErrTemplateInvalid) {
		status, code, message = http.StatusBadRequest, "invalid_template", "模板内容不合法"
	}
	if errors.Is(err, retrieval.ErrTemplateStoreUnavailable) {
		status, code, message = http.StatusNotImplemented, "template_store_unavailable", "当前环境未配置模板存储"
	}
	ctx.JSON(status, gin.H{"code": code, "message": message})
}
