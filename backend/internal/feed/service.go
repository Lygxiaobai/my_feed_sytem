package feed

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/observability"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

const (
	followingReadFactor = int64(4)
	followingReadCap    = int64(200)
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
	fanout           *FollowingFanout
	mediaValidator   video.MediaValidator
}

// FollowingFanout 汇总关注流「推拉结合」所需的存储与阈值。
type FollowingFanout struct {
	Inbox     *InboxStore
	Outbox    *OutboxStore
	Following *FollowingCache
	// PullThreshold 及以上粉丝数的作者走读扩散，读路径需要合并他们的发件箱。
	PullThreshold int64
	// MaxPullAuthors 限制单次读取最多合并几个大V 发件箱。
	MaxPullAuthors int
}

// Enabled 判断推拉结合能力是否可用。任一依赖缺失都退回纯读扩散。
func (f *FollowingFanout) Enabled() bool {
	return f != nil && f.Inbox.Enabled() && f.Outbox.Enabled() && f.Following.Enabled()
}

// WithFollowingFanout 挂载关注流推拉结合能力。
// 未挂载（或 Redis 不可用）时关注流退化为直接查 MySQL，行为与优化前一致。
func (s *Service) WithFollowingFanout(fanout *FollowingFanout) *Service {
	s.fanout = fanout
	return s
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
			slog.Warn("read latest cache version failed", slog.String("error", err.Error()))
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
			slog.Warn("read latest cache failed", slog.String("error", err.Error()))
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
			slog.Warn("read global timeline failed", slog.String("error", err.Error()))
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
			slog.Warn("cleanup stale global timeline members failed", slog.String("error", err.Error()))
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

// ListByFollowing 返回关注流。
//
// 优先走推拉结合：普通作者的视频在发布时已经写扩散进当前用户的收件箱，
// 大V 的视频则在这里从其发件箱实时拉取并归并。任何一步不可信都退回 MySQL，
// 因为关注流少一条内容是用户可感知的错误，而多查一次库只是慢一点。
func (s *Service) ListByFollowing(accountID uint64, req ListByFollowingRequest) (*ListByFollowingResult, error) {
	req.Limit = normalizeLimit(req.Limit)

	if accountID != 0 && s.fanout.Enabled() {
		result, ok, err := s.listByFollowingFromInbox(context.Background(), accountID, req)
		if err != nil {
			slog.Warn("read following inbox failed, falling back to MySQL", slog.String("error", err.Error()))
		} else if ok {
			return result, nil
		}
	}

	observability.IncFeedFollowingSource(observability.FollowingSourceFallback)
	return s.listByFollowingFromMySQL(accountID, req)
}

// listByFollowingFromInbox 用收件箱与大V 发件箱组装关注流。
// 返回 ok=false 表示这一页无法从缓存结构上给出可信答案，应由 MySQL 出数。
func (s *Service) listByFollowingFromInbox(
	ctx context.Context,
	accountID uint64,
	req ListByFollowingRequest,
) (*ListByFollowingResult, bool, error) {
	authors, err := s.loadFollowedAuthors(ctx, accountID)
	if err != nil {
		return nil, false, err
	}
	if len(authors) == 0 {
		// 没有关注任何人是确定结论，不必再回源确认一次。
		observability.IncFeedFollowingSource(observability.FollowingSourceInbox)
		return &ListByFollowingResult{Videos: []FeedVideo{}}, true, nil
	}

	followed := make(map[uint64]struct{}, len(authors))
	pullAuthorIDs := make([]uint64, 0)
	for _, author := range authors {
		followed[author.VloggerID] = struct{}{}
		if author.FollowerCount >= s.fanout.PullThreshold {
			pullAuthorIDs = append(pullAuthorIDs, author.VloggerID)
		}
	}
	if len(pullAuthorIDs) > s.fanout.MaxPullAuthors {
		// 关注的大V 越多，读扩散要打的发件箱就越多。超过上限后这条路径已经
		// 比一次带索引的联表查询更贵，继续走下去只会把 Redis 拖垮。
		return nil, false, nil
	}

	source := observability.FollowingSourceInbox
	active, err := s.fanout.Inbox.IsActive(ctx, accountID)
	if err != nil {
		return nil, false, err
	}
	if !active {
		// 冷启动、收件箱被淘汰、用户长期不活跃都会走到这里，统一按回源重建处理。
		if err := s.rebuildInbox(ctx, accountID); err != nil {
			return nil, false, err
		}
		source = observability.FollowingSourceRebuild
	}

	readCount := followingReadCount(req.Limit)
	inboxIDs, inboxCard, err := s.fanout.Inbox.ListVideoIDs(ctx, accountID, req.LatestTime, readCount)
	if err != nil {
		return nil, false, err
	}

	// 收件箱读满说明更早的候选还没取回来；发件箱读满同理。
	// 这个标记决定了「结果不足一页」到底是真的没有了，还是只是没读到。
	truncated := int64(len(inboxIDs)) >= readCount

	candidateIDs := make([]uint64, 0, len(inboxIDs))
	seen := make(map[uint64]struct{}, len(inboxIDs))
	appendCandidates := func(ids []uint64) {
		for _, videoID := range ids {
			if _, ok := seen[videoID]; ok {
				continue
			}
			seen[videoID] = struct{}{}
			candidateIDs = append(candidateIDs, videoID)
		}
	}
	appendCandidates(inboxIDs)

	for _, authorID := range pullAuthorIDs {
		outboxIDs, full, err := s.fanout.Outbox.ListVideoIDs(ctx, authorID, req.LatestTime, readCount)
		if err != nil {
			return nil, false, err
		}
		truncated = truncated || full
		appendCandidates(outboxIDs)
	}

	// FindByIDs 自带审核过滤，被下架的内容不会从这里泄漏出去。
	videos, err := s.repo.FindByIDs(candidateIDs)
	if err != nil {
		return nil, false, err
	}
	videoByID := make(map[uint64]video.Video, len(videos))
	for _, item := range videos {
		videoByID[item.ID] = item
	}

	candidates := make([]video.Video, 0, len(candidateIDs))
	staleIDs := make([]uint64, 0)
	for _, videoID := range candidateIDs {
		item, ok := videoByID[videoID]
		if !ok {
			// 已删除或未过审，收件箱里的残留 ID 顺手清掉。
			staleIDs = append(staleIDs, videoID)
			continue
		}
		if _, following := followed[item.AuthorID]; !following {
			// 取关后收件箱里仍会残留该作者的视频。这里按关注列表过滤是权威判定，
			// 取关时不做精确 ZREM——ZSET 的 member 只有 videoID，反查作者代价更大。
			staleIDs = append(staleIDs, videoID)
			continue
		}
		if !includeTimeCursor(item, req.LatestTime, req.LastID) {
			continue
		}
		if !s.mediaValidator.IsPlayable(item) {
			staleIDs = append(staleIDs, videoID)
			continue
		}
		candidates = append(candidates, item)
	}
	if len(staleIDs) > 0 {
		if err := s.fanout.Inbox.Remove(ctx, accountID, staleIDs...); err != nil {
			slog.Warn("cleanup stale following inbox members failed", slog.String("error", err.Error()))
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID > candidates[j].ID
		}
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})

	rawVideos, hasMore := trimVideoPage(candidates, req.Limit)
	if !hasMore && (truncated || inboxCard >= s.fanout.Inbox.MaxSize()) {
		// 凑不满一页，且收件箱或发件箱可能还有更早的内容没读到，
		// 此时断言「没有下一页」会让用户永远翻不到剩下的历史内容。
		return nil, false, nil
	}

	result := &ListByFollowingResult{
		Videos:  buildFeedVideos(rawVideos, s.followingScores(ctx, rawVideos)),
		HasMore: hasMore,
	}
	if len(rawVideos) > 0 {
		last := rawVideos[len(rawVideos)-1]
		result.NextTime = last.CreatedAt.UnixMilli()
		result.NextID = last.ID
	}

	observability.IncFeedFollowingSource(source)
	return result, true, nil
}

// followingScores 读取热度分。热度只影响卡片上的展示数值，
// 取不到时退回视频表里的持久化值，不值得让整条关注流失败。
func (s *Service) followingScores(ctx context.Context, videos []video.Video) map[uint64]int64 {
	scores, err := s.loadScores(ctx, videos, time.Time{})
	if err != nil {
		slog.Warn("load following feed scores failed", slog.String("error", err.Error()))
		scores = make(map[uint64]int64, len(videos))
		for _, item := range videos {
			scores[item.ID] = item.Popularity
		}
	}
	return scores
}

// loadFollowedAuthors 读取关注列表及作者粉丝数，优先走缓存。
func (s *Service) loadFollowedAuthors(ctx context.Context, accountID uint64) ([]FollowedAuthor, error) {
	cached, ok, err := s.fanout.Following.Get(ctx, accountID)
	if err != nil {
		observability.IncCacheL2Miss(observability.CacheFeedFollowing)
		slog.Warn("read following cache failed", slog.String("error", err.Error()))
	} else if ok {
		observability.IncCacheL2Hit(observability.CacheFeedFollowing)
		return cached, nil
	} else {
		observability.IncCacheL2Miss(observability.CacheFeedFollowing)
	}

	authors, err := s.repo.ListFollowedAuthors(accountID)
	if err != nil {
		return nil, err
	}
	if err := s.fanout.Following.Set(ctx, accountID, authors); err != nil {
		slog.Warn("write following cache failed", slog.String("error", err.Error()))
	}

	return authors, nil
}

// rebuildInbox 按 MySQL 里的真值重建收件箱。
//
// 标记活跃必须发生在查库之前。反过来的话存在这样一条时序：扩散 Worker 检查
// 活跃标记发现是「不活跃」而跳过推送，但那条新视频发布于我们查库之后，
// 于是它既没被推进来、也没被查出来，对这个用户永久不可见——
// 而 feed spec 明确要求异步时间线更新不得让新发布的视频永久不可见。
func (s *Service) rebuildInbox(ctx context.Context, accountID uint64) error {
	if err := s.fanout.Inbox.MarkActive(ctx, accountID); err != nil {
		return err
	}

	videos, err := s.repo.ListByFollowing(accountID, s.fanout.Inbox.MaxSize(), 0, 0)
	if err != nil {
		return err
	}

	entries := make([]TimelineEntry, 0, len(videos))
	for _, item := range videos {
		entries = append(entries, TimelineEntry{VideoID: item.ID, CreatedAt: item.CreatedAt})
	}

	return s.fanout.Inbox.Fill(ctx, accountID, entries)
}

func (s *Service) listByFollowingFromMySQL(accountID uint64, req ListByFollowingRequest) (*ListByFollowingResult, error) {
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
			slog.Warn("read hot cache failed", slog.String("error", err.Error()))
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

	if s.shouldUsePersistedPopularity(ctx, req) {
		return s.listByPersistedPopularity(ctx, req)
	}

	var asOf time.Time
	if req.AsOf > 0 {
		asOf = time.UnixMilli(req.AsOf)
	}

	videoIDs, scores, snapshotAsOf, err := s.popularity.ListHot(ctx, asOf, req.Limit+1, req.Offset)
	if err != nil {
		slog.Warn("read popularity ranking failed, falling back to MySQL", slog.String("error", err.Error()))
		return s.listByPersistedPopularity(ctx, req)
	}
	if len(videoIDs) == 0 {
		return s.listByPersistedPopularity(ctx, req)
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
			Tags:         []string(item.Tags),
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

	// 快照成员够数但可播放的不够一页时，窗口热度仍然撑不起首屏，整页改走库存热度。
	if req.AsOf == 0 && req.Offset == 0 && int64(len(result.Videos)) < req.Limit {
		slog.Info("popularity first page could not fill after playable filter, falling back to MySQL",
			slog.Int("redis_page", len(result.Videos)),
			slog.Int64("limit", req.Limit))
		return s.listByPersistedPopularity(ctx, req)
	}

	s.setHotCaches(ctx, req, result)
	observability.ObserveCacheLoadSeconds(observability.CacheFeedHot, time.Since(startedAt).Seconds())
	return result, nil
}

// shouldUsePersistedPopularity 判断这次热榜请求是否应整页走 MySQL。
//
// 不把 Redis 窗口分和 videos.popularity 拼进同一页：两者量纲不同，offset 分页也无法混排。
// 上一页已经回源时 as_of 为 0，翻页必须粘住，避免窗口刚好凑满后中途换排序。
func (s *Service) shouldUsePersistedPopularity(ctx context.Context, req ListByPopularityRequest) bool {
	if s.popularity == nil {
		return true
	}
	if req.AsOf == 0 && req.Offset > 0 {
		return true
	}
	if req.AsOf != 0 {
		return false
	}

	usable, err := s.popularity.HasUsableSnapshot(ctx, time.Time{})
	if err != nil {
		slog.Warn("read popularity snapshot size failed, falling back to MySQL", slog.String("error", err.Error()))
		return true
	}
	if !usable {
		slog.Info("popularity snapshot below usable threshold, falling back to MySQL")
		return true
	}
	return false
}

// listByPersistedPopularity 在 Redis 热度不可用或当前没有可用热度数据时提供稳定兜底。
func (s *Service) listByPersistedPopularity(ctx context.Context, req ListByPopularityRequest) (*ListByPopularityResult, error) {
	startedAt := time.Now()
	videos, err := s.repo.ListByPopularity(req.Limit+1, req.Offset)
	if err != nil {
		return nil, err
	}

	rawVideos, hasMore := trimVideoPage(videos, req.Limit)
	videos = s.mediaValidator.FilterPlayable(rawVideos)

	scores := make(map[uint64]int64, len(videos))
	for _, item := range videos {
		scores[item.ID] = item.Popularity
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

func (s *Service) setLatestCaches(ctx context.Context, version int64, req ListLatestRequest, result *ListLatestResult) {
	if s.latestCache != nil {
		if err := s.latestCache.Set(ctx, version, req, result); err != nil {
			slog.Warn("write latest cache failed", slog.String("error", err.Error()))
		}
	}
	if s.localLatestCache != nil {
		s.localLatestCache.Set(version, req, result)
	}
}

func (s *Service) setHotCaches(ctx context.Context, req ListByPopularityRequest, result *ListByPopularityResult) {
	if s.hotCache != nil {
		if err := s.hotCache.Set(ctx, req, result); err != nil {
			slog.Warn("write hot cache failed", slog.String("error", err.Error()))
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
	return includeTimeCursor(item, req.LatestTime, req.LastID)
}

// includeTimeCursor 判断一条视频是否落在 (created_at, id) 复合游标之后。
// ZSET 只能按毫秒分值切分，同毫秒内的多条记录必须靠 id 再比一次才不会重复或跳页。
func includeTimeCursor(item video.Video, latestTime int64, lastID uint64) bool {
	if latestTime <= 0 {
		return true
	}

	cursorTime := time.UnixMilli(latestTime)
	if item.CreatedAt.Before(cursorTime) {
		return true
	}
	if item.CreatedAt.After(cursorTime) {
		return false
	}
	if lastID == 0 {
		return true
	}
	return item.ID < lastID
}

// followingReadCount 计算一次关注流要从 ZSET 读取的候选数量。
// 放大是因为候选里混有已取关、已下架、已越过游标的条目，按页大小取会不够用。
func followingReadCount(limit int64) int64 {
	count := limit * followingReadFactor
	if count < limit {
		count = limit
	}
	if count > followingReadCap {
		count = followingReadCap
	}
	return count
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
			Tags:         []string(item.Tags),
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
