package feed

import (
	"context"
	"log"
	"sort"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/observability"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

// Service 负责组装 latest / following / hot 等不同入口的 feed 结果。
type Service struct {
	repo             *Repo
	popularity       *popularity.Service
	latestCache      *LatestCache
	localLatestCache *LocalLatestPageCache
	hotCache         *HotPageCache
	localHotCache    *LocalHotPageCache
	timelineStore    *GlobalTimelineStore
	mediaValidator   video.MediaValidator
}

func NewService(db *gorm.DB, popularityService *popularity.Service, uploadDir string) *Service {
	return NewServiceWithCachesAndTimeline(db, popularityService, nil, nil, nil, nil, nil, uploadDir)
}

func NewServiceWithLatestCache(db *gorm.DB, popularityService *popularity.Service, latestCache *LatestCache, uploadDir string) *Service {
	return NewServiceWithCachesAndTimeline(db, popularityService, latestCache, nil, nil, nil, nil, uploadDir)
}

func NewServiceWithLatestCacheAndTimeline(
	db *gorm.DB,
	popularityService *popularity.Service,
	latestCache *LatestCache,
	timelineStore *GlobalTimelineStore,
	uploadDir string,
) *Service {
	return NewServiceWithCachesAndTimeline(db, popularityService, latestCache, nil, nil, nil, timelineStore, uploadDir)
}

func NewServiceWithCachesAndTimeline(
	db *gorm.DB,
	popularityService *popularity.Service,
	latestCache *LatestCache,
	localLatestCache *LocalLatestPageCache,
	hotCache *HotPageCache,
	localHotCache *LocalHotPageCache,
	timelineStore *GlobalTimelineStore,
	uploadDir string,
) *Service {
	return &Service{
		repo:             NewRepo(db),
		popularity:       popularityService,
		latestCache:      latestCache,
		localLatestCache: localLatestCache,
		hotCache:         hotCache,
		localHotCache:    localHotCache,
		timelineStore:    timelineStore,
		mediaValidator:   video.NewMediaValidator(uploadDir),
	}
}

func (s *Service) ListLatest(req ListLatestRequest) (*ListLatestResult, error) {
	req.Limit = normalizeLimit(req.Limit)
	ctx := context.Background()

	version := int64(1)
	if s.latestCache != nil {
		// latest 页缓存先读版本号，后续所有 key 都带 version，避免全量删分页 key。
		cacheVersion, err := s.latestCache.GetVersion(ctx)
		if err != nil {
			log.Printf("feed service: read latest cache version failed: %v", err)
		} else if cacheVersion > 0 {
			version = cacheVersion
		}
	}

	if s.localLatestCache != nil {
		cachedResult, ok, err := s.localLatestCache.Get(version, req)
		if err != nil {
			observability.IncCacheL1Miss(observability.CacheFeedLatest)
			s.localLatestCache.Clear()
		} else if ok {
			observability.IncCacheL1Hit(observability.CacheFeedLatest)
			return cachedResult, nil
		} else {
			observability.IncCacheL1Miss(observability.CacheFeedLatest)
		}
	}

	if s.latestCache != nil {
		cachedResult, ok, err := s.latestCache.Get(ctx, version, req)
		if err != nil {
			observability.IncCacheL2Miss(observability.CacheFeedLatest)
			log.Printf("feed service: read latest cache failed: %v", err)
		} else if ok {
			observability.IncCacheL2Hit(observability.CacheFeedLatest)
			if s.localLatestCache != nil {
				s.localLatestCache.Set(version, req, cachedResult)
			}
			return cachedResult, nil
		} else {
			observability.IncCacheL2Miss(observability.CacheFeedLatest)
		}
	}

	startedAt := time.Now()

	if s.timelineStore != nil && s.timelineStore.Enabled() {
		// 全局时间线命中时优先复用，减少直接扫 MySQL 的次数。
		timelineResult, ok, err := s.listLatestFromTimeline(req)
		if err != nil {
			log.Printf("feed service: read global timeline failed: %v", err)
		} else if ok {
			s.setLatestCaches(ctx, version, req, timelineResult)
			observability.ObserveCacheLoadSeconds(observability.CacheFeedLatest, time.Since(startedAt).Seconds())
			return timelineResult, nil
		}
	}

	videos, err := s.repo.ListLatest(req.Limit+1, req.LatestTime, req.LastID)
	if err != nil {
		return nil, err
	}

	rawVideos, hasMore := trimVideoPage(videos, req.Limit)
	videos = s.mediaValidator.FilterPlayable(rawVideos)

	scores, err := s.loadScores(ctx, videos, time.Time{})
	if err != nil {
		return nil, err
	}

	result := &ListLatestResult{
		Videos:  buildFeedVideos(videos, scores),
		HasMore: hasMore,
	}
	if len(rawVideos) > 0 {
		last := rawVideos[len(rawVideos)-1]
		result.NextTime = last.CreatedAt.UnixMilli()
		result.NextID = last.ID
	}

	s.setLatestCaches(ctx, version, req, result)
	observability.ObserveCacheLoadSeconds(observability.CacheFeedLatest, time.Since(startedAt).Seconds())
	return result, nil
}

func (s *Service) listLatestFromTimeline(req ListLatestRequest) (*ListLatestResult, bool, error) {
	videoIDs, err := s.timelineStore.ListVideoIDs(context.Background(), req)
	if err != nil {
		return nil, false, err
	}
	if len(videoIDs) == 0 {
		return nil, false, nil
	}

	videos, err := s.repo.FindByIDs(videoIDs)
	if err != nil {
		return nil, false, err
	}

	videoByID := make(map[uint64]video.Video, len(videos))
	for _, item := range videos {
		videoByID[item.ID] = item
	}

	candidates := make([]video.Video, 0, len(videoIDs))
	staleIDs := make([]uint64, 0)
	for _, videoID := range videoIDs {
		item, ok := videoByID[videoID]
		if !ok {
			staleIDs = append(staleIDs, videoID)
			continue
		}
		if !includeLatestCursor(item, req) {
			continue
		}
		if !s.mediaValidator.IsPlayable(item) {
			// 时间线里的不可播放视频顺手清掉，避免后续请求重复命中脏数据。
			staleIDs = append(staleIDs, item.ID)
			continue
		}
		candidates = append(candidates, item)
	}
	if len(staleIDs) > 0 {
		if err := s.timelineStore.Remove(context.Background(), staleIDs...); err != nil {
			log.Printf("feed service: cleanup stale global timeline members failed: %v", err)
		}
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID > candidates[j].ID
		}
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})

	rawVideos, hasMore := trimVideoPage(candidates, req.Limit)

	scores, err := s.loadScores(context.Background(), rawVideos, time.Time{})
	if err != nil {
		return nil, false, err
	}

	result := &ListLatestResult{
		Videos:  buildFeedVideos(rawVideos, scores),
		HasMore: hasMore,
	}
	last := rawVideos[len(rawVideos)-1]
	result.NextTime = last.CreatedAt.UnixMilli()
	result.NextID = last.ID
	return result, true, nil
}

func (s *Service) ListLikesCount(req ListLikesCountRequest) (*ListLikesCountResult, error) {
	req.Limit = normalizeLimit(req.Limit)

	videos, err := s.repo.ListLikesCount(req.Limit+1, req.LikesCountBefore, req.IDBefore)
	if err != nil {
		return nil, err
	}
	rawVideos, hasMore := trimVideoPage(videos, req.Limit)
	videos = s.mediaValidator.FilterPlayable(rawVideos)

	scores, err := s.loadScores(context.Background(), videos, time.Time{})
	if err != nil {
		return nil, err
	}

	result := &ListLikesCountResult{
		Videos:  buildFeedVideos(videos, scores),
		HasMore: hasMore,
	}
	if len(rawVideos) > 0 {
		last := rawVideos[len(rawVideos)-1]
		nextLikesCount := last.LikesCount
		result.NextLikesCountBefore = &nextLikesCount
		result.NextIDBefore = last.ID
	}

	return result, nil
}

func (s *Service) ListByFollowing(accountID uint64, req ListByFollowingRequest) (*ListByFollowingResult, error) {
	req.Limit = normalizeLimit(req.Limit)

	videos, err := s.repo.ListByFollowing(accountID, req.Limit+1, req.LatestTime, req.LastID)
	if err != nil {
		return nil, err
	}
	rawVideos, hasMore := trimVideoPage(videos, req.Limit)
	videos = s.mediaValidator.FilterPlayable(rawVideos)

	scores, err := s.loadScores(context.Background(), videos, time.Time{})
	if err != nil {
		return nil, err
	}

	result := &ListByFollowingResult{
		Videos:  buildFeedVideos(videos, scores),
		HasMore: hasMore,
	}
	if len(rawVideos) > 0 {
		last := rawVideos[len(rawVideos)-1]
		result.NextTime = last.CreatedAt.UnixMilli()
		result.NextID = last.ID
	}

	return result, nil
}

func (s *Service) ListByPopularity(req ListByPopularityRequest) (*ListByPopularityResult, error) {
	req.Limit = normalizeLimit(req.Limit)
	if req.Offset < 0 {
		req.Offset = 0
	}

	ctx := context.Background()

	if s.localHotCache != nil {
		cachedResult, ok, err := s.localHotCache.Get(req)
		if err != nil {
			observability.IncCacheL1Miss(observability.CacheFeedHot)
		} else if ok {
			observability.IncCacheL1Hit(observability.CacheFeedHot)
			return cachedResult, nil
		} else {
			observability.IncCacheL1Miss(observability.CacheFeedHot)
		}
	}

	if s.hotCache != nil {
		cachedResult, ok, err := s.hotCache.Get(ctx, req)
		if err != nil {
			observability.IncCacheL2Miss(observability.CacheFeedHot)
			log.Printf("feed service: read hot cache failed: %v", err)
		} else if ok {
			observability.IncCacheL2Hit(observability.CacheFeedHot)
			if s.localHotCache != nil {
				s.localHotCache.Set(req, cachedResult)
			}
			return cachedResult, nil
		} else {
			observability.IncCacheL2Miss(observability.CacheFeedHot)
		}
	}

	startedAt := time.Now()

	if s.popularity == nil {
		// Redis 热度服务不可用时，退化为读 MySQL 持久化热度字段。
		videos, err := s.repo.ListByPopularity(req.Limit+1, req.Offset)
		if err != nil {
			return nil, err
		}
		rawVideos, hasMore := trimVideoPage(videos, req.Limit)
		videos = s.mediaValidator.FilterPlayable(rawVideos)

		scores, err := s.loadScores(ctx, videos, time.Time{})
		if err != nil {
			return nil, err
		}

		result := &ListByPopularityResult{
			Videos:     buildFeedVideos(videos, scores),
			AsOf:       0,
			NextOffset: req.Offset + int64(len(rawVideos)),
			HasMore:    hasMore,
		}
		s.setHotCaches(ctx, req, result)
		observability.ObserveCacheLoadSeconds(observability.CacheFeedHot, time.Since(startedAt).Seconds())
		return result, nil
	}

	var asOf time.Time
	if req.AsOf > 0 {
		asOf = time.UnixMilli(req.AsOf)
	}

	videoIDs, scores, snapshotAsOf, err := s.popularity.ListHot(ctx, asOf, req.Limit+1, req.Offset)
	if err != nil {
		return nil, err
	}
	pageVideoIDs, hasMore := trimUint64Page(videoIDs, req.Limit)

	videos, err := s.repo.FindByIDs(pageVideoIDs)
	if err != nil {
		return nil, err
	}

	videoByID := make(map[uint64]video.Video, len(videos))
	for _, item := range videos {
		videoByID[item.ID] = item
	}

	result := &ListByPopularityResult{
		Videos:     make([]FeedVideo, 0, len(pageVideoIDs)),
		AsOf:       snapshotAsOf,
		NextOffset: req.Offset,
		HasMore:    hasMore,
	}

	for _, videoID := range pageVideoIDs {
		item, ok := videoByID[videoID]
		if !ok {
			continue
		}
		if !s.mediaValidator.IsPlayable(item) {
			// hot 页缓存的是最终可返回结果，拼装阶段继续过滤失效媒体。
			continue
		}

		result.Videos = append(result.Videos, FeedVideo{
			ID:           item.ID,
			AuthorID:     item.AuthorID,
			Username:     item.Username,
			Title:        item.Title,
			Description:  item.Description,
			PlayURL:      item.PlayURL,
			CoverURL:     item.CoverURL,
			LikesCount:   item.LikesCount,
			CommentCount: item.CommentCount,
			Popularity:   scores[item.ID],
			CreatedAt:    item.CreatedAt,
			UpdatedAt:    item.UpdatedAt,
		})
	}
	result.NextOffset += int64(len(pageVideoIDs))

	s.setHotCaches(ctx, req, result)
	observability.ObserveCacheLoadSeconds(observability.CacheFeedHot, time.Since(startedAt).Seconds())
	return result, nil
}

func (s *Service) setLatestCaches(ctx context.Context, version int64, req ListLatestRequest, result *ListLatestResult) {
	if s.latestCache != nil {
		if err := s.latestCache.Set(ctx, version, req, result); err != nil {
			log.Printf("feed service: write latest cache failed: %v", err)
		}
	}
	if s.localLatestCache != nil {
		s.localLatestCache.Set(version, req, result)
	}
}

func (s *Service) setHotCaches(ctx context.Context, req ListByPopularityRequest, result *ListByPopularityResult) {
	if s.hotCache != nil {
		if err := s.hotCache.Set(ctx, req, result); err != nil {
			log.Printf("feed service: write hot cache failed: %v", err)
		}
	}
	if s.localHotCache != nil {
		s.localHotCache.Set(req, result)
	}
}

func normalizeLimit(limit int64) int64 {
	if limit <= 0 {
		return 10
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func includeLatestCursor(item video.Video, req ListLatestRequest) bool {
	if req.LatestTime <= 0 {
		return true
	}

	cursorTime := time.UnixMilli(req.LatestTime)
	if item.CreatedAt.Before(cursorTime) {
		return true
	}
	if item.CreatedAt.After(cursorTime) {
		return false
	}
	if req.LastID == 0 {
		return true
	}
	return item.ID < req.LastID
}

func trimVideoPage(videos []video.Video, limit int64) ([]video.Video, bool) {
	if limit <= 0 {
		return videos, false
	}
	if int64(len(videos)) <= limit {
		return videos, false
	}
	return videos[:limit], true
}

func trimUint64Page(items []uint64, limit int64) ([]uint64, bool) {
	if limit <= 0 {
		return items, false
	}
	if int64(len(items)) <= limit {
		return items, false
	}
	return items[:limit], true
}

func buildFeedVideos(videos []video.Video, scores map[uint64]int64) []FeedVideo {
	result := make([]FeedVideo, 0, len(videos))
	for _, item := range videos {
		result = append(result, FeedVideo{
			ID:           item.ID,
			AuthorID:     item.AuthorID,
			Username:     item.Username,
			Title:        item.Title,
			Description:  item.Description,
			PlayURL:      item.PlayURL,
			CoverURL:     item.CoverURL,
			LikesCount:   item.LikesCount,
			CommentCount: item.CommentCount,
			Popularity:   scores[item.ID],
			CreatedAt:    item.CreatedAt,
			UpdatedAt:    item.UpdatedAt,
		})
	}

	return result
}

func (s *Service) loadScores(ctx context.Context, videos []video.Video, asOf time.Time) (map[uint64]int64, error) {
	scores := make(map[uint64]int64, len(videos))
	if len(videos) == 0 {
		return scores, nil
	}

	if s.popularity == nil {
		for _, item := range videos {
			scores[item.ID] = item.Popularity
		}
		return scores, nil
	}

	videoIDs := make([]uint64, 0, len(videos))
	for _, item := range videos {
		videoIDs = append(videoIDs, item.ID)
	}

	return s.popularity.Scores(ctx, videoIDs, asOf)
}
