package video

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"my_feed_system/internal/audit"
	"my_feed_system/internal/idempotency"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/storage"
	"my_feed_system/internal/tag"
)

var (
	ErrVideoNotFound              = errors.New("video not found")
	ErrIdempotencyKeyRequired     = errors.New("idempotency key is required")
	ErrIdempotencyKeyTooLong      = errors.New("idempotency key is too long")
	ErrIdempotencyRequestConflict = errors.New("idempotency key already used with different request")
	ErrIdempotencyRequestBusy     = errors.New("request with the same idempotency key is still processing")
	ErrNotDraft                   = errors.New("video is not a draft")
	ErrDraftLimitReached          = errors.New("draft limit reached")
	ErrCannotUnpublish            = errors.New("video cannot be unpublished")
	ErrCannotRelist               = errors.New("video cannot be relisted")
)

const (
	videoPublishBizType     = "video.publish"
	maxIdempotencyKeyLength = 128
	// takedownModerator 标记流水来源是举报处置，便于与机审、常规人工复审区分。
	takedownModerator = "report"
	// adminTakedownModerator 标记流水来自管理后台的直接下架（不一定先有举报）。
	adminTakedownModerator = "admin"
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
	store           storage.ObjectStore
	approval        *ApprovalPublisher
	auditEnabled    bool
	detailGroup     singleflight.Group
}

func NewService(db *gorm.DB, popularityService *popularity.Service, store storage.ObjectStore) *Service {
	return NewServiceWithCachesAndPublisher(db, popularityService, nil, nil, nil, store, false)
}

func NewServiceWithDetailCache(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, store storage.ObjectStore) *Service {
	return NewServiceWithCachesAndPublisher(db, popularityService, detailCache, nil, nil, store, false)
}

func NewServiceWithDetailCacheAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, publisher *mq.Publisher, store storage.ObjectStore) *Service {
	return NewServiceWithCachesAndPublisher(db, popularityService, detailCache, nil, publisher, store, false)
}

func NewServiceWithCachesAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, localDetail *LocalDetailCache, publisher *mq.Publisher, store storage.ObjectStore, auditEnabled bool) *Service {
	return &Service{
		db:              db,
		repo:            NewRepo(db),
		idempotencyRepo: idempotency.NewRepo(db),
		outboxRepo:      outbox.NewRepo(db),
		popularity:      popularityService,
		detailCache:     detailCache,
		localDetail:     localDetail,
		publisher:       publisher,
		mediaValidator:  NewMediaValidator(),
		store:           store,
		approval:        NewApprovalPublisher(db),
		auditEnabled:    auditEnabled,
	}
}

// publishStatTimeout 限制发布校验里那一两次对象存储往返。
// 存储抖动时宁可让发布失败，也不要把请求挂住。
const publishStatTimeout = 5 * time.Second

// normalizePublishURLs 归一化并确认媒体确实存在。
//
// 读路径已经不再探测存储，这里是唯一还能挡住「伪造 play_url」的地方：
// 形状合法但对象不存在的 URL 一旦入库，就会在信息流里长期留下一条打不开的作品。
func (s *Service) normalizePublishURLs(playURL string, coverURL string) (string, string, error) {
	normalizedPlay, normalizedCover, err := s.mediaValidator.NormalizePublishURLs(playURL, coverURL)
	if err != nil {
		return "", "", err
	}
	if s.store == nil {
		return normalizedPlay, normalizedCover, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), publishStatTimeout)
	defer cancel()

	if _, err := s.store.StatMedia(ctx, ObjectKey(normalizedPlay)); err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return "", "", ErrInvalidPlayURL
		}
		return "", "", fmt.Errorf("stat play object: %w", err)
	}
	if normalizedCover != "" {
		if _, err := s.store.StatMedia(ctx, ObjectKey(normalizedCover)); err != nil {
			if errors.Is(err, storage.ErrObjectNotFound) {
				return "", "", ErrInvalidCoverURL
			}
			return "", "", fmt.Errorf("stat cover object: %w", err)
		}
	}
	return normalizedPlay, normalizedCover, nil
}

// Publish creates a video exactly once for the same idempotency key and payload.
func (s *Service) Publish(accountID uint64, username string, idemKey string, req PublishRequest) (*Video, error) {
	normalizedIdemKey, err := normalizeIdempotencyKey(idemKey)
	if err != nil {
		return nil, err
	}

	playURL, coverURL, err := s.normalizePublishURLs(req.PlayURL, req.CoverURL)
	if err != nil {
		return nil, err
	}

	tags := tag.ForPublish(req.Tags, req.Title, req.Description)
	requestHash, err := buildPublishRequestHash(req.Title, req.Description, []string(tags), playURL, coverURL)
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
		draft, err := s.findPromotableDraft(tx, accountID, req.DraftID, playURL)
		if err != nil {
			return err
		}
		if draft != nil {
			video, err = s.promoteDraft(tx, draft, username, req.Title, req.Description, tags, playURL, coverURL)
			if err != nil {
				return err
			}
		} else {
			video = &Video{
				AuthorID:    accountID,
				Username:    username,
				Title:       req.Title,
				Description: req.Description,
				Tags:        tags,
				PlayURL:     playURL,
				CoverURL:    coverURL,
				Popularity:  int64(popularity.PublishWeight),
				// 审核关闭时发布即公开；打开时先待审，仅作者可见。
				AuditStatus: s.initialAuditStatus(),
				Lifecycle:   LifecyclePublished,
			}

			if err := s.repo.Create(tx, video); err != nil {
				return err
			}
		}

		if err := s.enqueueAfterCreate(tx, video); err != nil {
			return err
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
		// 只在本地对象上带出初始热度供响应展示。
		// 审核开启时排行榜由过审流程写入；关闭时由下方的公开化事件写入。
		video.Popularity = int64(popularity.PublishWeight)
	}
	if createdNew {
		s.invalidateDetailCache(video.ID)
	}

	return video, nil
}

func (s *Service) initialAuditStatus() audit.Status {
	if s.auditEnabled {
		return audit.StatusPending
	}
	return audit.StatusApproved
}

// enqueueAfterCreate 在同一事务里投递发布后的后续事件。
// 审核开启时只发审核事件，公开化推迟到过审；关闭时直接公开。
func (s *Service) enqueueAfterCreate(tx *gorm.DB, video *Video) error {
	if !s.auditEnabled {
		if err := s.approval.EnqueueOnPublish(tx, video.ID, video.AuthorID); err != nil {
			return err
		}
		return nil
	}

	event, err := mq.NewEnvelope(mq.EventTypeAuditRequested, mq.ProducerAPIServer, mq.AuditRequestedPayload{
		TargetType: string(audit.TargetVideo),
		TargetID:   video.ID,
		AuthorID:   video.AuthorID,
	})
	if err != nil {
		return fmt.Errorf("build audit outbox event: %w", err)
	}
	if err := s.outboxRepo.Enqueue(tx, event); err != nil {
		return fmt.Errorf("enqueue audit outbox event: %w", err)
	}
	return nil
}

// ListByAuthorID 返回作者的作品列表。
// viewerID 是当前查看者：本人可见自己全部状态的作品，他人只见已过审的。
func (s *Service) ListByAuthorID(viewerID uint64, req ListByAuthorIDRequest) ([]Video, error) {
	videos, err := s.repo.FindByAuthorID(req.AuthorID, viewerID)
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
//
// viewerID 用于审核可见性判断：未过审的视频只有作者本人能打开，
// 其他人一律按「不存在」处理。
func (s *Service) GetDetail(viewerID uint64, req GetDetailRequest) (*Video, error) {
	ctx := context.Background()

	// 先在 singleflight 之外查一次缓存，让已经命中的请求直接返回，避免不必要地进入合并逻辑。
	if cachedVideo, ok, notFound, err := s.getDetailFromCaches(ctx, req.ID, true); err != nil {
		slog.Warn("read detail cache failed", slog.Uint64("video_id", req.ID), slog.String("error", err.Error()))
	} else if ok {
		if notFound {
			return nil, ErrVideoNotFound
		}
		if !visibleTo(cachedVideo, viewerID) {
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
	if !visibleTo(loadResult.video, viewerID) {
		return nil, ErrVideoNotFound
	}
	return loadResult.video, nil
}

// Share 返回视频的分享口令。
//
// 刻意复用 GetDetail 而不是直接查库：分享入口必须和详情页遵守同一套
// 可见性规则，否则未过审内容会通过「生成口令」这条旁路泄漏出去。
func (s *Service) Share(viewerID uint64, req ShareRequest) (*ShareInfo, error) {
	video, err := s.GetDetail(viewerID, GetDetailRequest{ID: req.ID})
	if err != nil {
		return nil, err
	}

	code, err := EncodeShareCode(video.ID)
	if err != nil {
		return nil, err
	}

	return &ShareInfo{
		VideoID:  video.ID,
		Code:     code,
		Title:    video.Title,
		Username: video.Username,
		CoverURL: video.CoverURL,
	}, nil
}

// ResolveShare 从粘贴文本中还原出视频。
//
// 同样走 GetDetail：口令只是寻址方式，不是访问凭据，
// 拿着有效口令也不能看到未过审或已下架的内容。
func (s *Service) ResolveShare(viewerID uint64, req ResolveShareRequest) (*Video, error) {
	videoID, err := ExtractShareCode(req.Text)
	if err != nil {
		return nil, err
	}
	return s.GetDetail(viewerID, GetDetailRequest{ID: videoID})
}

// LoadForReport 让 video 满足 report.ContentStore 的内容查询部分。
//
// 复用 GetDetail 而不是直接查库：举报入口只能作用于举报人本来就看得见的内容，
// 否则它会退化成一个「探测任意 ID 是否存在」的旁路。
func (s *Service) LoadForReport(viewerID uint64, targetID uint64) (uint64, bool, error) {
	video, err := s.GetDetail(viewerID, GetDetailRequest{ID: targetID})
	if errors.Is(err, ErrVideoNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return video.AuthorID, true, nil
}

// Takedown 依据人工结论下架视频，让 video 满足 report.ContentStore 的处置部分。
func (s *Service) Takedown(ctx context.Context, videoID uint64, operatorID uint64, note string) error {
	return s.takedown(ctx, videoID, operatorID, note, takedownModerator)
}

// AdminTakedown 是管理后台的直接下架。流水标记为 admin，与举报处置区分。
func (s *Service) AdminTakedown(ctx context.Context, videoID uint64, operatorID uint64, note string) error {
	return s.takedown(ctx, videoID, operatorID, note, adminTakedownModerator)
}

// takedown 把内容转为拒绝态并写流水。
//
// 状态变更与流水必须同事务：只改状态而流水丢失，事后就无法回答
// 「这条内容是谁、依据什么下架的」，而处置记录的留存期是合规要求，
// 远长于日志的轮转周期。
func (s *Service) takedown(ctx context.Context, videoID uint64, operatorID uint64, note string, moderator string) error {
	video, err := s.repo.FindByID(videoID)
	if err != nil {
		return err
	}
	if video == nil {
		return ErrVideoNotFound
	}
	// 已经是拒绝态时只需补一条流水说明本次处置依据，不重复写状态。
	fromStatus := video.AuditStatus

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := NewAuditStore(s.db).UpdateStatus(tx, videoID, audit.StatusRejected); err != nil {
			return err
		}
		return audit.NewRepo(s.db).Append(tx, &audit.Record{
			TargetType: audit.TargetVideo,
			TargetID:   videoID,
			FromStatus: fromStatus,
			ToStatus:   audit.StatusRejected,
			Source:     audit.SourceManual,
			Moderator:  moderator,
			OperatorID: operatorID,
			Label:      moderator,
			Detail:     note,
		})
	}); err != nil {
		return err
	}

	// 必须在事务提交后失效缓存：LocalDetailCache 是进程内的，只改数据库的话
	// 已下架的视频还会继续从 L1 返回，看起来像「下架没生效」。
	s.invalidateDetailCache(videoID)

	slog.InfoContext(ctx, "video taken down",
		slog.Uint64("video_id", videoID),
		slog.Uint64("operator_id", operatorID),
		slog.String("from_status", string(fromStatus)),
		slog.String("moderator", moderator))

	return nil
}

// LookupForReview 给管理面读任意一条视频，绕过公开可见性。
//
// 故意不走 GetDetail：那条路径会把未过审内容伪装成「不存在」，
// 审核员必须能看见被拒和待审的作品才能处置。也不回填公开缓存，
// 避免把未过审详情写进面向公众的 L1/L2。
func (s *Service) LookupForReview(videoID uint64) (*Video, error) {
	video, err := s.repo.FindByID(videoID)
	if err != nil {
		return nil, err
	}
	if video == nil {
		return nil, ErrVideoNotFound
	}
	return video, nil
}

// ListByAuthorForReview 给管理面列出某作者的作品，含未过审和下架。
func (s *Service) ListByAuthorForReview(authorID uint64) ([]Video, error) {
	return s.repo.FindByAuthorIDAll(authorID, 50)
}

// visibleTo 判断查看者能否看到该视频。
//
// 未过审时返回「不存在」而不是「无权限」：后者会泄漏视频确实存在这一事实，
// 可被用来枚举他人尚未公开的内容。
func visibleTo(video *Video, viewerID uint64) bool {
	if video == nil || video.IsDeleted() {
		return false
	}
	if video.IsPubliclyListed() {
		return true
	}
	return viewerID != 0 && viewerID == video.AuthorID
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
			slog.Warn("write detail cache failed", slog.Uint64("video_id", videoID), slog.String("error", err.Error()))
		}
	}
	if s.localDetail != nil {
		s.localDetail.SetVideo(videoID, payload)
	}
}

func (s *Service) setDetailNotFound(videoID uint64) {
	if s.detailCache != nil {
		if err := s.detailCache.SetNotFound(context.Background(), videoID); err != nil {
			slog.Warn("write detail not-found cache failed", slog.Uint64("video_id", videoID), slog.String("error", err.Error()))
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
			slog.Warn("delete detail cache failed", slog.Uint64("video_id", videoID), slog.String("error", err.Error()))
		} else {
			observability.IncCacheInvalidation(observability.CacheVideoDetail, "l2", "write")
		}
	}
	if s.publisher != nil {
		if err := s.publisher.PublishCacheInvalidated(ctx, mq.CacheInvalidatedPayload{
			Cache:   mq.CacheNameVideoDetail,
			VideoID: videoID,
		}); err != nil {
			slog.Warn("publish detail invalidation failed", slog.Uint64("video_id", videoID), slog.String("error", err.Error()))
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

func buildPublishRequestHash(title string, description string, tags []string, playURL string, coverURL string) (string, error) {
	payload := struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		PlayURL     string   `json:"play_url"`
		CoverURL    string   `json:"cover_url"`
	}{
		Title:       title,
		Description: description,
		Tags:        tags,
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
