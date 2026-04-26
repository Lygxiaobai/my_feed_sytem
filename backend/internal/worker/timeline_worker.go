package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"my_feed_system/internal/feed"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
)

type TimelineConsumer struct {
	timelineStore *feed.GlobalTimelineStore
	latestCache   *feed.LatestCache
	publisher     *mq.Publisher
}

func NewTimelineConsumer(timelineStore *feed.GlobalTimelineStore, latestCache *feed.LatestCache, publisher *mq.Publisher) *TimelineConsumer {
	return &TimelineConsumer{
		timelineStore: timelineStore,
		latestCache:   latestCache,
		publisher:     publisher,
	}
}

func (w *TimelineConsumer) Handle(ctx context.Context, event mq.Envelope) error {
	if event.EventType != mq.EventTypeVideoTimelinePush {
		return fmt.Errorf("timeline worker unsupported event: %s", event.EventType)
	}
	if w.timelineStore == nil || !w.timelineStore.Enabled() {
		return errors.New("global timeline store unavailable")
	}

	var payload mq.VideoTimelinePayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode video.timeline.publish payload: %w", err)
	}
	if payload.VideoID == 0 {
		return errors.New("invalid video.timeline.publish payload")
	}

	createdAt := payload.CreatedAt
	if createdAt.IsZero() {
		createdAt = event.OccurredAt
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	if err := w.timelineStore.Add(ctx, payload.VideoID, createdAt); err != nil {
		return err
	}

	if w.latestCache != nil {
		// 发布新视频后只 bump latest 版本号，不再扫描删除历史分页 key。
		version, err := w.latestCache.BumpVersion(ctx)
		if err != nil {
			return err
		}
		observability.IncCacheInvalidation(observability.CacheFeedLatest, "l2", "write")
		if w.publisher != nil {
			// fanout 只负责通知各 API 实例清本机 L1，不阻塞主写链路。
			_ = w.publisher.PublishCacheInvalidated(ctx, mq.CacheInvalidatedPayload{
				Cache:   mq.CacheNameFeedLatest,
				Version: version,
			})
		}
	}

	return nil
}
