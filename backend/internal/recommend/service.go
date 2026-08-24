package recommend

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"my_feed_system/internal/config"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

// PlayableFilter 复用视频模块的媒体可读性检查；测试可注入空实现。
type PlayableFilter interface {
	FilterPlayable(items []video.Video) []video.Video
}

type Service struct {
	repo       *Repo
	embedder   Embedder
	interest   *InterestCache
	popularity *popularity.Service
	playable   PlayableFilter
	cfg        mixConfig
	model      string
}

func NewService(
	db *gorm.DB,
	embedder Embedder,
	redisClient redis.Cmdable,
	popularityService *popularity.Service,
	playable PlayableFilter,
	recCfg config.RecommendConfig,
) *Service {
	recCfg.ApplyDefaults()
	model := ""
	if embedder != nil {
		model = embedder.Model()
	}
	return &Service{
		repo:       NewRepo(db),
		embedder:   embedder,
		interest:   NewInterestCache(redisClient),
		popularity: popularityService,
		playable:   playable,
		cfg: mixConfig{
			SmallMaxFollowers: recCfg.SmallCreatorMaxFollowers,
			SlotSmallRatio:    recCfg.SlotSmallRatio,
			Lambda:            recCfg.MMRLambda,
			AuthorWindow:      authorWindow,
			NearDup:           nearDupCosine,
		},
		model: model,
	}
}

func (s *Service) modelName() string {
	if s.model != "" {
		return s.model
	}
	if s.embedder != nil {
		return s.embedder.Model()
	}
	return ""
}

func (s *Service) List(ctx context.Context, accountID uint64, req ListRequest) (*ListResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	exclude := normalizeExclude(req.ExcludeIDs)
	cands, err := s.repo.ListApprovedCandidates(exclude, accountID)
	if err != nil {
		return nil, fmt.Errorf("list recommend candidates: %w", err)
	}
	cands = s.filterPlayable(cands)
	s.attachHotScores(ctx, cands)

	tags, err := s.repo.LoadAccountInterests(accountID)
	if err != nil {
		return nil, err
	}

	picked, hasMore := s.pickByTagsOrDefault(cands, tags, int(limit))

	used := make(map[uint64]struct{}, len(picked)+len(exclude))
	for id := range exclude {
		used[id] = struct{}{}
	}
	videos := make([]video.Video, 0, len(picked))
	scores := make(map[uint64]int64, len(picked))
	nextExclude := append([]uint64{}, req.ExcludeIDs...)
	for _, item := range picked {
		videos = append(videos, item.Video)
		scores[item.Video.ID] = item.Hot
		used[item.Video.ID] = struct{}{}
		nextExclude = append(nextExclude, item.Video.ID)
	}
	if len(nextExclude) > maxExcludeIDs {
		nextExclude = nextExclude[len(nextExclude)-maxExcludeIDs:]
	}

	return &ListResult{
		Videos:     videos,
		Scores:     scores,
		ExcludeIDs: nextExclude,
		HasMore:    hasMore,
	}, nil
}

// pickByTagsOrDefault：有兴趣 tag 时先按 tag 取，不够再用默认混排补到一页。
func (s *Service) pickByTagsOrDefault(cands []candidate, tags []string, limit int) ([]candidate, bool) {
	if limit <= 0 {
		return nil, leftoverAny(cands, nil)
	}
	used := make(map[uint64]struct{}, limit)
	var picked []candidate
	if len(tags) > 0 {
		tagged := matchInterestTags(cands, tags)
		take := limit
		if take > len(tagged) {
			take = len(tagged)
		}
		picked = append(picked, tagged[:take]...)
		for _, item := range picked {
			used[item.Video.ID] = struct{}{}
		}
	}
	if len(picked) < limit {
		interest, small, hot := splitQueues(cands, nil, s.cfg)
		extra := mixExcluding(interest, small, hot, limit-len(picked), s.cfg, used)
		for _, item := range extra {
			picked = append(picked, item)
			used[item.Video.ID] = struct{}{}
		}
	}
	return picked, leftoverAny(cands, used)
}

func (s *Service) filterPlayable(cands []candidate) []candidate {
	if s.playable == nil || len(cands) == 0 {
		return cands
	}
	videos := make([]video.Video, 0, len(cands))
	for _, c := range cands {
		videos = append(videos, c.Video)
	}
	ok := map[uint64]struct{}{}
	for _, item := range s.playable.FilterPlayable(videos) {
		ok[item.ID] = struct{}{}
	}
	out := cands[:0]
	for _, c := range cands {
		if _, keep := ok[c.Video.ID]; keep {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) attachHotScores(ctx context.Context, cands []candidate) {
	if s.popularity == nil || !s.popularity.Enabled() || len(cands) == 0 {
		return
	}
	// 窗口热度不足一页时继续用 videos.popularity，避免少数近期事件和库存分混在同一尺度上。
	usable, err := s.popularity.HasUsableSnapshot(ctx, time.Time{})
	if err != nil || !usable {
		return
	}
	ids := make([]uint64, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.Video.ID)
	}
	scores, err := s.popularity.Scores(ctx, ids, time.Time{})
	if err != nil || len(scores) == 0 {
		return
	}
	for i := range cands {
		if v, ok := scores[cands[i].Video.ID]; ok {
			cands[i].Hot = v
		}
	}
}

func normalizeExclude(ids []uint64) map[uint64]struct{} {
	if len(ids) > maxExcludeIDs {
		ids = ids[len(ids)-maxExcludeIDs:]
	}
	out := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			out[id] = struct{}{}
		}
	}
	return out
}
