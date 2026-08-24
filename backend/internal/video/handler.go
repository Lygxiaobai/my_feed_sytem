package video

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"my_feed_system/internal/media"
	"my_feed_system/internal/middleware/ratelimit"
	"my_feed_system/internal/response"
)

type Handler struct {
	service   *Service
	uploadDir string
	media     *media.Service
	limiter   ratelimit.Checker
}

func NewHandler(service *Service, uploadDir string, mediaService *media.Service, limiter ratelimit.Checker) *Handler {
	return &Handler{service: service, uploadDir: uploadDir, media: mediaService, limiter: limiter}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/listByAuthorID", h.ListByAuthorID)
	rg.POST("/getDetail", h.GetDetail)
	rg.POST("/share", h.Share)
	rg.POST("/resolveShare", h.ResolveShare)
}

func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup, uploadMW []gin.HandlerFunc, publishMW []gin.HandlerFunc) {
	rg.POST("/uploadVideo", append(append([]gin.HandlerFunc{}, uploadMW...), h.UploadVideo)...)
	// 建会话和分段都不走「一条视频」限流；配额只在 uploadInit 扣一次。
	rg.POST("/uploadInit", h.UploadInit)
	rg.POST("/uploadPart", h.UploadPart)
	rg.POST("/uploadCover", h.UploadCover)
	rg.POST("/mediaTaskStatus", h.MediaTaskStatus)
	rg.POST("/publish", append(append([]gin.HandlerFunc{}, publishMW...), h.Publish)...)
	rg.POST("/saveDraft", append(append([]gin.HandlerFunc{}, publishMW...), h.SaveDraft)...)
	rg.POST("/listDrafts", h.ListDrafts)
	rg.POST("/unpublish", h.Unpublish)
	rg.POST("/relist", h.Relist)
	rg.POST("/delete", h.Delete)
	rg.POST("/listLiked", h.ListLiked)
}

func (h *Handler) UploadVideo(c *gin.Context) {
	if h.media == nil {
		response.Fail(c, http.StatusServiceUnavailable, response.MiddlewareError, nil)
		return
	}
	// 在 multipart 解析前限制请求体，避免超大文件先被 Gin 写入临时目录。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.media.MaxVideoBytes()+1<<20)
	file, err := c.FormFile("file")
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			response.Fail(c, http.StatusRequestEntityTooLarge, response.UploadTooLarge, err)
			return
		}
		response.FailTip(c, http.StatusBadRequest, response.ParamMissing, "请选择要上传的视频文件", err)
		return
	}

	task, err := h.media.CreateVideoTask(c.GetUint64("account_id"), file)
	if err != nil {
		if errors.Is(err, media.ErrVideoUploadTooLarge) {
			response.Fail(c, http.StatusBadRequest, response.UploadTooLarge, err)
			return
		}
		if errors.Is(err, media.ErrVideoUploadEmpty) {
			response.FailTip(c, http.StatusBadRequest, response.UploadTypeInvalid, "视频文件内容为空", err)
			return
		}
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	// 转码是异步的，用 202 表达「已接收，仍在处理」。
	response.OKWithStatus(c, http.StatusAccepted, gin.H{"task": task})
}

func (h *Handler) UploadInit(c *gin.Context) {
	if h.media == nil {
		response.Fail(c, http.StatusServiceUnavailable, response.MiddlewareError, nil)
		return
	}
	var req struct {
		Total int64 `json:"total"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Total <= 0 {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "上传参数无效", err)
		return
	}
	if !h.enforceUploadQuota(c) {
		return
	}
	partBytes := media.UploadPartBytes
	concurrency := media.UploadPartConcurrency
	// 直连源站时不再绕 Cloudflare，可以用大段多路。地址只来自进程环境，避免 overlay YAML 盖掉。
	origin := strings.TrimRight(strings.TrimSpace(os.Getenv("VIDEO_UPLOAD_ORIGIN")), "/")
	if origin != "" {
		partBytes = media.UploadPartMaxBytes
		concurrency = media.UploadPartDirectConcurrency
	}
	sessionID, count, err := h.media.BeginUpload(c.GetUint64("account_id"), req.Total, partBytes)
	if err != nil {
		h.replyUploadErr(c, err)
		return
	}
	payload := gin.H{
		"session_id":       sessionID,
		"part_bytes":       partBytes,
		"part_concurrency": concurrency,
		"part_count":       count,
	}
	if origin != "" {
		payload["part_origin"] = origin
	}
	response.OK(c, payload)
}

func (h *Handler) UploadPart(c *gin.Context) {
	if h.media == nil {
		response.Fail(c, http.StatusServiceUnavailable, response.MiddlewareError, nil)
		return
	}

	// 会话放请求头：不依赖 multipart 里排在文件后面的字段。
	sessionID := strings.TrimSpace(c.GetHeader("X-Upload-Session"))
	index, indexErr := strconv.Atoi(strings.TrimSpace(c.GetHeader("X-Upload-Index")))
	count, countErr := strconv.Atoi(strings.TrimSpace(c.GetHeader("X-Upload-Count")))
	if sessionID == "" || indexErr != nil || countErr != nil {
		response.FailTip(c, http.StatusBadRequest, response.ParamMissing, "上传已失效，请重新选择视频", nil)
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, media.UploadPartMaxBytes+2<<20)
	file, err := c.FormFile("file")
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			response.Fail(c, http.StatusRequestEntityTooLarge, response.UploadTooLarge, err)
			return
		}
		response.FailTip(c, http.StatusBadRequest, response.ParamMissing, "请选择要上传的视频文件", err)
		return
	}

	src, err := file.Open()
	if err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	defer src.Close()

	received, task, err := h.media.PutUploadPart(c.GetUint64("account_id"), sessionID, index, count, src, file.Size)
	if err != nil {
		h.replyUploadErr(c, err)
		return
	}

	payload := gin.H{"session_id": sessionID, "received": received}
	if task != nil {
		payload["task"] = task
		response.OKWithStatus(c, http.StatusAccepted, payload)
		return
	}
	response.OK(c, payload)
}

// enforceUploadQuota 与路由上 video.upload.* 使用同一把 Redis 钥匙，
// 这样整文件上传和分段上传的第一段共享「12 次 / 10 分钟」的投稿上限。
func (h *Handler) enforceUploadQuota(c *gin.Context) bool {
	if h.limiter == nil {
		return true
	}
	if !h.allowUploadSubject(c, "video.upload.ip", c.ClientIP(), 20) {
		return false
	}
	accountID := c.GetUint64("account_id")
	if accountID == 0 {
		return true
	}
	return h.allowUploadSubject(c, "video.upload.account", strconv.FormatUint(accountID, 10), 12)
}

func (h *Handler) allowUploadSubject(c *gin.Context, scope, subject string, limit int64) bool {
	if subject == "" {
		return true
	}
	result, err := h.limiter.Allow(c.Request.Context(), scope, subject, limit, 10*time.Minute)
	if err != nil {
		return true
	}
	if result.Allowed {
		return true
	}
	response.FailTip(c, http.StatusTooManyRequests, response.RateLimited, "操作过于频繁，请稍后再试", nil)
	return false
}

func (h *Handler) replyUploadErr(c *gin.Context, err error) {
	if errors.Is(err, media.ErrVideoUploadTooLarge) {
		response.Fail(c, http.StatusBadRequest, response.UploadTooLarge, err)
		return
	}
	if errors.Is(err, media.ErrVideoUploadEmpty) {
		response.FailTip(c, http.StatusBadRequest, response.UploadTypeInvalid, "视频文件内容为空", err)
		return
	}
	if errors.Is(err, media.ErrUploadSessionNotFound) {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "上传已失效，请重新选择视频", err)
		return
	}
	if errors.Is(err, media.ErrUploadSessionConflict) {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "上传进度不一致，请重新选择视频", err)
		return
	}
	if errors.Is(err, media.ErrTooManyUploadSessions) {
		response.FailTip(c, http.StatusTooManyRequests, response.RateLimited, "还有未完成的上传，请先取消或稍后再试", err)
		return
	}
	response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
}

func (h *Handler) UploadCover(c *gin.Context) {
	h.uploadFile(c, "file", "covers")
}

func (h *Handler) Publish(c *gin.Context) {
	var req PublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
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
			response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "视频地址无效，请重新上传", err)
			return
		}
		if errors.Is(err, ErrIdempotencyKeyRequired) || errors.Is(err, ErrIdempotencyKeyTooLong) {
			response.Fail(c, http.StatusBadRequest, response.ParamError, err)
			return
		}
		// 同 key 异参、或首个请求仍在处理中时，都对外返回 409，提醒客户端不要盲目复用同一个 key。
		if errors.Is(err, ErrIdempotencyRequestConflict) || errors.Is(err, ErrIdempotencyRequestBusy) {
			response.Fail(c, http.StatusConflict, response.DuplicatedRequest, err)
			return
		}
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{"video": video})
}

func (h *Handler) SaveDraft(c *gin.Context) {
	var req SaveDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}

	video, err := h.service.SaveDraft(c.GetUint64("account_id"), c.GetString("account_username"), req)
	if err != nil {
		h.replyAuthorVideoErr(c, err)
		return
	}
	response.OK(c, gin.H{"video": video})
}

func (h *Handler) ListDrafts(c *gin.Context) {
	videos, err := h.service.ListDrafts(c.GetUint64("account_id"))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}
	response.OK(c, gin.H{"videos": videos})
}

func (h *Handler) Unpublish(c *gin.Context) {
	var req AuthorVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	video, err := h.service.Unpublish(c.Request.Context(), c.GetUint64("account_id"), req)
	if err != nil {
		h.replyAuthorVideoErr(c, err)
		return
	}
	response.OK(c, gin.H{"video": video})
}

func (h *Handler) Relist(c *gin.Context) {
	var req AuthorVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	video, err := h.service.Relist(c.Request.Context(), c.GetUint64("account_id"), req)
	if err != nil {
		h.replyAuthorVideoErr(c, err)
		return
	}
	response.OK(c, gin.H{"video": video})
}

func (h *Handler) Delete(c *gin.Context) {
	var req AuthorVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	if err := h.service.Delete(c.Request.Context(), c.GetUint64("account_id"), req); err != nil {
		h.replyAuthorVideoErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

func (h *Handler) replyAuthorVideoErr(c *gin.Context, err error) {
	if errors.Is(err, ErrInvalidPlayURL) || errors.Is(err, ErrInvalidCoverURL) {
		response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "视频地址无效，请重新上传", err)
		return
	}
	if errors.Is(err, ErrVideoNotFound) {
		response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "视频不存在或已被删除", err)
		return
	}
	if errors.Is(err, ErrNotDraft) {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "只能保存或发布草稿", err)
		return
	}
	if errors.Is(err, ErrDraftLimitReached) {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "草稿已满，请先清理后再保存", err)
		return
	}
	if errors.Is(err, ErrCannotUnpublish) {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "当前状态不能下架", err)
		return
	}
	if errors.Is(err, ErrCannotRelist) {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "当前状态不能重新上架", err)
		return
	}
	response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
}

func (h *Handler) ListByAuthorID(c *gin.Context) {
	var req ListByAuthorIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}

	videos, err := h.service.ListByAuthorID(c.GetUint64("account_id"), req)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{"videos": videos})
}

func (h *Handler) ListLiked(c *gin.Context) {
	videos, err := h.service.ListLiked(c.GetUint64("account_id"))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{"videos": videos})
}

func (h *Handler) GetDetail(c *gin.Context) {
	var req GetDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}

	video, err := h.service.GetDetail(c.GetUint64("account_id"), req)
	if err != nil {
		if errors.Is(err, ErrVideoNotFound) {
			response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "视频不存在或已被删除", err)
			return
		}
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{"video": video})
}

func (h *Handler) Share(c *gin.Context) {
	var req ShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}

	share, err := h.service.Share(c.GetUint64("account_id"), req)
	if err != nil {
		if errors.Is(err, ErrVideoNotFound) {
			response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "视频不存在或已被删除", err)
			return
		}
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{"share": share})
}

func (h *Handler) ResolveShare(c *gin.Context) {
	var req ResolveShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}

	video, err := h.service.ResolveShare(c.GetUint64("account_id"), req)
	if err != nil {
		if errors.Is(err, ErrShareTextTooLong) {
			response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "内容过长，请只粘贴分享口令", err)
			return
		}
		// 口令无法识别、校验位不符、内容已下架，对外统一是「口令无效」：
		// 区分这几种情况会把「该视频确实存在」告诉持有随机口令的人。
		if errors.Is(err, ErrShareCodeNotFound) || errors.Is(err, ErrInvalidShareCode) || errors.Is(err, ErrVideoNotFound) {
			response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "口令无效或内容已下架", err)
			return
		}
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{"video": video})
}

func (h *Handler) MediaTaskStatus(c *gin.Context) {
	var req struct {
		TaskID uint64 `json:"task_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	if h.media == nil {
		response.Fail(c, http.StatusServiceUnavailable, response.MiddlewareError, nil)
		return
	}

	task, err := h.media.GetTask(c.GetUint64("account_id"), req.TaskID)
	if err != nil {
		if errors.Is(err, media.ErrTaskNotFound) {
			response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "上传任务不存在", err)
			return
		}
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{"task": task})
}

func (h *Handler) uploadFile(c *gin.Context, formField string, subDir string) {
	file, err := c.FormFile(formField)
	if err != nil {
		response.FailTip(c, http.StatusBadRequest, response.ParamMissing, "请选择要上传的文件", err)
		return
	}
	if err := NewMediaValidator(h.uploadDir).ValidateUploadedFile(file, subDir); err != nil {
		response.Fail(c, http.StatusBadRequest, response.UploadTypeInvalid, err)
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	filename := fmt.Sprintf("%d_%d%s", c.GetUint64("account_id"), time.Now().UnixNano(), ext)
	targetDir := filepath.Join(h.uploadDir, subDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	targetPath := filepath.Join(targetDir, filename)
	if err := c.SaveUploadedFile(file, targetPath); err != nil {
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{
		"filename": filename,
		"url":      "/static/" + subDir + "/" + filename,
	})
}
