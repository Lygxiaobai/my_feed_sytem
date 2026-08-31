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
	"my_feed_system/internal/storage"
)

type Handler struct {
	service *Service
	store   storage.ObjectStore
	media   *media.Service
	limiter ratelimit.Checker
}

func NewHandler(service *Service, store storage.ObjectStore, mediaService *media.Service, limiter ratelimit.Checker) *Handler {
	return &Handler{service: service, store: store, media: mediaService, limiter: limiter}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/listByAuthorID", h.ListByAuthorID)
	rg.POST("/getDetail", h.GetDetail)
	rg.POST("/share", h.Share)
	rg.POST("/resolveShare", h.ResolveShare)
}

func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup, uploadMW []gin.HandlerFunc, publishMW []gin.HandlerFunc) {
	rg.POST("/uploadVideo", append(append([]gin.HandlerFunc{}, uploadMW...), h.UploadVideo)...)
	// 建会话和拼装都不走「一条视频」限流；配额只在 uploadInit 扣一次。
	// 分片字节直传对象存储，不再经过后端，因此没有 uploadPart 这个路由。
	rg.POST("/uploadInit", h.UploadInit)
	rg.POST("/uploadComplete", h.UploadComplete)
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

	task, err := h.media.CreateVideoTask(c.Request.Context(), c.GetUint64("account_id"), file)
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

// UploadInit 开一次分片直传，返回每一段的预签名 URL。
// 之后浏览器把字节直接 PUT 给对象存储，后端不再经手视频内容。
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

	// 直连灰云时不经 Cloudflare，可以多开几路。
	// 分片尺寸不再随通道变化：S3 对非末尾分片有 5MiB 硬下限，
	// 低于它整单会以 EntityTooSmall 作废，没有为通道让步的余地。
	concurrency := media.UploadPartConcurrency
	if strings.TrimSpace(os.Getenv("VIDEO_UPLOAD_ORIGIN")) != "" {
		concurrency = media.UploadPartDirectConcurrency
	}

	result, err := h.media.InitMultipart(c.Request.Context(), c.GetUint64("account_id"), req.Total, media.UploadPartBytes)
	if err != nil {
		h.replyUploadErr(c, err)
		return
	}

	response.OK(c, gin.H{
		"upload_id":        result.UploadID,
		"object_key":       result.ObjectKey,
		"part_urls":        result.PartURLs,
		"part_bytes":       result.PartBytes,
		"part_count":       result.PartCount,
		"part_concurrency": concurrency,
	})
}

// UploadComplete 收下客户端攒齐的分片 ETag，拼装成源文件并登记转码任务。
func (h *Handler) UploadComplete(c *gin.Context) {
	if h.media == nil {
		response.Fail(c, http.StatusServiceUnavailable, response.MiddlewareError, nil)
		return
	}
	var req struct {
		UploadID  string         `json:"upload_id"`
		ObjectKey string         `json:"object_key"`
		Parts     []storage.Part `json:"parts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UploadID == "" || req.ObjectKey == "" || len(req.Parts) == 0 {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "上传已失效，请重新选择视频", err)
		return
	}

	task, err := h.media.CompleteMultipart(c.Request.Context(), c.GetUint64("account_id"), req.ObjectKey, req.UploadID, req.Parts)
	if err != nil {
		h.replyUploadErr(c, err)
		return
	}

	// 转码是异步的，用 202 表达「已接收，仍在处理」。
	response.OKWithStatus(c, http.StatusAccepted, gin.H{"task": task})
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
	if errors.Is(err, media.ErrUploadKeyRejected) {
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "上传已失效，请重新选择视频", err)
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

// uploadFile 处理封面这类小文件：字节仍经过后端（要校验魔数），但落点是媒体桶。
func (h *Handler) uploadFile(c *gin.Context, formField string, subDir string) {
	if h.store == nil {
		response.Fail(c, http.StatusServiceUnavailable, response.MiddlewareError, nil)
		return
	}
	file, err := c.FormFile(formField)
	if err != nil {
		response.FailTip(c, http.StatusBadRequest, response.ParamMissing, "请选择要上传的文件", err)
		return
	}
	if err := NewMediaValidator().ValidateUploadedFile(file, subDir); err != nil {
		response.Fail(c, http.StatusBadRequest, response.UploadTypeInvalid, err)
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	filename := fmt.Sprintf("%d_%d%s", c.GetUint64("account_id"), time.Now().UnixNano(), ext)
	key := subDir + "/" + filename

	src, err := file.Open()
	if err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	defer src.Close()

	if err := h.store.PutMedia(c.Request.Context(), key, src, file.Size, contentTypeForExt(ext)); err != nil {
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
		return
	}

	response.OK(c, gin.H{
		"filename": filename,
		"url":      "/static/" + key,
	})
}

// contentTypeForExt 让对象带上正确的 Content-Type：
// 浏览器直接从对象存储取封面，类型错了会当成下载而不是图片渲染。
func contentTypeForExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
