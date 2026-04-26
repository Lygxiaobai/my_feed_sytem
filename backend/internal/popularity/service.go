package popularity

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultWindowMinutes = 60
	defaultBucketTTL     = 2 * time.Hour
	defaultSnapshotTTL   = 2 * time.Minute
	defaultEventTTL      = 3 * time.Hour
)

const (
	PublishWeight        float64 = 2
	LikeWeight           float64 = 3
	UnlikeWeight         float64 = -3
	CommentPublishWeight float64 = 5
	CommentDeleteWeight  float64 = -5
)

// Service 负责热度事件写入、窗口聚合和热榜快照读取。
type Service struct {
	client        redis.Cmdable
	windowMinutes int
	bucketTTL     time.Duration
	snapshotTTL   time.Duration
}

// NewService 使用默认窗口和 TTL 配置创建热度服务。
func NewService(client redis.Cmdable) *Service {
	return NewServiceWithOptions(client, defaultWindowMinutes, defaultBucketTTL, defaultSnapshotTTL)
}

// NewServiceWithOptions 使用指定窗口和 TTL 配置创建热度服务。
func NewServiceWithOptions(client redis.Cmdable, windowMinutes int, bucketTTL time.Duration, snapshotTTL time.Duration) *Service {
	if windowMinutes <= 0 {
		windowMinutes = defaultWindowMinutes
	}
	if bucketTTL <= 0 {
		bucketTTL = defaultBucketTTL
	}
	if snapshotTTL <= 0 {
		snapshotTTL = defaultSnapshotTTL
	}

	return &Service{
		client:        client,
		windowMinutes: windowMinutes,
		bucketTTL:     bucketTTL,
		snapshotTTL:   snapshotTTL,
	}
}

// Enabled 判断热度服务是否真正可用。
func (s *Service) Enabled() bool {
	return s != nil && s.client != nil
}

// Record 把一次热度变化写入对应分钟桶。
func (s *Service) Record(ctx context.Context, videoID uint64, delta float64, occurredAt time.Time) error {
	if !s.Enabled() || delta == 0 {
		return nil
	}

	// 每分钟一个桶，把“这一分钟新增了多少热度”写进去。
	key := s.bucketKey(occurredAt)
	member := strconv.FormatUint(videoID, 10)

	pipe := s.client.TxPipeline()
	pipe.ZIncrBy(ctx, key, delta, member)
	pipe.Expire(ctx, key, s.bucketTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// RecordEvent writes a popularity delta into the minute bucket exactly once for the same event ID.
func (s *Service) RecordEvent(ctx context.Context, eventID string, videoID uint64, delta float64, occurredAt time.Time) error {
	if !s.Enabled() || delta == 0 {
		return nil
	}

	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return s.Record(ctx, videoID, delta, occurredAt)
	}

	_, err := s.client.Eval(ctx, `
local claimed = redis.call("SET", KEYS[1], "1", "NX", "EX", ARGV[1])
if not claimed then
	return 0
end
redis.call("ZINCRBY", KEYS[2], ARGV[2], ARGV[3])
redis.call("EXPIRE", KEYS[2], ARGV[4])
return 1
`, []string{s.eventKey(eventID), s.bucketKey(occurredAt)},
		int64(s.eventTTL()/time.Second),
		strconv.FormatFloat(delta, 'f', -1, 64),
		strconv.FormatUint(videoID, 10),
		int64(s.bucketTTL/time.Second),
	).Result()
	return err
}

// Scores 从指定快照中批量读取视频热度分值。
func (s *Service) Scores(ctx context.Context, videoIDs []uint64, asOf time.Time) (map[uint64]int64, error) {
	scores := make(map[uint64]int64, len(videoIDs))
	if !s.Enabled() || len(videoIDs) == 0 {
		return scores, nil
	}

	snapshotKey, err := s.ensureSnapshot(ctx, asOf)
	if err != nil {
		return nil, err
	}

	pipe := s.client.Pipeline()
	cmds := make(map[uint64]*redis.FloatCmd, len(videoIDs))
	for _, videoID := range videoIDs {
		cmds[videoID] = pipe.ZScore(ctx, snapshotKey, strconv.FormatUint(videoID, 10))
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	for videoID, cmd := range cmds {
		score, err := cmd.Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, err
		}
		scores[videoID] = int64(score)
	}

	return scores, nil
}

// ListHot 从某个热度快照中读取热榜分页结果。
func (s *Service) ListHot(ctx context.Context, asOf time.Time, limit int64, offset int64) ([]uint64, map[uint64]int64, int64, error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	snapshotTime := s.snapshotTime(asOf)
	if !s.Enabled() {
		return []uint64{}, map[uint64]int64{}, snapshotTime.UnixMilli(), nil
	}

	// 先把最近窗口合并成一个短生命周期快照，再按 offset 读取榜单。
	snapshotKey, err := s.ensureSnapshot(ctx, snapshotTime)
	if err != nil {
		return nil, nil, 0, err
	}

	items, err := s.client.ZRevRangeWithScores(ctx, snapshotKey, offset, offset+limit-1).Result()
	if err != nil {
		return nil, nil, 0, err
	}

	videoIDs := make([]uint64, 0, len(items))
	scores := make(map[uint64]int64, len(items))
	for _, item := range items {
		member, ok := item.Member.(string)
		if !ok {
			continue
		}

		videoID, err := strconv.ParseUint(member, 10, 64)
		if err != nil {
			continue
		}

		videoIDs = append(videoIDs, videoID)
		scores[videoID] = int64(item.Score)
	}

	return videoIDs, scores, snapshotTime.UnixMilli(), nil
}

// ensureSnapshot 生成或复用某个时间点对应的热榜快照。
func (s *Service) ensureSnapshot(ctx context.Context, asOf time.Time) (string, error) {
	snapshotTime := s.snapshotTime(asOf)
	bucketAnchor := s.bucketAnchor(snapshotTime)
	snapshotKey := s.snapshotKey(snapshotTime)

	exists, err := s.client.Exists(ctx, snapshotKey).Result()
	if err != nil {
		return "", err
	}
	if exists > 0 {
		return snapshotKey, nil
	}

	// 把最近 N 个分钟桶聚合成一个热榜快照，后续分页都读取同一个快照。
	keys := make([]string, 0, s.windowMinutes)
	for i := 0; i < s.windowMinutes; i++ {
		keys = append(keys, s.bucketKey(bucketAnchor.Add(-time.Duration(i)*time.Minute)))
	}

	store := &redis.ZStore{
		Keys:      keys,
		Aggregate: "SUM",
	}
	if err := s.client.ZUnionStore(ctx, snapshotKey, store).Err(); err != nil {
		return "", err
	}
	if err := s.client.Expire(ctx, snapshotKey, s.snapshotTTL).Err(); err != nil {
		return "", err
	}

	return snapshotKey, nil
}

// snapshotTime 统一热榜快照的时间基准，默认取当前 UTC 时间。
func (s *Service) snapshotTime(asOf time.Time) time.Time {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	return asOf.UTC()
}

// bucketAnchor 返回热度桶的分钟级对齐时间。
func (s *Service) bucketAnchor(asOf time.Time) time.Time {
	return s.snapshotTime(asOf).Truncate(time.Minute)
}

// bucketKey 生成分钟桶对应的 Redis key。
func (s *Service) bucketKey(ts time.Time) string {
	return fmt.Sprintf("hot:video:1m:%s", ts.UTC().Format("200601021504"))
}

func (s *Service) eventKey(eventID string) string {
	return fmt.Sprintf("hot:video:event:%s", eventID)
}

func (s *Service) eventTTL() time.Duration {
	if s.bucketTTL <= 0 {
		return defaultEventTTL
	}
	return s.bucketTTL + time.Hour
}

// snapshotKey 生成热榜快照对应的 Redis key。
func (s *Service) snapshotKey(asOf time.Time) string {
	return fmt.Sprintf("hot:video:merge:1m:%d:w%d", s.snapshotTime(asOf).UnixMilli(), s.windowMinutes)
}
