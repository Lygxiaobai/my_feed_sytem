package video

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service   *Service
	uploadDir string
}

func NewHandler(service *Service, uploadDir string) *Handler {
	return &Handler{service: service, uploadDir: uploadDir}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/listByAuthorID", h.ListByAuthorID)
	rg.POST("/getDetail", h.GetDetail)
}

func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup) {
	rg.POST("/uploadVideo", h.UploadVideo)
	rg.POST("/uploadCover", h.UploadCover)
	rg.POST("/publish", h.Publish)
	rg.POST("/listLiked", h.ListLiked)
}

func (h *Handler) UploadVideo(c *gin.Context) {
	h.uploadFile(c, "file", "videos")
}

func (h *Handler) UploadCover(c *gin.Context) {
	h.uploadFile(c, "file", "covers")
}

func (h *Handler) Publish(c *gin.Context) {
	var req PublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	// 优先使用语义更清晰的请求头；为了兼容旧客户端，再回退到 body 里的 client_request_id。
	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idemKey == "" {
		idemKey = strings.TrimSpace(req.ClientRequestID)
	}

	video, err := h.service.Publish(c.GetUint64("account_id"), c.GetString("account_username"), idemKey, req)
	if err != nil {
		if errors.Is(err, ErrInvalidPlayURL) || errors.Is(err, ErrInvalidCoverURL) {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if errors.Is(err, ErrIdempotencyKeyRequired) || errors.Is(err, ErrIdempotencyKeyTooLong) {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		// 同 key 异参、或首个请求仍在处理中时，都对外返回 409，提醒客户端不要盲目复用同一个 key。
		if errors.Is(err, ErrIdempotencyRequestConflict) || errors.Is(err, ErrIdempotencyRequestBusy) {
			c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "publish video failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "publish success",
		"video":   video,
	})
}

func (h *Handler) ListByAuthorID(c *gin.Context) {
	var req ListByAuthorIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	videos, err := h.service.ListByAuthorID(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "list videos failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"videos": videos})
}

func (h *Handler) ListLiked(c *gin.Context) {
	videos, err := h.service.ListLiked(c.GetUint64("account_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "list liked videos failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"videos": videos})
}

func (h *Handler) GetDetail(c *gin.Context) {
	var req GetDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	video, err := h.service.GetDetail(req)
	if err != nil {
		if errors.Is(err, ErrVideoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "get video detail failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"video": video})
}

func (h *Handler) uploadFile(c *gin.Context, formField string, subDir string) {
	file, err := c.FormFile(formField)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "missing file",
			"error":   err.Error(),
		})
		return
	}
	if err := NewMediaValidator(h.uploadDir).ValidateUploadedFile(file, subDir); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	filename := fmt.Sprintf("%d_%d%s", c.GetUint64("account_id"), time.Now().UnixNano(), ext)
	targetDir := filepath.Join(h.uploadDir, subDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "create upload directory failed",
			"error":   err.Error(),
		})
		return
	}

	targetPath := filepath.Join(targetDir, filename)
	if err := c.SaveUploadedFile(file, targetPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "save file failed",
			"error":   err.Error(),
		})
		return
	}

	urlPath := "/static/" + subDir + "/" + filename
	c.JSON(http.StatusOK, gin.H{
		"message":  "upload success",
		"filename": filename,
		"url":      urlPath,
	})
}
