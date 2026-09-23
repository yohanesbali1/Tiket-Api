package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"tiket/internal/queue"
)

type Handler struct {
	svc *queue.Service
}

func New(svc *queue.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Health(c *gin.Context) {
	if err := h.svc.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "redis": "down"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) Join(c *gin.Context) {
	result, err := h.svc.Join(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Status(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "token is required"})
		return
	}

	result, err := h.svc.Status(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type heartbeatRequest struct {
	AccessToken string `json:"access_token" binding:"required"`
}

func (h *Handler) Heartbeat(c *gin.Context) {
	var body heartbeatRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "access_token is required"})
		return
	}

	result, err := h.svc.Heartbeat(c.Request.Context(), body.AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type leaveRequest struct {
	Token string `json:"token" binding:"required"`
}

func (h *Handler) Leave(c *gin.Context) {
	var body leaveRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "token is required"})
		return
	}

	result, err := h.svc.Leave(c.Request.Context(), body.Token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}