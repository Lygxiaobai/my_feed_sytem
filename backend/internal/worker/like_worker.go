package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"my_feed_system/internal/like"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

type LikeWorker struct {
	db          *gorm.DB
	videoRepo   *video.Repo
	detailCache *video.DetailCache
	publisher   *mq.Publisher
}

func NewLikeWorker(db *gorm.DB, publisher *mq.Publisher, detailCache *video.DetailCache) *LikeWorker {
	return &LikeWorker{
		db:          db,
		videoRepo:   video.NewRepo(db),
		detailCache: detailCache,
		publisher:   publisher,
	}
}

func (w *LikeWorker) Handle(ctx context.Context, event mq.Envelope) error {
	switch event.EventType {
	case mq.EventTypeLikeCreated:
		return w.handleLikeCreated(ctx, event)
	case mq.EventTypeLikeDeleted:
		return w.handleLikeDeleted(ctx, event)
	default:
		return fmt.Errorf("like worker unsupported event: %s", event.EventType)
	}
}

func (w *LikeWorker) handleLikeCreated(ctx context.Context, event mq.Envelope) error {
	var payload mq.LikePayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode like.created payload: %w", err)
	}
	if payload.AccountID == 0 || payload.VideoID == 0 {
		return errors.New("invalid like.created payload")
	}

	shouldEmit := false
	if err := w.db.Transaction(func(tx *gorm.DB) error {
		// 第一步：先过幂等门。
		if err := mq.MarkProcessed(tx, "like-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}

		err := tx.Create(&like.VideoLike{
			VideoID:   payload.VideoID,
			AccountID: payload.AccountID,
		}).Error
		if err != nil {
			// 点赞关系唯一键冲突，说明状态已收敛，按幂等成功处理。
			if isDuplicateKey(err) {
				return nil
			}
			return err
		}

		if err := w.videoRepo.AdjustCounters(tx, payload.VideoID, 1, 0, 0); err != nil {
			return err
		}

		shouldEmit = true
		return nil
	}); err != nil {
		return err
	}

	if shouldEmit {
		// 仅在主事务提交后再发送副作用事件。
		publishPopularityChanged(ctx, w.publisher, mq.PopularityChangedPayload{
			VideoID: payload.VideoID,
			Delta:   int64(popularity.LikeWeight),
			Reason:  mq.EventTypeLikeCreated,
		})
		invalidateVideoDetailCache(w.detailCache, payload.VideoID)
	}

	return nil
}

func (w *LikeWorker) handleLikeDeleted(ctx context.Context, event mq.Envelope) error {
	var payload mq.LikePayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode like.deleted payload: %w", err)
	}
	if payload.AccountID == 0 || payload.VideoID == 0 {
		return errors.New("invalid like.deleted payload")
	}

	shouldEmit := false
	if err := w.db.Transaction(func(tx *gorm.DB) error {
		// 第一步：先过幂等门。
		if err := mq.MarkProcessed(tx, "like-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}

		result := tx.Where("video_id = ? AND account_id = ?", payload.VideoID, payload.AccountID).Delete(&like.VideoLike{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 已删除视为幂等成功。
			return nil
		}

		if err := w.videoRepo.AdjustCounters(tx, payload.VideoID, -1, 0, 0); err != nil {
			return err
		}

		shouldEmit = true
		return nil
	}); err != nil {
		return err
	}

	if shouldEmit {
		// 仅在主事务提交后再发送副作用事件。
		publishPopularityChanged(ctx, w.publisher, mq.PopularityChangedPayload{
			VideoID: payload.VideoID,
			Delta:   int64(popularity.UnlikeWeight),
			Reason:  mq.EventTypeLikeDeleted,
		})
		invalidateVideoDetailCache(w.detailCache, payload.VideoID)
	}

	return nil
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "UNIQUE constraint failed")
}
