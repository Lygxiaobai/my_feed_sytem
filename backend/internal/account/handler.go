package account

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler 负责账号模块的 HTTP 接口编排。
type Handler struct {
	service *Service
}

// NewHandler 创建账号接口处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes 注册无需登录即可访问的账号接口。
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/register", h.Register)
	rg.POST("/login", h.Login)
	rg.POST("/findByID", h.FindByID)
	rg.POST("/findByUsername", h.FindByUsername)
}

// RegisterProtectedRoutes 注册需要登录后才能访问的账号接口。
func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup) {
	rg.GET("/me", h.Me)
	rg.POST("/logout", h.Logout)
	rg.POST("/changePassword", h.ChangePassword)
	rg.POST("/rename", h.Rename)
}

// Register 处理用户注册请求。
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	account, err := h.service.Register(req)
	if err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			c.JSON(http.StatusBadRequest, gin.H{
				"message": err.Error(),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "register failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "register success",
		"account": gin.H{
			"id":       account.ID,
			"username": account.Username,
		},
	})
}

// Login 校验用户名密码并签发 JWT。
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	account, err := h.service.Login(req)
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"message": err.Error(),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "login failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "login success",
		"account": gin.H{
			"id":       account.Account.ID,
			"username": account.Account.Username,
		},
		"token": account.Token,
	})
}

// FindByID 根据账号 ID 查询公开资料。
func (h *Handler) FindByID(c *gin.Context) {
	var req FindByIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	account, err := h.service.FindByID(req)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "find account failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account": gin.H{
			"id":       account.ID,
			"username": account.Username,
		},
	})
}

// FindByUsername 根据用户名查询公开资料。
func (h *Handler) FindByUsername(c *gin.Context) {
	var req FindByUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	account, err := h.service.FindByUsername(req)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "find account failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account": gin.H{
			"id":       account.ID,
			"username": account.Username,
		},
	})
}

// Me 返回当前 JWT 对应的账号信息。
func (h *Handler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "authorized",
		"account": gin.H{
			"id":       c.GetUint64("account_id"),
			"username": c.GetString("account_username"),
		},
	})
}

// Logout 清空数据库中的 token，实现服务端登出。
func (h *Handler) Logout(c *gin.Context) {
	accountID := c.GetUint64("account_id")
	if err := h.service.Logout(accountID); err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "logout failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "logout success"})
}

// ChangePassword 校验旧密码并更新密码，同时使旧 token 失效。
func (h *Handler) ChangePassword(c *gin.Context) {
	accountID := c.GetUint64("account_id")
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	if err := h.service.ChangePassword(accountID, req); err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, ErrInvalidCredential) {
			c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "change password failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "change password success, please login again"})
}

// Rename 修改用户名并重新签发 token，保证 JWT 中的用户名同步更新。
func (h *Handler) Rename(c *gin.Context) {
	accountID := c.GetUint64("account_id")
	var req RenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	result, err := h.service.Rename(accountID, req)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, ErrUsernameTaken) {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "rename failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "rename success",
		"account": gin.H{
			"id":       result.Account.ID,
			"username": result.Account.Username,
		},
		"token": result.Token,
	})
}
