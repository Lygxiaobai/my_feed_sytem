package video

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"my_feed_system/internal/idempotency"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
)

var (
	ErrVideoNotFound              = errors.New("video not found")
	ErrIdempotencyKeyRequired     = errors.New("idempotency key is required")
	ErrIdempotencyKeyTooLong      = errors.New("idempotency key is too long")
	ErrIdempotencyRequestConflict = errors.New("idempotency key already used with different request")
	ErrIdempotencyRequestBusy     = errors.New("request with the same idempotency key is still processing")
)

const (
	videoPublishBizType     = "video.publish"
	maxIdempotencyKeyLength = 128
)

// Service encapsulates video publish and query workflows.
type Service struct {
	db              *gorm.DB
	repo            *Repo
	idempotencyRepo *idempotency.Repo
	outboxRepo      *outbox.Repo
	popularity      *popularity.Service
	detailCache     *DetailCache
	localDetail     *LocalDetailCache
	publisher       *mq.Publisher
	mediaValidator  MediaValidator
	detailGroup     singleflight.Group
}

func NewService(db *gorm.DB, popularityService *popularity.Service, uploadDir string) *Service {
	return NewServiceWithCachesAndPublisher(db, popularityService, nil, nil, nil, uploadDir)
}

func NewServiceWithDetailCache(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, uploadDir string) *Service {
	return NewServiceWithCachesAndPublisher(db, popularityService, detailCache, nil, nil, uploadDir)
}

func NewServiceWithDetailCacheAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, publisher *mq.Publisher, uploadDir string) *Service {
	return NewServiceWithCachesAndPublisher(db, popularityService, detailCache, nil, publisher, uploadDir)
}

func NewServiceWithCachesAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, localDetail *LocalDetailCache, publisher *mq.Publisher, uploadDir string) *Service {
	return &Service{
		db:              db,
		repo:            NewRepo(db),
		idempotencyRepo: idempotency.NewRepo(db),
		outboxRepo:      outbox.NewRepo(db),
		popularity:      popularityService,
		detailCache:     detailCache,
		localDetail:     localDetail,
		publisher:       publisher,
		mediaValidator:  NewMediaValidator(uploadDir),
	}
}

// Publish creates a video exactly once for the same idempotency key and payload.
func (s *Service) Publish(accountID uint64, username string, idemKey string, req PublishRequest) (*Video, error) {
	normalizedIdemKey, err := normalizeIdempotencyKey(idemKey)
	if err != nil {
		return nil, err
	}

	playURL, coverURL, err := s.mediaValidator.NormalizePublishURLs(req.PlayURL, req.CoverURL)
	if err != nil {
		return nil, err
	}

	requestHash, err := buildPublishRequestHash(req.Title, req.Description, playURL, coverURL)
	if err != nil {
		return nil, fmt.Errorf("build publish request hash: %w", err)
	}

	var (
		video      *Video
		createdNew bool
	)

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		idemRow := &idempotency.Key{
			AccountID:   accountID,
			BizType:     videoPublishBizType,
			IdemKey:     normalizedIdemKey,
			RequestHash: requestHash,
			Status:      idempotency.StatusProcessing,
		}

		inserted, err := s.idempotencyRepo.CreateProcessing(tx, idemRow)
		if err != nil {
			return err
		}

		if !inserted {
			existing, err := s.idempotencyRepo.FindByScope(tx, accountID, videoPublishBizType, normalizedIdemKey)
			if err != nil {
				return err
			}
			if existing.RequestHash != requestHash {
				return ErrIdempotencyRequestConflict
			}

			video, err = s.replayPublishedVideo(existing)
			if err != nil {
				return err
			}
			if video == nil {
				return ErrIdempotencyRequestBusy
			}
			return nil
		}

		createdNew = true
		video = &Video{
			AuthorID:    accountID,
			Username:    username,
			Title:       req.Title,
			Description: req.Description,
			PlayURL:     playURL,
			CoverURL:    coverURL,
			Popularity:  int64(popularity.PublishWeight),
		}

		if err := s.repo.Create(tx, video); err != nil {
			return err
		}

		event, err := mq.NewEnvelope(mq.EventTypeVideoTimelinePush, mq.ProducerAPIServer, mq.VideoTimelinePayload{
			VideoID:   video.ID,
			AuthorID:  video.AuthorID,
			CreatedAt: video.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("build timeline outbox event: %w", err)
		}
		if err := s.outboxRepo.Enqueue(tx, event); err != nil {
			return fmt.Errorf("enqueue timeline outbox event: %w", err)
		}

		responseVideo := *video
		if s.popularity != nil {
			responseVideo.Popularity = int64(popularity.PublishWeight)
			video.Popularity = responseVideo.Popularity
		}

		responseJSON, err := json.Marshal(responseVideo)
		if err != nil {
			return err
		}

		return s.idempotencyRepo.MarkDone(tx, idemRow.ID, video.ID, string(responseJSON))
	}); err != nil {
		return nil, err
	}

	if createdNew && s.popularity != nil {
		_ = s.popularity.Record(context.Background(), video.ID, popularity.PublishWeight, video.CreatedAt)
		video.Popularity = int64(popularity.PublishWeight)
	}
	if createdNew {
		s.invalidateDetailCache(video.ID)
	}

	return video, nil
}

func (s *Service) ListByAuthorID(req ListByAuthorIDRequest) ([]Video, error) {
	videos, err := s.repo.FindByAuthorID(req.AuthorID)
	if err != nil {
		return nil, err
	}

	videos = s.mediaValidator.FilterPlayable(videos)
	return s.decoratePopularity(context.Background(), videos)
}

func (s *Service) ListLiked(accountID uint64) ([]Video, error) {
	videos, err := s.repo.FindLikedByAccountID(accountID)
	if err != nil {
		return nil, err
	}

	videos = s.mediaValidator.FilterPlayable(videos)
	return s.decoratePopularity(context.Background(), videos)
}

// GetDetail 按 “L1 -> L2 -> DB” 的顺序读取视频详情，并用 singleflight 合并同一个 videoID 的并发回源。
func (s *Service) GetDetail(req GetDetailRequest) (*Video, error) {
	ctx := context.Background()

	// 先在 singleflight 之外查一次缓存，让已经命中的请求直接返回，避免不必要地进入合并逻辑。
	if cachedVideo, ok, notFound, err := s.getDetailFromCaches(ctx, req.ID, true); err != nil {
		log.Printf("video service: read detail cache failed for video %d: %v", req.ID, err)
	} else if ok {
		if notFound {
			return nil, ErrVideoNotFound
		}
		return cachedVideo, nil
	}

	result, err, shared := s.detailGroup.Do(strconv.FormatUint(req.ID, 10), func() (any, error) {
		// 进入 singleflight 后再查一次缓存，避免等待期间已有其他请求把结果回填好了。
		if cachedVideo, ok, notFound, err := s.getDetailFromCaches(ctx, req.ID, false); err == nil && ok {
			return detailLoadResult{video: cachedVideo, notFound: notFound}, nil
		}

		startedAt := time.Now()

		// 两级缓存都未命中后，才真正回源 MySQL。
		video, err := s.repo.FindByID(req.ID)
		if err != nil {
			return nil, err
		}
		if video == nil || !s.mediaValidator.IsPlayable(*video) {
			// 对不存在或不可播放的视频写短负缓存，降低后续同类请求再次穿透到 DB。
			s.setDetailNotFound(req.ID)
			observability.ObserveCacheLoadSeconds(observability.CacheVideoDetail, time.Since(startedAt).Seconds())
			return detailLoadResult{notFound: true}, nil
		}

		if s.popularity != nil {
			// 详情主体来自 MySQL，但热度分数优先从热度服务补齐，避免依赖持久化字段的新鲜度。
			scores, err := s.popularity.Scores(ctx, []uint64{video.ID}, time.Time{})
			if err == nil {
				video.Popularity = scores[video.ID]
			}
		}

		payload, err := json.Marshal(video)
		if err != nil {
			return nil, err
		}

		// DB 回源成功后同时回填 L2/L1，后续请求就能直接命中缓存。
		s.setDetailCaches(video.ID, payload)
		observability.ObserveCacheLoadSeconds(observability.CacheVideoDetail, time.Since(startedAt).Seconds())
		return detailLoadResult{video: video}, nil
	})
	if err != nil {
		return nil, err
	}
	if shared {
		// shared=true 表示这次结果被多个并发请求复用，可用于观察击穿保护效果。
		observability.IncCacheSingleflightShared(observability.CacheVideoDetail)
	}

	loadResult, ok := result.(detailLoadResult)
	if !ok {
		return nil, fmt.Errorf("unexpected detail load result type %T", result)
	}
	if loadResult.notFound {
		return nil, ErrVideoNotFound
	}

	// 到这里要么是自己回源成功，要么是复用了其他并发请求产出的缓存/回源结果。
	return loadResult.video, nil
}

// decoratePopularity bulk loads popularity scores for a video list.
func (s *Service) decoratePopularity(ctx context.Context, videos []Video) ([]Video, error) {
	if s.popularity == nil || len(videos) == 0 {
		return videos, nil
	}

	videoIDs := make([]uint64, 0, len(videos))
	for _, item := range videos {
		videoIDs = append(videoIDs, item.ID)
	}

	scores, err := s.popularity.Scores(ctx, videoIDs, time.Time{})
	if err != nil {
		return nil, err
	}

	for i := range videos {
		videos[i].Popularity = scores[videos[i].ID]
	}

	return videos, nil
}

func (s *Service) replayPublishedVideo(row *idempotency.Key) (*Video, error) {
	if row == nil {
		return nil, nil
	}

	if row.ResponseJSON != "" {
		var item Video
		if err := json.Unmarshal([]byte(row.ResponseJSON), &item); err == nil && item.ID != 0 {
			return &item, nil
		}
	}

	if row.Status != idempotency.StatusDone || row.ResourceID == 0 {
		return nil, nil
	}

	video, err := s.repo.FindByID(row.ResourceID)
	if err != nil {
		return nil, err
	}
	if video == nil {
		return nil, nil
	}
	if s.popularity != nil && video.Popularity == 0 {
		video.Popularity = int64(popularity.PublishWeight)
	}

	return video, nil
}

type detailLoadResult struct {
	video    *Video
	notFound bool
}

// getDetailFromCaches 只负责读取 L1/L2 缓存，不做 DB 回源。
func (s *Service) getDetailFromCaches(ctx context.Context, videoID uint64, recordMetrics bool) (*Video, bool, bool, error) {
	// 这个函数只查缓存，不回源 DB。
	// 返回值含义：
	// 1. *Video：命中正常详情时返回对象；命中 not-found 或未命中时为 nil。
	// 2. bool(ok)：是否命中缓存。只要任一层明确返回结果，就视为 true。
	// 3. bool(notFound)：命中的是否是“短负缓存”，true 表示该视频当前被认为不存在或不可播放。
	// 4. error：缓存访问或反序列化过程中出现的异常。
	if s.localDetail != nil {
		// 先查进程内 L1。本地缓存命中时延最低，也能挡住同实例的热点重复访问。
		cachedVideo, notFound, ok, err := s.localDetail.Get(videoID)
		if err != nil {
			// 本地缓存读失败通常意味着内容损坏，直接删除，避免后续请求反复命中坏数据。
			s.localDetail.Delete(videoID)
			if recordMetrics {
				observability.IncCacheL1Miss(observability.CacheVideoDetail)
			}
		} else if ok { //命中
			if recordMetrics {
				observability.IncCacheL1Hit(observability.CacheVideoDetail)
			}
			if notFound {
				// 命中短负缓存，说明最近已经确认过这个视频不存在或不可播放。
				return nil, true, true, nil
			}
			if s.mediaValidator.IsPlayable(*cachedVideo) {
				// 即使命中缓存，也要确认底层媒体文件仍然可用，避免返回脏数据。
				return cachedVideo, true, false, nil
			}
			// 详情对象还在，但媒体资源已经失效，这条 L1 缓存不再可信。
			s.localDetail.Delete(videoID)
		} else if recordMetrics {
			observability.IncCacheL1Miss(observability.CacheVideoDetail)
		}
	}

	if s.detailCache == nil {
		// 没有 Redis L2 时，明确告诉上层“缓存未命中”，后续由上层决定是否回源。
		return nil, false, false, nil
	}

	// L2 中存的是序列化后的 bytes，而不是共享指针对象，能降低对象被意外修改的风险。
	payload, ok, err := s.detailCache.GetRaw(ctx, videoID)
	if err != nil {
		if recordMetrics {
			observability.IncCacheL2Miss(observability.CacheVideoDetail)
		}
		return nil, false, false, err
	}
	if !ok {
		if recordMetrics {
			observability.IncCacheL2Miss(observability.CacheVideoDetail)
		}
		return nil, false, false, nil
	}
	if isDetailNotFoundPayload(payload) {
		if recordMetrics {
			observability.IncCacheL2Hit(observability.CacheVideoDetail)
		}
		if s.localDetail != nil {
			// 把 L2 的负缓存回填到 L1，减少当前实例对 Redis 的重复访问。
			s.localDetail.SetNotFound(videoID)
		}
		return nil, true, true, nil
	}

	var item Video
	if err := json.Unmarshal(payload, &item); err != nil {
		// 反序列化失败说明 Redis 数据已经损坏，删掉后让后续请求通过回源重建。
		_ = s.detailCache.Delete(ctx, videoID)
		if recordMetrics {
			observability.IncCacheL2Miss(observability.CacheVideoDetail)
		}
		return nil, false, false, err
	}
	if !s.mediaValidator.IsPlayable(item) {
		// Redis 命中的详情也要过媒体校验，避免把文件已丢失的视频返回给前端。
		_ = s.detailCache.Delete(ctx, videoID)
		if recordMetrics {
			observability.IncCacheL2Miss(observability.CacheVideoDetail)
		}
		return nil, false, false, nil
	}

	if recordMetrics {
		observability.IncCacheL2Hit(observability.CacheVideoDetail)
	}
	if s.localDetail != nil {
		// L2 命中正常详情后顺手回填 L1，缩短同实例下一次访问路径。
		s.localDetail.SetVideo(videoID, payload)
	}

	// 走到这里说明缓存里拿到的是一条可直接返回的可信结果。
	return &item, true, false, nil
}

func (s *Service) setDetailCaches(videoID uint64, payload []byte) {
	if s.detailCache != nil {
		if err := s.detailCache.SetRaw(context.Background(), videoID, payload); err != nil {
			log.Printf("video service: write detail cache failed for video %d: %v", videoID, err)
		}
	}
	if s.localDetail != nil {
		s.localDetail.SetVideo(videoID, payload)
	}
}

func (s *Service) setDetailNotFound(videoID uint64) {
	if s.detailCache != nil {
		if err := s.detailCache.SetNotFound(context.Background(), videoID); err != nil {
			log.Printf("video service: write detail not-found cache failed for video %d: %v", videoID, err)
		}
	}
	if s.localDetail != nil {
		s.localDetail.SetNotFound(videoID)
	}
}

func (s *Service) invalidateDetailCache(videoID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if s.detailCache != nil {
		if err := s.detailCache.Delete(ctx, videoID); err != nil {
			log.Printf("video service: delete detail cache failed for video %d: %v", videoID, err)
		} else {
			observability.IncCacheInvalidation(observability.CacheVideoDetail, "l2", "write")
		}
	}
	if s.publisher != nil {
		if err := s.publisher.PublishCacheInvalidated(ctx, mq.CacheInvalidatedPayload{
			Cache:   mq.CacheNameVideoDetail,
			VideoID: videoID,
		}); err != nil {
			log.Printf("video service: publish detail invalidation failed for video %d: %v", videoID, err)
		}
	}
}

func normalizeIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", ErrIdempotencyKeyRequired
	}
	if len(key) > maxIdempotencyKeyLength {
		return "", ErrIdempotencyKeyTooLong
	}
	return key, nil
}

func buildPublishRequestHash(title string, description string, playURL string, coverURL string) (string, error) {
	payload := struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		PlayURL     string `json:"play_url"`
		CoverURL    string `json:"cover_url"`
	}{
		Title:       title,
		Description: description,
		PlayURL:     playURL,
		CoverURL:    coverURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
