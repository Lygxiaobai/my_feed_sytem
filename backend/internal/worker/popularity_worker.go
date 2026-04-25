package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

type PopularityWorker struct {
	db          *gorm.DB
	videoRepo   *video.Repo
	service     *popularity.Service
	detailCache *video.DetailCache
}

func NewPopularityWorker(db *gorm.DB, service *popularity.Service, detailCache *video.DetailCache) *PopularityWorker {
	return &PopularityWorker{
		db:          db,
		videoRepo:   video.NewRepo(db),
		service:     service,
		detailCache: detailCache,
	}
}

func (w *PopularityWorker) Handle(ctx context.Context, event mq.Envelope) error {
	if event.EventType != mq.EventTypePopularityChanged {
		return fmt.Errorf("popularity worker unsupported event: %s", event.EventType)
	}

	var payload mq.PopularityChangedPayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode popularity.changed payload: %w", err)
	}
	if payload.VideoID == 0 {
		return errors.New("invalid popularity.changed payload")
	}

	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	alreadyProcessed := false
	//mysql热度持久化 原子累加 update
	if err := w.db.Transaction(func(tx *gorm.DB) error {
		if err := mq.MarkProcessed(tx, "popularity-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				alreadyProcessed = true
				return nil
			}
			return err
		}

		// 这里的 return 只是结束这个匿名函数
		return w.videoRepo.AdjustCounters(tx, payload.VideoID, 0, 0, payload.Delta)
	}); err != nil {
		return err
	}
	if alreadyProcessed {
		return nil
	}

	if w.service == nil {
		log.Printf("popularity worker: redis unavailable, persisted mysql only event_type=%s event_id=%s", event.EventType, event.EventID)
		invalidateVideoDetailCache(w.detailCache, payload.VideoID)
		return nil
	}

	if err := w.service.Record(ctx, payload.VideoID, float64(payload.Delta), occurredAt); err != nil {
		return err
	}
	invalidateVideoDetailCache(w.detailCache, payload.VideoID)

	return nil
}
