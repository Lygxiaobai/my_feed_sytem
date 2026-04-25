package like

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
	publisher   *mq.Publisher
}

func NewService(db *gorm.DB, popularityService *popularity.Service) *Service {
	return NewServiceWithDetailCache(db, popularityService, nil)
}

func NewServiceWithDetailCache(db *gorm.DB, popularityService *popularity.Service, detailCache *video.DetailCache) *Service {
	return NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, nil)
}

func NewServiceWithDetailCacheAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *video.DetailCache, publisher *mq.Publisher) *Service {
	return &Service{
		db:          db,
		repo:        NewRepo(db),
		videoRepo:   video.NewRepo(db),
		popularity:  popularityService,
		detailCache: detailCache,
		publisher:   publisher,
	}
}

func (s *Service) Like(accountID uint64, req LikeRequest) error {
	currentVideo, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return err
	}
	if currentVideo == nil {
		return video.ErrVideoNotFound
	}

	existing, err := s.repo.FindByVideoAndAccount(req.VideoID, accountID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrAlreadyLiked
	}

	// 兜底：未接入 MQ 时沿用同步写逻辑。
	if s.publisher == nil {
		return s.likeSync(accountID, req)
	}

	// 异步路径：发布事件后快速返回，由 Worker 落库。
	event, err := mq.NewEnvelope(mq.EventTypeLikeCreated, mq.ProducerAPIServer, mq.LikePayload{
		AccountID: accountID,
		VideoID:   req.VideoID,
	})
	if err != nil {
		return fmt.Errorf("build like.created event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.publisher.Publish(ctx, event); err != nil {
		return err
	}
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

	existing, err := s.repo.FindByVideoAndAccount(req.VideoID, accountID)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrLikeNotFound
	}

	// 兜底：未接入 MQ 时沿用同步写逻辑。
	if s.publisher == nil {
		return s.unlikeSync(accountID, req)
	}

	// 异步路径：发布事件后快速返回，由 Worker 落库。
	event, err := mq.NewEnvelope(mq.EventTypeLikeDeleted, mq.ProducerAPIServer, mq.LikePayload{
		AccountID: accountID,
		VideoID:   req.VideoID,
	})
	if err != nil {
		return fmt.Errorf("build like.deleted event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.publisher.Publish(ctx, event); err != nil {
		return err
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

func (s *Service) likeSync(accountID uint64, req LikeRequest) error {
	popularityDelta := int64(0)
	if s.popularity == nil {
		popularityDelta = int64(popularity.LikeWeight)
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&VideoLike{VideoID: req.VideoID, AccountID: accountID}).Error; err != nil {
			return err
		}

		return s.videoRepo.AdjustCounters(tx, req.VideoID, 1, 0, popularityDelta)
	}); err != nil {
		return err
	}

	if s.popularity != nil {
		_ = s.popularity.Record(context.Background(), req.VideoID, popularity.LikeWeight, time.Now())
	}
	s.invalidateDetailCache(req.VideoID)

	return nil
}

func (s *Service) unlikeSync(accountID uint64, req LikeRequest) error {
	popularityDelta := int64(0)
	if s.popularity == nil {
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

		return s.videoRepo.AdjustCounters(tx, req.VideoID, -1, 0, popularityDelta)
	}); err != nil {
		return err
	}

	if s.popularity != nil {
		_ = s.popularity.Record(context.Background(), req.VideoID, popularity.UnlikeWeight, time.Now())
	}
	s.invalidateDetailCache(req.VideoID)

	return nil
}

func (s *Service) invalidateDetailCache(videoID uint64) {
	if s.detailCache == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.detailCache.Delete(ctx, videoID); err != nil {
		log.Printf("like service: delete detail cache failed for video %d: %v", videoID, err)
	}
}
