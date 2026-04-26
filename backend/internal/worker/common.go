package worker

import (
	"context"
	"log"
	"time"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/video"
)

func invalidateVideoDetailCache(cache *video.DetailCache, publisher *mq.Publisher, videoID uint64) {
	if cache == nil && publisher == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if cache != nil {
		if err := cache.Delete(ctx, videoID); err != nil {
			log.Printf("worker: invalidate video detail cache failed, video_id=%d err=%v", videoID, err)
		} else {
			observability.IncCacheInvalidation(observability.CacheVideoDetail, "l2", "write")
		}
	}
	if publisher != nil {
		if err := publisher.PublishCacheInvalidated(ctx, mq.CacheInvalidatedPayload{
			Cache:   mq.CacheNameVideoDetail,
			VideoID: videoID,
		}); err != nil {
			log.Printf("worker: publish detail invalidation failed, video_id=%d err=%v", videoID, err)
		}
	}
}

func publishPopularityChanged(ctx context.Context, publisher *mq.Publisher, payload mq.PopularityChangedPayload) {
	// 热度事件属于副作用，尽量不影响主写链路。
	if publisher == nil {
		return
	}

	env, err := mq.NewEnvelope(mq.EventTypePopularityChanged, mq.ProducerWorker, payload)
	if err != nil {
		log.Printf("worker: build popularity event failed: %v", err)
		return
	}
	if err := publisher.Publish(ctx, env); err != nil {
		log.Printf("worker: publish popularity event failed, video_id=%d delta=%d err=%v", payload.VideoID, payload.Delta, err)
	}
}
