package like

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/notification"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

var (
	ErrAlreadyLiked = errors.New("video already liked")
	ErrLikeNotFound = errors.New("like record not found")
)

type Service struct {
	db          *gorm.DB
	repo        *Repo
	videoRepo   *video.Repo
	popularity  *popularity.Service
	detailCache *video.DetailCache
	localDetail *video.LocalDetailCache
	publisher   *mq.Publisher
	outboxRepo  *outbox.Repo
	notify      *notification.Writer
	interest    interestInvalidator
	recorder    interestRecorder
}

type interestInvalidator interface {
	InvalidateUser(accountID uint64)
}

type interestRecorder interface {
	RecordFromVideo(accountID uint64, tags []string) error
}

func (s *Service) SetNotifier(n *notification.Writer) {
	s.notify = n
}

func (s *Service) SetInterestInvalidator(v interestInvalidator) {
	s.interest = v
}

func (s *Service) SetInterestRecorder(v interestRecorder) {
	s.recorder = v
}

func NewService(db *gorm.DB, popularityService *popularity.Service) *Service {
	return NewServiceWithDetailCache(db, popularityService, nil)
}

func NewServiceWithDetailCache(db *gorm.DB, popularityService *popularity.Service, detailCache *video.DetailCache) *Service {
	return NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, nil)
}

func NewServiceWithDetailCacheAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *video.DetailCache, publisher *mq.Publisher) *Service {
	return NewServiceWithCachesAndPublisher(db, popularityService, detailCache, nil, publisher)
}

func NewServiceWithCachesAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *video.DetailCache, localDetail *video.LocalDetailCache, publisher *mq.Publisher) *Service {
	return &Service{
		db:          db,
		repo:        NewRepo(db),
		videoRepo:   video.NewRepo(db),
		popularity:  popularityService,
		detailCache: detailCache,
		localDetail: localDetail,
		publisher:   publisher,
		outboxRepo:  outbox.NewRepo(db),
	}
}

func (s *Service) Like(accountID uint64, req LikeRequest) error {
	currentVideo, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return err
	}
	if currentVideo == nil || !currentVideo.IsPubliclyListed() {
		return video.ErrVideoNotFound
	}

	popularityDelta := int64(0)
	if s.publisher == nil && s.popularity == nil {
		popularityDelta = int64(popularity.LikeWeight)
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing VideoLike
		queryErr := tx.Where("video_id = ? AND account_id = ?", req.VideoID, accountID).First(&existing).Error
		if queryErr == nil {
			return ErrAlreadyLiked
		}
		if !errors.Is(queryErr, gorm.ErrRecordNotFound) {
			return queryErr
		}

		if err := tx.Create(&VideoLike{VideoID: req.VideoID, AccountID: accountID}).Error; err != nil {
			if isDuplicateKey(err) {
				return ErrAlreadyLiked
			}
			return err
		}
		if err := s.videoRepo.AdjustCounters(tx, req.VideoID, 1, 0, popularityDelta); err != nil {
			return err
		}

		if err := s.notify.ApplyLike(tx, accountID, req.VideoID, currentVideo.AuthorID); err != nil {
			return err
		}

		if s.publisher != nil {
			event, err := mq.NewEnvelope(mq.EventTypePopularityChanged, mq.ProducerAPIServer, mq.PopularityChangedPayload{
				VideoID: req.VideoID,
				Delta:   int64(popularity.LikeWeight),
				Reason:  mq.EventTypeLikeCreated,
			})
			if err != nil {
				return fmt.Errorf("build popularity.changed event: %w", err)
			}
			if err := s.outboxRepo.Enqueue(tx, event); err != nil {
				return fmt.Errorf("enqueue popularity.changed event: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if s.publisher == nil && s.popularity != nil {
		_ = s.popularity.Record(context.Background(), req.VideoID, popularity.LikeWeight, time.Now())
	}
	s.invalidateDetailCache(req.VideoID)
	if s.interest != nil {
		s.interest.InvalidateUser(accountID)
	}
	s.recordInterest(accountID, currentVideo)
	return nil
}

func (s *Service) Unlike(accountID uint64, req LikeRequest) error {
	currentVideo, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return err
	}
	if currentVideo == nil {
		return video.ErrVideoNotFound
	}

	popularityDelta := int64(0)
	if s.publisher == nil && s.popularity == nil {
		popularityDelta = int64(popularity.UnlikeWeight)
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("video_id = ? AND account_id = ?", req.VideoID, accountID).Delete(&VideoLike{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrLikeNotFound
		}
		if err := s.videoRepo.AdjustCounters(tx, req.VideoID, -1, 0, popularityDelta); err != nil {
			return err
		}

		if err := s.notify.RetractLike(tx, accountID, req.VideoID, currentVideo.AuthorID); err != nil {
			return err
		}

		if s.publisher != nil {
			event, err := mq.NewEnvelope(mq.EventTypePopularityChanged, mq.ProducerAPIServer, mq.PopularityChangedPayload{
				VideoID: req.VideoID,
				Delta:   int64(popularity.UnlikeWeight),
				Reason:  mq.EventTypeLikeDeleted,
			})
			if err != nil {
				return fmt.Errorf("build popularity.changed event: %w", err)
			}
			if err := s.outboxRepo.Enqueue(tx, event); err != nil {
				return fmt.Errorf("enqueue popularity.changed event: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if s.publisher == nil && s.popularity != nil {
		_ = s.popularity.Record(context.Background(), req.VideoID, popularity.UnlikeWeight, time.Now())
	}
	s.invalidateDetailCache(req.VideoID)
	if s.interest != nil {
		s.interest.InvalidateUser(accountID)
	}
	return nil
}

func (s *Service) IsLiked(accountID uint64, req LikeRequest) (bool, error) {
	currentVideo, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return false, err
	}
	if currentVideo == nil {
		return false, video.ErrVideoNotFound
	}

	record, err := s.repo.FindByVideoAndAccount(req.VideoID, accountID)
	if err != nil {
		return false, err
	}

	return record != nil, nil
}

func (s *Service) ListLikedVideoIDs(accountID uint64, videoIDs []uint64) ([]uint64, error) {
	if len(videoIDs) == 0 {
		return []uint64{}, nil
	}

	return s.repo.FindLikedVideoIDs(accountID, videoIDs)
}

func (s *Service) recordInterest(accountID uint64, item *video.Video) {
	if s.recorder == nil || item == nil {
		return
	}
	if err := s.recorder.RecordFromVideo(accountID, []string(item.Tags)); err != nil {
		slog.Warn("record interest tags failed",
			slog.Uint64("account_id", accountID),
			slog.Uint64("video_id", item.ID),
			slog.String("error", err.Error()))
	}
}

func (s *Service) invalidateDetailCache(videoID uint64) {
	if s.detailCache == nil && s.localDetail == nil && s.publisher == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if s.detailCache != nil {
		if err := s.detailCache.Delete(ctx, videoID); err != nil {
			slog.Warn("delete detail cache failed", slog.Uint64("video_id", videoID), slog.String("error", err.Error()))
		} else {
			observability.IncCacheInvalidation(observability.CacheVideoDetail, "l2", "write")
		}
	}
	if s.localDetail != nil {
		s.localDetail.Delete(videoID)
	}
	if s.publisher != nil {
		if err := s.publisher.PublishCacheInvalidated(ctx, mq.CacheInvalidatedPayload{
			Cache:   mq.CacheNameVideoDetail,
			VideoID: videoID,
		}); err != nil {
			slog.Warn("publish detail invalidation failed", slog.Uint64("video_id", videoID), slog.String("error", err.Error()))
		}
	}
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "Duplicate entry") || strings.Contains(message, "UNIQUE constraint failed")
}
