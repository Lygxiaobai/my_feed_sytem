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
	db             *gorm.DB
	videoRepo      *video.Repo
	service        *popularity.Service
	projectionRepo *popularity.ProjectionRepo
	detailCache    *video.DetailCache
}

func NewPopularityWorker(db *gorm.DB, service *popularity.Service, detailCache *video.DetailCache) *PopularityWorker {
	return &PopularityWorker{
		db:             db,
		videoRepo:      video.NewRepo(db),
		service:        service,
		projectionRepo: popularity.NewProjectionRepo(db),
		detailCache:    detailCache,
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
	var projection *popularity.Projection
	if err := w.db.Transaction(func(tx *gorm.DB) error {
		if err := mq.MarkProcessed(tx, "popularity-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				alreadyProcessed = true
				return nil
			}
			return err
		}

		if err := w.videoRepo.AdjustCounters(tx, payload.VideoID, 0, 0, payload.Delta); err != nil {
			return err
		}

		projection = popularity.NewProjection(event, payload, occurredAt)
		return w.projectionRepo.Enqueue(tx, projection)
	}); err != nil {
		return err
	}
	if alreadyProcessed {
		return nil
	}

	if w.service == nil {
		log.Printf("popularity worker: redis unavailable, queued replay after mysql commit event_type=%s event_id=%s", event.EventType, event.EventID)
		invalidateVideoDetailCache(w.detailCache, nil, payload.VideoID)
		return nil
	}

	if err := w.service.RecordEvent(ctx, projection.EventID, projection.VideoID, float64(projection.Delta), projection.OccurredAt); err != nil {
		log.Printf("popularity worker: defer redis projection to poller event_type=%s event_id=%s err=%v", event.EventType, event.EventID, err)
		invalidateVideoDetailCache(w.detailCache, nil, payload.VideoID)
		return nil
	}
	if err := w.projectionRepo.Delete(projection.ID); err != nil {
		log.Printf("popularity worker: delete applied projection failed, poller will retry cleanup event_id=%s err=%v", projection.EventID, err)
	}
	invalidateVideoDetailCache(w.detailCache, nil, payload.VideoID)

	return nil
}
