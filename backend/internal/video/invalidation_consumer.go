package video

import (
	"context"
	"fmt"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
)

// DetailInvalidationConsumer 负责消费 video.detail 的缓存失效广播。
//
// 它只处理“删本机 L1”这一件事：
// 1. Redis L2 会在写路径里先被主动删除；
// 2. RabbitMQ fanout 事件只负责通知每个 API 实例清理自己的进程内缓存；
// 3. 这样可以避免跨实例共享本地缓存失效状态，同时不影响主写链路。
type DetailInvalidationConsumer struct {
	localCache *LocalDetailCache
}

// NewDetailInvalidationConsumer 创建一个只感知本机 LocalDetailCache 的失效消费者。
func NewDetailInvalidationConsumer(localCache *LocalDetailCache) *DetailInvalidationConsumer {
	return &DetailInvalidationConsumer{localCache: localCache}
}

// Handle 处理 fanout 交换机上广播出来的 cache.invalidated 事件。
//
// 这里会显式过滤两层条件：
// 1. 只接受 event_type = cache.invalidated 的事件；
// 2. 只处理 cache = video.detail 的载荷。
//
// 这样同一个广播通道就可以复用给多类缓存失效事件，而当前消费者只关心自己负责的那一类。
func (c *DetailInvalidationConsumer) Handle(_ context.Context, event mq.Envelope) error {
	if event.EventType != mq.EventTypeCacheInvalidated {
		// 不是缓存失效事件时直接忽略，交由同通道上的其他消费者逻辑处理。
		return nil
	}

	var payload mq.CacheInvalidatedPayload
	if err := event.DecodePayload(&payload); err != nil {
		// 载荷无法解析时返回错误，让上层决定 nack/记录异常，避免静默吞掉坏消息。
		return fmt.Errorf("decode cache.invalidated payload: %w", err)
	}
	if payload.Cache != mq.CacheNameVideoDetail {
		// 同一个 fanout exchange 上还会广播其他缓存类型，这里只处理 video.detail。
		return nil
	}
	if payload.VideoID == 0 {
		// video.detail 的失效事件必须携带 video_id，否则无法定位本地缓存 key。
		return fmt.Errorf("invalid video.detail cache invalidation payload")
	}

	if c.localCache != nil {
		// 这里只删本机 L1；不会反向操作 Redis，也不会触发新的广播，避免形成回环。
		c.localCache.Delete(payload.VideoID)
	}
	// 记录一次“事件驱动的 L1 失效”，方便区分写路径删缓存和广播删缓存的效果。
	observability.IncCacheInvalidation(observability.CacheVideoDetail, "l1", "event")
	return nil
}
