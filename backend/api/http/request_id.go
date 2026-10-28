package http

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDKey = "request_id"

func requestIDMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requestID := ctx.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		ctx.Set(requestIDKey, requestID)
		ctx.Header("X-Request-ID", requestID)
		ctx.Next()
	}
}
