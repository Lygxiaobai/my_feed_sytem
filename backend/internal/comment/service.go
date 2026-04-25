package comment

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

var (
	ErrCommentNotFound       = errors.New("comment not found")
	ErrCommentForbidden      = errors.New("only comment author or video author can delete this comment")
	ErrInvalidParentComment  = errors.New("parent comment not found")
	ErrParentCommentMismatch = errors.New("parent comment does not belong to this video")
)

var commentIDSequence atomic.Uint64

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

func (s *Service) ListAll(req ListAllRequest) ([]CommentItem, error) {
	currentVideo, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return nil, err
	}
	if currentVideo == nil {
		return nil, video.ErrVideoNotFound
	}

	roots, err := s.repo.FindRootCommentsByVideoID(req.VideoID)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return []CommentItem{}, nil
	}

	rootIDs := make([]uint64, 0, len(roots))
	for _, root := range roots {
		rootIDs = append(rootIDs, root.ID)
	}

	replies, err := s.repo.FindRepliesByRootCommentIDs(req.VideoID, rootIDs)
	if err != nil {
		return nil, err
	}

	return buildCommentItems(roots, replies), nil
}

func (s *Service) Publish(accountID uint64, username string, req PublishRequest) (*VideoComment, error) {
	currentVideo, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return nil, err
	}
	if currentVideo == nil {
		return nil, video.ErrVideoNotFound
	}

	comment := &VideoComment{
		VideoID:  req.VideoID,
		AuthorID: accountID,
		Username: username,
		Content:  req.Content,
	}

	if req.ParentCommentID > 0 {
		parentComment, err := s.repo.FindByID(req.ParentCommentID)
		if err != nil {
			return nil, err
		}
		if parentComment == nil {
			return nil, ErrInvalidParentComment
		}
		if parentComment.VideoID != req.VideoID {
			return nil, ErrParentCommentMismatch
		}

		comment.ParentCommentID = parentComment.ID
		comment.ReplyToUserID = parentComment.AuthorID
		comment.ReplyToUsername = parentComment.Username
		if parentComment.RootCommentID == 0 {
			comment.RootCommentID = parentComment.ID
		} else {
			comment.RootCommentID = parentComment.RootCommentID
		}
	}

	// 兜底：未接入 MQ 时沿用同步写逻辑。
	if s.publisher == nil {
		return s.publishSync(comment)
	}

	// 先分配 comment_id，再发事件，保证客户端能立即拿到稳定 ID。
	comment.ID = nextCommentID()
	comment.CreatedAt = time.Now().UTC()
	comment.UpdatedAt = comment.CreatedAt

	// 异步路径：发布写事件后快速返回，由 Worker 落库。
	event, err := mq.NewEnvelope(mq.EventTypeCommentCreated, mq.ProducerAPIServer, mq.CommentCreatedPayload{
		CommentID:       comment.ID,
		VideoID:         comment.VideoID,
		AuthorID:        comment.AuthorID,
		Username:        comment.Username,
		Content:         comment.Content,
		ParentCommentID: comment.ParentCommentID,
		RootCommentID:   comment.RootCommentID,
		ReplyToUserID:   comment.ReplyToUserID,
		ReplyToUsername: comment.ReplyToUsername,
	})
	if err != nil {
		return nil, fmt.Errorf("build comment.created event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.publisher.Publish(ctx, event); err != nil {
		return nil, err
	}

	return comment, nil
}

func (s *Service) Delete(accountID uint64, req DeleteRequest) error {
	comment, err := s.repo.FindByID(req.CommentID)
	if err != nil {
		return err
	}
	if comment == nil {
		return ErrCommentNotFound
	}

	currentVideo, err := s.videoRepo.FindByID(comment.VideoID)
	if err != nil {
		return err
	}
	if currentVideo == nil {
		return video.ErrVideoNotFound
	}
	if comment.AuthorID != accountID && currentVideo.AuthorID != accountID {
		return ErrCommentForbidden
	}

	// 兜底：未接入 MQ 时沿用同步写逻辑。
	if s.publisher == nil {
		return s.deleteSync(accountID, req, comment.VideoID)
	}

	// 异步路径：发布删除事件后快速返回，由 Worker 执行删除。
	event, err := mq.NewEnvelope(mq.EventTypeCommentDeleted, mq.ProducerAPIServer, mq.CommentDeletedPayload{
		CommentID:  req.CommentID,
		VideoID:    comment.VideoID,
		OperatorID: accountID,
	})
	if err != nil {
		return fmt.Errorf("build comment.deleted event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.publisher.Publish(ctx, event); err != nil {
		return err
	}

	return nil
}

func (s *Service) publishSync(comment *VideoComment) (*VideoComment, error) {
	popularityDelta := int64(0)
	if s.popularity == nil {
		popularityDelta = int64(popularity.CommentPublishWeight)
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(comment).Error; err != nil {
			return err
		}

		return s.videoRepo.AdjustCounters(tx, comment.VideoID, 0, 1, popularityDelta)
	}); err != nil {
		return nil, err
	}

	if s.popularity != nil {
		_ = s.popularity.Record(context.Background(), comment.VideoID, popularity.CommentPublishWeight, time.Now())
	}
	s.invalidateDetailCache(comment.VideoID)

	return comment, nil
}

func (s *Service) deleteSync(_ uint64, req DeleteRequest, videoID uint64) error {
	deletedCount := int64(0)
	popularityDelta := int64(0)

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		deletedCount, err = s.repo.DeleteByIDOrRootID(tx, req.CommentID)
		if err != nil {
			return err
		}
		if deletedCount == 0 {
			return ErrCommentNotFound
		}

		if s.popularity == nil {
			popularityDelta = int64(popularity.CommentDeleteWeight) * deletedCount
		}

		return s.videoRepo.AdjustCounters(tx, videoID, 0, -deletedCount, popularityDelta)
	}); err != nil {
		return err
	}

	if s.popularity != nil {
		for i := int64(0); i < deletedCount; i++ {
			_ = s.popularity.Record(context.Background(), videoID, popularity.CommentDeleteWeight, time.Now())
		}
	}
	s.invalidateDetailCache(videoID)

	return nil
}

func nextCommentID() uint64 {
	// 时间戳 + 本地序列，降低高并发同毫秒下的碰撞概率。
	return uint64(time.Now().UnixNano()) + commentIDSequence.Add(1)
}

func (s *Service) invalidateDetailCache(videoID uint64) {
	if s.detailCache == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.detailCache.Delete(ctx, videoID); err != nil {
		log.Printf("comment service: delete detail cache failed for video %d: %v", videoID, err)
	}
}
