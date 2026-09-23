package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"tiket/internal/handler"
)

func Setup(r *gin.Engine, h *handler.Handler) {
	r.GET("/health", h.Health)

	api := r.Group("/api/queue")
	{
		api.POST("/join", h.Join)
		api.GET("/status", h.Status)
		api.POST("/heartbeat", h.Heartbeat)
		api.POST("/leave", h.Leave)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"message": "not found"})
	})
}
