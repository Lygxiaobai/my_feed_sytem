package feed

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	globalTimelineKey = "feed:global_timeline"
	//Redis该key的最大承载数量
	defaultGlobalTimelineLimit = int64(50000)
	globalTimelineReadFactor   = int64(4)
	//最大读取容量
	globalTimelineReadCap = int64(200)
)

// GlobalTimelineStore 封装全局最新流时间线的 Redis ZSET 操作。
type GlobalTimelineStore struct {
	client  redis.Cmdable
	key     string
	maxSize int64
}

// NewGlobalTimelineStore 使用默认容量创建全局时间线存储。
func NewGlobalTimelineStore(client redis.Cmdable) *GlobalTimelineStore {
	return NewGlobalTimelineStoreWithLimit(client, defaultGlobalTimelineLimit)
}

// NewGlobalTimelineStoreWithLimit 使用指定容量创建全局时间线存储。
func NewGlobalTimelineStoreWithLimit(client redis.Cmdable, maxSize int64) *GlobalTimelineStore {
	if maxSize <= 0 {
		maxSize = defaultGlobalTimelineLimit
	}

	return &GlobalTimelineStore{
		client:  client,
		key:     globalTimelineKey,
		maxSize: maxSize,
	}
}

// Enabled 判断全局时间线存储是否可用。
func (s *GlobalTimelineStore) Enabled() bool {
	return s != nil && s.client != nil
}

// Add 把一个视频按发布时间写入全局最新流时间线。
func (s *GlobalTimelineStore) Add(ctx context.Context, videoID uint64, createdAt time.Time) error {
	if !s.Enabled() || videoID == 0 {
		return nil
	}

	score := float64(createdAt.UnixMilli())
	member := strconv.FormatUint(videoID, 10)
	if err := s.client.ZAdd(ctx, s.key, redis.Z{
		Score:  score,
		Member: member,
	}).Err(); err != nil {
		return err
	}

	if s.maxSize > 0 {
		// 只保留最近的一段全局时间线，避免 ZSET 无界增长。
		return s.client.ZRemRangeByRank(ctx, s.key, 0, -s.maxSize-1).Err()
	}

	return nil
}

// ListVideoIDs 按最新流游标从时间线中读取一批候选视频 ID。
func (s *GlobalTimelineStore) ListVideoIDs(ctx context.Context, req ListLatestRequest) ([]uint64, error) {
	if !s.Enabled() {
		return nil, nil
	}

	// 读取量会适当放大，给后续游标过滤和脏数据剔除留出余量。
	count := req.Limit * globalTimelineReadFactor
	if count < req.Limit {
		count = req.Limit
	}
	if count > globalTimelineReadCap {
		count = globalTimelineReadCap
	}

	rangeBy := &redis.ZRangeBy{
		Max:    "+inf",
		Min:    "-inf",
		Offset: 0,
		Count:  count,
	}
	if req.LatestTime > 0 {
		rangeBy.Max = strconv.FormatInt(req.LatestTime, 10)
	}

	members, err := s.client.ZRevRangeByScore(ctx, s.key, rangeBy).Result()
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(members))
	for _, member := range members {
		videoID, parseErr := strconv.ParseUint(member, 10, 64)
		if parseErr != nil {
			continue
		}
		ids = append(ids, videoID)
	}

	return ids, nil
}

// Remove 从全局时间线中移除一组已经失效的成员。
func (s *GlobalTimelineStore) Remove(ctx context.Context, videoIDs ...uint64) error {
	if !s.Enabled() || len(videoIDs) == 0 {
		return nil
	}

	members := make([]any, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		if videoID == 0 {
			continue
		}
		members = append(members, strconv.FormatUint(videoID, 10))
	}
	if len(members) == 0 {
		return nil
	}

	return s.client.ZRem(ctx, s.key, members...).Err()
}
