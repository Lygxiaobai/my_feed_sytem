package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"my_feed_system/internal/feed"
	"my_feed_system/internal/mq"
)

// TimelineConsumer 负责消费视频发布时间线事件并刷新最新流索引。
type TimelineConsumer struct {
	timelineStore *feed.GlobalTimelineStore
	latestCache   *feed.LatestCache
}

// NewTimelineConsumer 创建时间线事件消费者。
func NewTimelineConsumer(timelineStore *feed.GlobalTimelineStore, latestCache *feed.LatestCache) *TimelineConsumer {
	return &TimelineConsumer{
		timelineStore: timelineStore,
		latestCache:   latestCache,
	}
}

// Handle 把发布时间线事件写入全局时间线，并失效最新流分页缓存。
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
	// 优先使用业务事件自带的发布时间，缺失时再退回消息元数据或当前时间。
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
		// 时间线一旦更新，现有的最新流分页结果都可能过期，需要整体失效。
		if err := w.latestCache.DeleteAll(ctx); err != nil {
			log.Printf("timeline worker: delete latest feed cache failed, video_id=%d err=%v", payload.VideoID, err)
		}
	}

	return nil
}
