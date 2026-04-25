package video

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/idempotency"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
)

var (
	ErrVideoNotFound              = errors.New("video not found")
	ErrIdempotencyKeyRequired     = errors.New("idempotency key is required")
	ErrIdempotencyKeyTooLong      = errors.New("idempotency key is too long")
	ErrIdempotencyRequestConflict = errors.New("idempotency key already used with different request")
	ErrIdempotencyRequestBusy     = errors.New("request with the same idempotency key is still processing")
)

const (
	videoPublishBizType     = "video.publish"
	maxIdempotencyKeyLength = 128
)

// Service encapsulates video publish and query workflows.
type Service struct {
	db              *gorm.DB
	repo            *Repo
	idempotencyRepo *idempotency.Repo
	outboxRepo      *outbox.Repo
	popularity      *popularity.Service
	detailCache     *DetailCache
	mediaValidator  MediaValidator
}

// NewService creates a video service.
func NewService(db *gorm.DB, popularityService *popularity.Service, uploadDir string) *Service {
	return NewServiceWithDetailCacheAndPublisher(db, popularityService, nil, nil, uploadDir)
}

// NewServiceWithDetailCache creates a video service with detail cache support.
func NewServiceWithDetailCache(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, uploadDir string) *Service {
	return NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, nil, uploadDir)
}

// NewServiceWithDetailCacheAndPublisher creates a video service with cache support.
func NewServiceWithDetailCacheAndPublisher(db *gorm.DB, popularityService *popularity.Service, detailCache *DetailCache, _ *mq.Publisher, uploadDir string) *Service {
	return &Service{
		db:              db,
		repo:            NewRepo(db),
		idempotencyRepo: idempotency.NewRepo(db),
		outboxRepo:      outbox.NewRepo(db),
		popularity:      popularityService,
		detailCache:     detailCache,
		mediaValidator:  NewMediaValidator(uploadDir),
	}
}

// Publish creates a video exactly once for the same idempotency key and payload.
func (s *Service) Publish(accountID uint64, username string, idemKey string, req PublishRequest) (*Video, error) {
	normalizedIdemKey, err := normalizeIdempotencyKey(idemKey)
	if err != nil {
		return nil, err
	}

	playURL, coverURL, err := s.mediaValidator.NormalizePublishURLs(req.PlayURL, req.CoverURL)
	if err != nil {
		return nil, err
	}

	requestHash, err := buildPublishRequestHash(req.Title, req.Description, playURL, coverURL)
	if err != nil {
		return nil, fmt.Errorf("build publish request hash: %w", err)
	}

	var (
		video      *Video
		createdNew bool
	)

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		idemRow := &idempotency.Key{
			AccountID:   accountID,
			BizType:     videoPublishBizType,
			IdemKey:     normalizedIdemKey,
			RequestHash: requestHash,
			Status:      idempotency.StatusProcessing,
		}

		inserted, err := s.idempotencyRepo.CreateProcessing(tx, idemRow)
		if err != nil {
			return err
		}

		if !inserted {
			existing, err := s.idempotencyRepo.FindByScope(tx, accountID, videoPublishBizType, normalizedIdemKey)
			if err != nil {
				return err
			}
			if existing.RequestHash != requestHash {
				return ErrIdempotencyRequestConflict
			}

			video, err = s.replayPublishedVideo(existing)
			if err != nil {
				return err
			}
			if video == nil {
				return ErrIdempotencyRequestBusy
			}
			return nil
		}

		createdNew = true
		video = &Video{
			AuthorID:    accountID,
			Username:    username,
			Title:       req.Title,
			Description: req.Description,
			PlayURL:     playURL,
			CoverURL:    coverURL,
		}
		if s.popularity == nil {
			video.Popularity = int64(popularity.PublishWeight)
		}

		if err := s.repo.Create(tx, video); err != nil {
			return err
		}

		// 把“视频落库”和“待发 timeline 事件”放进同一个事务，避免只成功一半。
		event, err := mq.NewEnvelope(mq.EventTypeVideoTimelinePush, mq.ProducerAPIServer, mq.VideoTimelinePayload{
			VideoID:   video.ID,
			AuthorID:  video.AuthorID,
			CreatedAt: video.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("build timeline outbox event: %w", err)
		}
		if err := s.outboxRepo.Enqueue(tx, event); err != nil {
			return fmt.Errorf("enqueue timeline outbox event: %w", err)
		}

		responseVideo := *video
		if s.popularity != nil {
			responseVideo.Popularity = int64(popularity.PublishWeight)
			video.Popularity = responseVideo.Popularity
		}

		responseJSON, err := json.Marshal(responseVideo)
		if err != nil {
			return err
		}

		return s.idempotencyRepo.MarkDone(tx, idemRow.ID, video.ID, string(responseJSON))
	}); err != nil {
		return nil, err
	}

	if createdNew && s.popularity != nil {
		// 热度记录属于事务外的派生数据，失败不会影响视频发布主结果。
		_ = s.popularity.Record(context.Background(), video.ID, popularity.PublishWeight, video.CreatedAt)
		video.Popularity = int64(popularity.PublishWeight)
	}

	return video, nil
}

// ListByAuthorID returns videos by author and decorates popularity.
func (s *Service) ListByAuthorID(req ListByAuthorIDRequest) ([]Video, error) {
	videos, err := s.repo.FindByAuthorID(req.AuthorID)
	if err != nil {
		return nil, err
	}

	videos = s.mediaValidator.FilterPlayable(videos)
	return s.decoratePopularity(context.Background(), videos)
}

// ListLiked returns videos liked by the current account and decorates popularity.
func (s *Service) ListLiked(accountID uint64) ([]Video, error) {
	videos, err := s.repo.FindLikedByAccountID(accountID)
	if err != nil {
		return nil, err
	}

	videos = s.mediaValidator.FilterPlayable(videos)
	return s.decoratePopularity(context.Background(), videos)
}

// GetDetail returns one video and enriches popularity when Redis is available.
func (s *Service) GetDetail(req GetDetailRequest) (*Video, error) {
	if s.detailCache != nil {
		cachedVideo, ok, err := s.detailCache.Get(context.Background(), req.ID)
		if err != nil {
			log.Printf("video service: read detail cache failed for video %d: %v", req.ID, err)
		} else if ok {
			if s.mediaValidator.IsPlayable(*cachedVideo) {
				return cachedVideo, nil
			}

			if err := s.detailCache.Delete(context.Background(), req.ID); err != nil {
				log.Printf("video service: delete stale detail cache failed for video %d: %v", req.ID, err)
			}
		}
	}

	video, err := s.repo.FindByID(req.ID)
	if err != nil {
		return nil, err
	}
	if video == nil {
		return nil, ErrVideoNotFound
	}
	if !s.mediaValidator.IsPlayable(*video) {
		return nil, ErrVideoNotFound
	}

	if s.popularity != nil {
		scores, err := s.popularity.Scores(context.Background(), []uint64{video.ID}, time.Time{})
		if err == nil {
			video.Popularity = scores[video.ID]
		}
	}
	if s.detailCache != nil {
		if err := s.detailCache.Set(context.Background(), video); err != nil {
			log.Printf("video service: write detail cache failed for video %d: %v", video.ID, err)
		}
	}

	return video, nil
}

// decoratePopularity bulk loads popularity scores for a video list.
func (s *Service) decoratePopularity(ctx context.Context, videos []Video) ([]Video, error) {
	if s.popularity == nil || len(videos) == 0 {
		return videos, nil
	}

	videoIDs := make([]uint64, 0, len(videos))
	for _, item := range videos {
		videoIDs = append(videoIDs, item.ID)
	}

	scores, err := s.popularity.Scores(ctx, videoIDs, time.Time{})
	if err != nil {
		return nil, err
	}

	for i := range videos {
		videos[i].Popularity = scores[videos[i].ID]
	}

	return videos, nil
}

func (s *Service) replayPublishedVideo(row *idempotency.Key) (*Video, error) {
	if row == nil {
		return nil, nil
	}

	if row.ResponseJSON != "" {
		var item Video
		if err := json.Unmarshal([]byte(row.ResponseJSON), &item); err == nil && item.ID != 0 {
			return &item, nil
		}
	}

	if row.Status != idempotency.StatusDone || row.ResourceID == 0 {
		return nil, nil
	}

	video, err := s.repo.FindByID(row.ResourceID)
	if err != nil {
		return nil, err
	}
	if video == nil {
		return nil, nil
	}
	if s.popularity != nil && video.Popularity == 0 {
		video.Popularity = int64(popularity.PublishWeight)
	}

	return video, nil
}

func normalizeIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", ErrIdempotencyKeyRequired
	}
	if len(key) > maxIdempotencyKeyLength {
		return "", ErrIdempotencyKeyTooLong
	}
	return key, nil
}

func buildPublishRequestHash(title string, description string, playURL string, coverURL string) (string, error) {
	payload := struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		PlayURL     string `json:"play_url"`
		CoverURL    string `json:"cover_url"`
	}{
		Title:       title,
		Description: description,
		PlayURL:     playURL,
		CoverURL:    coverURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
