package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime/multipart"
	"path"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/storage"
)

const defaultMaxVideoBytes int64 = 256 << 20

// UploadPartMinBytes 是 S3 分片协议对非末尾分片的硬性下限。
// 低于它时 CompleteMultipartUpload 会整单返回 EntityTooSmall，
// 已传的字节全部作废，所以分片尺寸只能在这条线之上取。
const UploadPartMinBytes int64 = 5 << 20

// UploadPartBytes 是默认分片尺寸。取 8MiB 而不是贴着 5MiB 的下限：
// 留出余量，且本机入站约 1.5MB/s 时单段约 5.6 秒，
// 即使 4 路并行分摊带宽也远在 Cloudflare 约 100 秒的回源窗口之内。
const UploadPartBytes int64 = 8 << 20

// UploadPartConcurrency 是经 Cloudflare 时的并行路数；直连源站时用 Direct 那个。
const UploadPartConcurrency = 2

// UploadPartDirectConcurrency 是直连灰云时的并行路数。
const UploadPartDirectConcurrency = 4

// presignTTL 覆盖一次完整上传的时长。256MB 在 1.5MB/s 下约需 3 分钟，
// 留到 2 小时是为了容纳慢网络和重试，与改造前的会话 TTL 保持一致。
const presignTTL = 2 * time.Hour

const sourcePrefix = "sources/"

var (
	ErrVideoUploadEmpty    = errors.New("video file is empty")
	ErrVideoUploadTooLarge = errors.New("video file is too large")
	ErrUploadKeyRejected   = errors.New("upload key does not belong to this account")
)

type Service struct {
	db            *gorm.DB
	repo          *Repo
	outboxRepo    *outbox.Repo
	store         storage.ObjectStore
	maxVideoBytes int64
}

func NewService(db *gorm.DB, store storage.ObjectStore, maxVideoBytes int64) *Service {
	if maxVideoBytes <= 0 {
		maxVideoBytes = defaultMaxVideoBytes
	}
	return &Service{
		db:            db,
		repo:          NewRepo(db),
		outboxRepo:    outbox.NewRepo(db),
		store:         store,
		maxVideoBytes: maxVideoBytes,
	}
}

// InitResult 是分片直传的开场白：客户端拿着这些 URL 直接把字节推给对象存储，
// 全程不经过后端。
type InitResult struct {
	UploadID  string   `json:"upload_id"`
	ObjectKey string   `json:"object_key"`
	PartURLs  []string `json:"part_urls"`
	PartBytes int64    `json:"part_bytes"`
	PartCount int      `json:"part_count"`
}

// InitMultipart 建立一次分片上传，并为每一段签好直传 URL。
func (s *Service) InitMultipart(ctx context.Context, accountID uint64, totalSize int64, partBytes int64) (*InitResult, error) {
	if totalSize <= 0 {
		return nil, ErrVideoUploadEmpty
	}
	if totalSize > s.maxVideoBytes {
		return nil, ErrVideoUploadTooLarge
	}
	partBytes = normalizePartBytes(partBytes)

	count := partCountForSize(totalSize, partBytes)
	if count <= 0 {
		return nil, ErrVideoUploadEmpty
	}

	key := sourceKey(accountID)
	uploadID, err := s.store.CreateMultipart(ctx, key, "application/octet-stream")
	if err != nil {
		return nil, err
	}

	urls := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		signed, err := s.store.PresignPart(ctx, key, uploadID, i, presignTTL)
		if err != nil {
			// 已开的分片上传不必显式回收：Silo 的 stale_uploads_expiry 会兜底，
			// 但能立刻放掉就不要留给后台扫描。
			_ = s.store.AbortMultipart(ctx, key, uploadID)
			return nil, err
		}
		urls = append(urls, signed)
	}

	return &InitResult{
		UploadID:  uploadID,
		ObjectKey: key,
		PartURLs:  urls,
		PartBytes: partBytes,
		PartCount: count,
	}, nil
}

// CompleteMultipart 拼装分片并登记转码任务。
//
// objectKey 来自客户端，必须校验它确实属于当前账号，否则 A 可以拿着 B 的
// key 去拼装。key 里带 accountID 段，比对这一段即可；uploadID 本身也不可猜。
func (s *Service) CompleteMultipart(ctx context.Context, accountID uint64, objectKey string, uploadID string, parts []storage.Part) (*Task, error) {
	if strings.TrimSpace(uploadID) == "" || len(parts) == 0 {
		return nil, ErrVideoUploadEmpty
	}
	if !ownsSourceKey(accountID, objectKey) {
		return nil, ErrUploadKeyRejected
	}

	size, err := s.store.CompleteMultipart(ctx, objectKey, uploadID, parts)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		_ = s.store.RemoveUpload(ctx, objectKey)
		return nil, ErrVideoUploadEmpty
	}
	// 分片各自直传，后端没有经手字节，真实大小只能在拼装后回查。
	if size > s.maxVideoBytes {
		_ = s.store.RemoveUpload(ctx, objectKey)
		return nil, ErrVideoUploadTooLarge
	}

	return s.enqueueVideoTask(ctx, accountID, objectKey)
}

// CreateVideoTask 是小文件的整传兜底入口，字节仍经过后端。
func (s *Service) CreateVideoTask(ctx context.Context, accountID uint64, file *multipart.FileHeader) (*Task, error) {
	if file == nil || file.Size <= 0 {
		return nil, ErrVideoUploadEmpty
	}
	if file.Size > s.maxVideoBytes {
		return nil, ErrVideoUploadTooLarge
	}

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open uploaded video: %w", err)
	}
	defer src.Close()

	key := sourceKey(accountID)
	if err := s.store.PutUpload(ctx, key, src, file.Size, "application/octet-stream"); err != nil {
		return nil, err
	}

	task, err := s.enqueueVideoTask(ctx, accountID, key)
	if err != nil {
		_ = s.store.RemoveUpload(ctx, key)
		return nil, err
	}
	return task, nil
}

func (s *Service) enqueueVideoTask(ctx context.Context, accountID uint64, sourceKey string) (*Task, error) {
	task := &Task{
		AccountID:   accountID,
		SourceKey:   sourceKey,
		ContentType: "video/mp4",
		Status:      StatusProcessing,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.Create(tx, task); err != nil {
			return err
		}
		event, err := mq.NewEnvelope(mq.EventTypeMediaTranscodeRequested, mq.ProducerAPIServer, mq.MediaTranscodePayload{
			TaskID:    task.ID,
			AccountID: accountID,
		})
		if err != nil {
			return fmt.Errorf("build media.transcode.requested event: %w", err)
		}
		if err := s.outboxRepo.Enqueue(tx, event); err != nil {
			return fmt.Errorf("enqueue media.transcode.requested event: %w", err)
		}
		return nil
	}); err != nil {
		_ = s.store.RemoveUpload(ctx, sourceKey)
		return nil, err
	}

	return task, nil
}

func (s *Service) GetTask(accountID uint64, taskID uint64) (*Task, error) {
	return s.repo.FindByIDForAccount(taskID, accountID)
}

func (s *Service) MaxVideoBytes() int64 {
	return s.maxVideoBytes
}

// normalizePartBytes 把分片尺寸夹在 S3 下限之上。
// 客户端传来的值只是建议，低于下限时一律抬回默认值——
// 让步的代价是整单 EntityTooSmall，不值得迁就。
func normalizePartBytes(partBytes int64) int64 {
	if partBytes < UploadPartMinBytes {
		return UploadPartBytes
	}
	return partBytes
}

func partCountForSize(totalSize int64, partBytes int64) int {
	if totalSize <= 0 || partBytes <= 0 {
		return 0
	}
	return int((totalSize + partBytes - 1) / partBytes)
}

// sourceKey 形如 sources/{accountID}/{随机}.source。
// 把 accountID 放进路径，拼装时才能校验归属。
func sourceKey(accountID uint64) string {
	return fmt.Sprintf("%s%d/%s.source", sourcePrefix, accountID, randomSuffix())
}

func ownsSourceKey(accountID uint64, key string) bool {
	if !strings.HasPrefix(key, sourcePrefix) {
		return false
	}
	// 只认 sources/{id}/{name} 这一种形状，挡掉 ../ 和多层前缀。
	rest := strings.TrimPrefix(key, sourcePrefix)
	owner, name, found := strings.Cut(rest, "/")
	if !found || name == "" || name != path.Base(name) {
		return false
	}
	parsed, err := strconv.ParseUint(owner, 10, 64)
	return err == nil && parsed == accountID
}

func randomSuffix() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
