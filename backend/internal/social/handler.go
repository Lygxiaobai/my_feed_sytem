package social

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"my_feed_system/internal/account"
)

// Handler 负责关注模块的 HTTP 接口。
type Handler struct {
	service *Service
}

// NewHandler 创建关注接口处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes 预留匿名可访问接口；当前关注模块暂无匿名接口。
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
}

// RegisterProtectedRoutes 注册需要登录后才能访问的关注接口。
func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup) {
	rg.POST("/follow", h.Follow)
	rg.POST("/unfollow", h.Unfollow)
	rg.POST("/getAllFollowers", h.GetAllFollowers)
	rg.POST("/getAllVloggers", h.GetAllVloggers)
}

// Follow 为当前登录用户创建关注关系。
func (h *Handler) Follow(c *gin.Context) {
	var req FollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	if err := h.service.Follow(c.GetUint64("account_id"), req); err != nil {
		if errors.Is(err, ErrCannotFollowSelf) || errors.Is(err, ErrAlreadyFollowed) {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, account.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "follow failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "follow success"})
}

// Unfollow 删除当前登录用户的关注关系。
func (h *Handler) Unfollow(c *gin.Context) {
	var req FollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	if err := h.service.Unfollow(c.GetUint64("account_id"), req); err != nil {
		if errors.Is(err, ErrFollowNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "unfollow failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "unfollow success"})
}

// GetAllFollowers 返回粉丝列表；未传 vlogger_id 时默认查询当前登录用户。
func (h *Handler) GetAllFollowers(c *gin.Context) {
	var req GetAllFollowersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	if req.VloggerID == 0 {
		req.VloggerID = c.GetUint64("account_id")
	}

	relations, err := h.service.GetAllFollowers(req.VloggerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "get followers failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"followers": relations})
}

// GetAllVloggers 返回关注列表；未传 follower_id 时默认查询当前登录用户。
func (h *Handler) GetAllVloggers(c *gin.Context) {
	var req GetAllVloggersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request", "error": err.Error()})
		return
	}

	if req.FollowerID == 0 {
		req.FollowerID = c.GetUint64("account_id")
	}

	relations, err := h.service.GetAllVloggers(req.FollowerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "get vloggers failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"vloggers": relations})
}
