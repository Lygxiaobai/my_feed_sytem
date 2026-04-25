package comment

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"my_feed_system/internal/video"
)

// Handler 负责评论模块的 HTTP 接口。
type Handler struct {
	service *Service
}

// NewHandler 创建评论接口处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes 注册匿名可访问的评论接口。
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/listAll", h.ListAll)
}

// RegisterProtectedRoutes 注册需要登录后才能访问的评论接口。
func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup) {
	rg.POST("/publish", h.Publish)
	rg.POST("/delete", h.Delete)
}

// ListAll 返回某个视频下的根评论及其回复列表。
func (h *Handler) ListAll(c *gin.Context) {
	var req ListAllRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	comments, err := h.service.ListAll(req)
	if err != nil {
		if errors.Is(err, video.ErrVideoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "list comments failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"comments": comments})
}

// Publish 发布一条新评论或回复评论。
func (h *Handler) Publish(c *gin.Context) {
	var req PublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	comment, err := h.service.Publish(c.GetUint64("account_id"), c.GetString("account_username"), req)
	if err != nil {
		if errors.Is(err, video.ErrVideoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, ErrInvalidParentComment) || errors.Is(err, ErrParentCommentMismatch) {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "publish comment failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "publish comment success", "comment": comment})
}

// Delete 删除评论，并在需要时一并移除其回复树。
func (h *Handler) Delete(c *gin.Context) {
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	if err := h.service.Delete(c.GetUint64("account_id"), req); err != nil {
		if errors.Is(err, ErrCommentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, ErrCommentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "delete comment failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "delete comment success"})
}
