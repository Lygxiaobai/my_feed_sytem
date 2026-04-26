package feed

import (
	"context"
	"fmt"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
)

type LatestInvalidationConsumer struct {
	localCache *LocalLatestPageCache
}

func NewLatestInvalidationConsumer(localCache *LocalLatestPageCache) *LatestInvalidationConsumer {
	return &LatestInvalidationConsumer{localCache: localCache}
}

func (c *LatestInvalidationConsumer) Handle(_ context.Context, event mq.Envelope) error {
	if event.EventType != mq.EventTypeCacheInvalidated {
		return nil
	}

	var payload mq.CacheInvalidatedPayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode cache.invalidated payload: %w", err)
	}
	if payload.Cache != mq.CacheNameFeedLatest {
		return nil
	}

	if c.localCache != nil {
		// API 实例只删除本机 L1，Redis L2 已由版本号切换完成隔离。
		c.localCache.Clear()
	}
	observability.IncCacheInvalidation(observability.CacheFeedLatest, "l1", "event")
	return nil
}
