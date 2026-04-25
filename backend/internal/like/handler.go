package like

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"my_feed_system/internal/video"
)

// Handler 负责点赞模块的 HTTP 接口。
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterProtectedRoutes 注册需要登录后才能访问的点赞接口。
func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup) {
	rg.POST("/like", h.Like)
	rg.POST("/unlike", h.Unlike)
	rg.POST("/isLiked", h.IsLiked)
	rg.POST("/listLikedVideoIDs", h.ListLikedVideoIDs)
}

func (h *Handler) Like(c *gin.Context) {
	var req LikeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	if err := h.service.Like(c.GetUint64("account_id"), req); err != nil {
		if errors.Is(err, video.ErrVideoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, ErrAlreadyLiked) {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "like failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "like success"})
}

func (h *Handler) Unlike(c *gin.Context) {
	var req LikeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	if err := h.service.Unlike(c.GetUint64("account_id"), req); err != nil {
		if errors.Is(err, video.ErrVideoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, ErrLikeNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "unlike failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "unlike success"})
}

func (h *Handler) IsLiked(c *gin.Context) {
	var req LikeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	isLiked, err := h.service.IsLiked(c.GetUint64("account_id"), req)
	if err != nil {
		if errors.Is(err, video.ErrVideoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "check like failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"is_liked": isLiked})
}

func (h *Handler) ListLikedVideoIDs(c *gin.Context) {
	var req ListLikedVideoIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	ids, err := h.service.ListLikedVideoIDs(c.GetUint64("account_id"), req.VideoIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "list liked video ids failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"video_ids": ids})
}
