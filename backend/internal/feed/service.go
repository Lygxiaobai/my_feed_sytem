package feed

import (
	"context"
	"log"
	"sort"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

// Service 负责把底层视频数据组装成前端所需的信息流响应。
type Service struct {
	repo           *Repo
	popularity     *popularity.Service
	latestCache    *LatestCache
	timelineStore  *GlobalTimelineStore
	mediaValidator video.MediaValidator
}

// NewService 创建信息流服务。
func NewService(db *gorm.DB, popularityService *popularity.Service, uploadDir string) *Service {
	return NewServiceWithLatestCacheAndTimeline(db, popularityService, nil, nil, uploadDir)
}

// NewServiceWithLatestCache 创建带最新流缓存能力的信息流服务。
func NewServiceWithLatestCache(db *gorm.DB, popularityService *popularity.Service, latestCache *LatestCache, uploadDir string) *Service {
	return NewServiceWithLatestCacheAndTimeline(db, popularityService, latestCache, nil, uploadDir)
}

// NewServiceWithLatestCacheAndTimeline 创建带最新流缓存和时间线读取能力的信息流服务。
func NewServiceWithLatestCacheAndTimeline(db *gorm.DB, popularityService *popularity.Service, latestCache *LatestCache, timelineStore *GlobalTimelineStore, uploadDir string) *Service {
	return &Service{
		repo:           NewRepo(db),
		popularity:     popularityService,
		latestCache:    latestCache,
		timelineStore:  timelineStore,
		mediaValidator: video.NewMediaValidator(uploadDir),
	}
}

// ListLatest 返回最新发布的视频流和下一页游标。
func (s *Service) ListLatest(req ListLatestRequest) (*ListLatestResult, error) {
	req.Limit = normalizeLimit(req.Limit)

	if s.latestCache != nil {
		cachedResult, ok, err := s.latestCache.Get(context.Background(), req)
		if err != nil {
			log.Printf("feed service: read latest cache failed: %v", err)
		} else if ok {
			return cachedResult, nil
		}
	}

	if s.timelineStore != nil && s.timelineStore.Enabled() {
		timelineResult, ok, err := s.listLatestFromTimeline(req)
		if err != nil {
			log.Printf("feed service: read global timeline failed: %v", err)
		} else if ok {
			if s.latestCache != nil {
				if err := s.latestCache.Set(context.Background(), req, timelineResult); err != nil {
					log.Printf("feed service: write latest cache failed: %v", err)
				}
			}
			return timelineResult, nil
		}
	}

	// 数据库多查一条，用来精确判断当前游标后面是否还有下一页。
	videos, err := s.repo.ListLatest(req.Limit+1, req.LatestTime, req.LastID)
	if err != nil {
		return nil, err
	}
	// 原始分页结果负责推进游标；真正返回前端前再过滤不可播放媒体。
	rawVideos, hasMore := trimVideoPage(videos, req.Limit)
	videos = s.mediaValidator.FilterPlayable(rawVideos)

	scores, err := s.loadScores(context.Background(), videos, time.Time{})
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
	if s.latestCache != nil {
		if err := s.latestCache.Set(context.Background(), req, result); err != nil {
			log.Printf("feed service: write latest cache failed: %v", err)
		}
	}

	return result, nil
}

func (s *Service) listLatestFromTimeline(req ListLatestRequest) (*ListLatestResult, bool, error) {
	// 第一步先从 Redis 全局时间线里拿一批视频 ID。
	// 这里拿到的只是“候选集”，真正返回给前端前还要再经过查库、过滤和排序。
	videoIDs, err := s.timelineStore.ListVideoIDs(context.Background(), req)
	if err != nil {
		return nil, false, err
	}
	if len(videoIDs) == 0 {
		// 时间线里没有数据时，交给上层继续走数据库兜底逻辑。
		return nil, false, nil
	}

	// 按 ID 批量回表查视频详情。
	// 时间线只存 video_id，所以标题、封面、作者等信息仍然要从 MySQL 读取。
	videos, err := s.repo.FindByIDs(videoIDs)
	if err != nil {
		return nil, false, err
	}

	// 先转成 map，方便后面按时间线顺序重新组装结果。
	// 因为数据库按 ID 查询返回的顺序，不一定和 Redis 里的时间线顺序一致。
	videoByID := make(map[uint64]video.Video, len(videos))
	for _, item := range videos {
		videoByID[item.ID] = item
	}

	// candidates 是这次真正可返回的视频列表。
	// staleIDs 记录时间线里已经失效的成员，后面顺手从 Redis 里清掉。
	candidates := make([]video.Video, 0, len(videoIDs))
	staleIDs := make([]uint64, 0)
	for _, videoID := range videoIDs {
		item, ok := videoByID[videoID]
		if !ok {
			// Redis 里有，但数据库里没查到，说明这是脏时间线成员。
			staleIDs = append(staleIDs, videoID)
			continue
		}
		if !includeLatestCursor(item, req) {
			// 再做一次游标过滤，保证分页边界和数据库兜底逻辑保持一致。
			//id的过滤主要
			continue
		}
		if !s.mediaValidator.IsPlayable(item) {
			// 媒体文件不可用的视频不应该出现在 Feed 里，同时把它从时间线中清掉，避免下次反复命中。
			staleIDs = append(staleIDs, item.ID)
			continue
		}
		candidates = append(candidates, item)
	}
	if len(staleIDs) > 0 {
		// 异步链路可能留下失效 member，这里读到后顺手做一次清理。
		if err := s.timelineStore.Remove(context.Background(), staleIDs...); err != nil {
			log.Printf("feed service: cleanup stale global timeline members failed: %v", err)
		}
	}
	if len(candidates) == 0 {
		// 有候选 ID，但过滤后没有可返回的数据，也让上层继续走兜底查询。
		return nil, false, nil
	}

	// 按发布时间倒序排；时间相同则按 ID 倒序，保证分页顺序稳定。
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID > candidates[j].ID
		}
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})

	// Redis 读取时会故意多拿一些候选，这里再裁成真实页大小，并顺手得出 has_more。
	rawVideos, hasMore := trimVideoPage(candidates, req.Limit)

	// 组装 Feed 展示时需要的热度分数等派生信息。
	scores, err := s.loadScores(context.Background(), rawVideos, time.Time{})
	if err != nil {
		return nil, false, err
	}

	result := &ListLatestResult{
		Videos:  buildFeedVideos(rawVideos, scores),
		HasMore: hasMore,
	}
	last := rawVideos[len(rawVideos)-1]
	// 下一页游标取当前页最后一条记录的时间和 ID，供前端继续向后翻页。
	result.NextTime = last.CreatedAt.UnixMilli()
	result.NextID = last.ID
	return result, true, nil
}

// ListLikesCount 返回按点赞数排序的视频流和下一页游标。
func (s *Service) ListLikesCount(req ListLikesCountRequest) (*ListLikesCountResult, error) {
	req.Limit = normalizeLimit(req.Limit)

	// 排行榜同样多查一条，让后端直接返回 has_more。
	videos, err := s.repo.ListLikesCount(req.Limit+1, req.LikesCountBefore, req.IDBefore)
	if err != nil {
		return nil, err
	}
	// Filtering happens after the DB page is loaded so stale rows do not reappear on the next request.
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

// ListByFollowing 返回关注作者的视频流和下一页游标。
func (s *Service) ListByFollowing(accountID uint64, req ListByFollowingRequest) (*ListByFollowingResult, error) {
	req.Limit = normalizeLimit(req.Limit)

	// 关注流也统一使用 limit+1，避免前端再根据返回条数猜测。
	videos, err := s.repo.ListByFollowing(accountID, req.Limit+1, req.LatestTime, req.LastID)
	if err != nil {
		return nil, err
	}
	// Follow feed shares the same playable-media gate as recommend/hot to keep all entrances consistent.
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

// ListByPopularity 优先读取 Redis 热度快照；若未启用 Redis，则退化为读表字段。
func (s *Service) ListByPopularity(req ListByPopularityRequest) (*ListByPopularityResult, error) {
	req.Limit = normalizeLimit(req.Limit)
	if req.Offset < 0 {
		req.Offset = 0
	}

	// MySQL-only 降级模式下，直接使用 videos 表中的 popularity 字段排序。
	if s.popularity == nil {
		// 偏移分页也多查一条，这样返回的 has_more 不再是前端的近似判断。
		videos, err := s.repo.ListByPopularity(req.Limit+1, req.Offset)
		if err != nil {
			return nil, err
		}
		rawVideos, hasMore := trimVideoPage(videos, req.Limit)
		videos = s.mediaValidator.FilterPlayable(rawVideos)

		scores, err := s.loadScores(context.Background(), videos, time.Time{})
		if err != nil {
			return nil, err
		}

		return &ListByPopularityResult{
			Videos:     buildFeedVideos(videos, scores),
			AsOf:       0,
			NextOffset: req.Offset + int64(len(rawVideos)),
			HasMore:    hasMore,
		}, nil
	}

	var asOf time.Time
	if req.AsOf > 0 {
		asOf = time.UnixMilli(req.AsOf)
	}

	videoIDs, scores, snapshotAsOf, err := s.popularity.ListHot(context.Background(), asOf, req.Limit+1, req.Offset)
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

	return result, nil
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
	//第一页
	if req.LatestTime <= 0 {
		return true
	}

	cursorTime := time.UnixMilli(req.LatestTime)
	//之前的是视频
	if item.CreatedAt.Before(cursorTime) {
		return true
	}
	//之后的视频
	if item.CreatedAt.After(cursorTime) {
		return false
	}
	//一个时刻的视频
	if req.LastID == 0 {
		return true
	}
	return item.ID < req.LastID
}

// trimVideoPage 保留当前页真正需要返回的记录，并标记是否还有下一页。
func trimVideoPage(videos []video.Video, limit int64) ([]video.Video, bool) {
	if limit <= 0 {
		return videos, false
	}
	if int64(len(videos)) <= limit {
		return videos, false
	}
	return videos[:limit], true
}

// trimUint64Page 用于只携带 ID 的分页场景，比如热榜快照读取。
func trimUint64Page(items []uint64, limit int64) ([]uint64, bool) {
	if limit <= 0 {
		return items, false
	}
	if int64(len(items)) <= limit {
		return items, false
	}
	return items[:limit], true
}

// buildFeedVideos 把 video 实体和热度分值合并成前端消费的 FeedVideo。
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

// loadScores 按视频 ID 批量加载热度分值。
func (s *Service) loadScores(ctx context.Context, videos []video.Video, asOf time.Time) (map[uint64]int64, error) {
	scores := make(map[uint64]int64, len(videos))
	if len(videos) == 0 {
		return scores, nil
	}

	// MySQL-only 模式下直接返回表中持久化的热度字段。
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
