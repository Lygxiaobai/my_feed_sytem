package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"my_feed_system/internal/comment"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/notification"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

type CommentWorker struct {
	db          *gorm.DB
	repo        *comment.Repo
	videoRepo   *video.Repo
	detailCache *video.DetailCache
	publisher   *mq.Publisher
	notify      *notification.Writer
	recorder    interestRecorder
}

type interestRecorder interface {
	RecordFromVideo(accountID uint64, tags []string) error
}

func (w *CommentWorker) SetNotifier(n *notification.Writer) {
	w.notify = n
}

func (w *CommentWorker) SetInterestRecorder(v interestRecorder) {
	w.recorder = v
}

func NewCommentWorker(db *gorm.DB, publisher *mq.Publisher, detailCache *video.DetailCache) *CommentWorker {
	return &CommentWorker{
		db:          db,
		repo:        comment.NewRepo(db),
		videoRepo:   video.NewRepo(db),
		detailCache: detailCache,
		publisher:   publisher,
	}
}

func (w *CommentWorker) Handle(ctx context.Context, event mq.Envelope) error {
	switch event.EventType {
	case mq.EventTypeCommentCreated:
		return w.handleCommentCreated(ctx, event)
	case mq.EventTypeCommentDeleted:
		return w.handleCommentDeleted(ctx, event)
	default:
		return fmt.Errorf("comment worker unsupported event: %s", event.EventType)
	}
}

func (w *CommentWorker) handleCommentCreated(ctx context.Context, event mq.Envelope) error {
	var payload mq.CommentCreatedPayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode comment.created payload: %w", err)
	}
	if payload.CommentID == 0 || payload.VideoID == 0 || payload.AuthorID == 0 {
		return errors.New("invalid comment.created payload")
	}

	shouldEmit := false
	if err := w.db.Transaction(func(tx *gorm.DB) error {
		// Step 1: idempotency gate.
		if err := mq.MarkProcessed(tx, "comment-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}

		err := tx.Create(&comment.VideoComment{
			ID:              payload.CommentID,
			VideoID:         payload.VideoID,
			RootCommentID:   payload.RootCommentID,
			ParentCommentID: payload.ParentCommentID,
			AuthorID:        payload.AuthorID,
			Username:        payload.Username,
			ReplyToUserID:   payload.ReplyToUserID,
			ReplyToUsername: payload.ReplyToUsername,
			Content:         payload.Content,
		}).Error
		if err != nil {
			// Duplicate comment ID is treated as already applied.
			if isDuplicateKey(err) {
				return nil
			}
			return err
		}

		if err := w.videoRepo.AdjustCounters(tx, payload.VideoID, 0, 1, 0); err != nil {
			return err
		}

		currentVideo, lookupErr := w.videoRepo.FindByID(payload.VideoID)
		if lookupErr != nil {
			return lookupErr
		}
		videoAuthorID := uint64(0)
		if currentVideo != nil {
			videoAuthorID = currentVideo.AuthorID
		}
		if err := w.notify.ApplyComment(tx, notification.CommentFanout{
			CommentID:       payload.CommentID,
			VideoID:         payload.VideoID,
			ActorID:         payload.AuthorID,
			VideoAuthorID:   videoAuthorID,
			ParentCommentID: payload.ParentCommentID,
			RootCommentID:   payload.RootCommentID,
			ReplyToUserID:   payload.ReplyToUserID,
			Content:         payload.Content,
		}); err != nil {
			return err
		}

		shouldEmit = true
		return nil
	}); err != nil {
		return err
	}

	if shouldEmit {
		// Emit side effect event only after DB transaction is committed.
		publishPopularityChanged(ctx, w.publisher, mq.PopularityChangedPayload{
			VideoID: payload.VideoID,
			Delta:   int64(popularity.CommentPublishWeight),
			Reason:  mq.EventTypeCommentCreated,
		})
		invalidateVideoDetailCache(w.detailCache, w.publisher, payload.VideoID)
		w.recordInterest(payload.AuthorID, payload.VideoID)
	}

	return nil
}

func (w *CommentWorker) recordInterest(accountID, videoID uint64) {
	if w.recorder == nil || accountID == 0 || videoID == 0 {
		return
	}
	item, err := w.videoRepo.FindByID(videoID)
	if err != nil || item == nil {
		return
	}
	if err := w.recorder.RecordFromVideo(accountID, []string(item.Tags)); err != nil {
		slog.Warn("record interest tags failed",
			slog.Uint64("account_id", accountID),
			slog.Uint64("video_id", videoID),
			slog.String("error", err.Error()))
	}
}

func (w *CommentWorker) handleCommentDeleted(ctx context.Context, event mq.Envelope) error {
	var payload mq.CommentDeletedPayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode comment.deleted payload: %w", err)
	}
	if payload.CommentID == 0 || payload.VideoID == 0 {
		return errors.New("invalid comment.deleted payload")
	}

	deletedCount := int64(0)
	if err := w.db.Transaction(func(tx *gorm.DB) error {
		// Step 1: idempotency gate.
		if err := mq.MarkProcessed(tx, "comment-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}

		var err error
		deletedCount, err = w.repo.DeleteByIDOrRootID(tx, payload.CommentID)
		if err != nil {
			return err
		}
		if deletedCount == 0 {
			// Already deleted, keep idempotent.
			return nil
		}
		if err := w.notify.HideByComment(tx, payload.CommentID); err != nil {
			return err
		}

		return w.videoRepo.AdjustCounters(tx, payload.VideoID, 0, -deletedCount, 0)
	}); err != nil {
		return err
	}

	if deletedCount > 0 {
		// One delete request can remove a subtree, so delta scales with deletedCount.
		publishPopularityChanged(ctx, w.publisher, mq.PopularityChangedPayload{
			VideoID: payload.VideoID,
			Delta:   int64(popularity.CommentDeleteWeight) * deletedCount,
			Reason:  mq.EventTypeCommentDeleted,
		})
		invalidateVideoDetailCache(w.detailCache, w.publisher, payload.VideoID)
	}

	return nil
}
