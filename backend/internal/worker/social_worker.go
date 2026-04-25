package worker

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/social"
)

type SocialWorker struct {
	db *gorm.DB
}

func NewSocialWorker(db *gorm.DB) *SocialWorker {
	return &SocialWorker{db: db}
}

func (w *SocialWorker) Handle(_ context.Context, event mq.Envelope) error {
	switch event.EventType {
	case mq.EventTypeSocialFollowed:
		return w.handleFollowed(event)
	case mq.EventTypeSocialUnfollowed:
		return w.handleUnfollowed(event)
	default:
		return fmt.Errorf("social worker unsupported event: %s", event.EventType)
	}
}

func (w *SocialWorker) handleFollowed(event mq.Envelope) error {
	var payload mq.SocialPayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode social.followed payload: %w", err)
	}
	if payload.FollowerID == 0 || payload.VloggerID == 0 {
		return errors.New("invalid social.followed payload")
	}

	return w.db.Transaction(func(tx *gorm.DB) error {
		// Step 1: idempotency gate.
		if err := mq.MarkProcessed(tx, "social-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}

		err := tx.Create(&social.SocialRelation{
			FollowerID: payload.FollowerID,
			VloggerID:  payload.VloggerID,
		}).Error
		if err != nil && !isDuplicateKey(err) {
			return err
		}

		return nil
	})
}

func (w *SocialWorker) handleUnfollowed(event mq.Envelope) error {
	var payload mq.SocialPayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode social.unfollowed payload: %w", err)
	}
	if payload.FollowerID == 0 || payload.VloggerID == 0 {
		return errors.New("invalid social.unfollowed payload")
	}

	return w.db.Transaction(func(tx *gorm.DB) error {
		// Step 1: idempotency gate.
		if err := mq.MarkProcessed(tx, "social-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}

		return tx.Where("follower_id = ? AND vlogger_id = ?", payload.FollowerID, payload.VloggerID).Delete(&social.SocialRelation{}).Error
	})
}
