package social

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/account"
	"my_feed_system/internal/mq"
)

var (
	ErrAlreadyFollowed  = errors.New("already followed this vlogger")
	ErrFollowNotFound   = errors.New("follow relation not found")
	ErrCannotFollowSelf = errors.New("cannot follow yourself")
)

type Service struct {
	repo        *Repo
	accountRepo *account.Repo
	publisher   *mq.Publisher
}

func NewService(db *gorm.DB) *Service {
	return NewServiceWithPublisher(db, nil)
}

func NewServiceWithPublisher(db *gorm.DB, publisher *mq.Publisher) *Service {
	return &Service{
		repo:        NewRepo(db),
		accountRepo: account.NewRepo(db),
		publisher:   publisher,
	}
}

func (s *Service) Follow(followerID uint64, req FollowRequest) error {
	if followerID == req.VloggerID {
		return ErrCannotFollowSelf
	}

	vlogger, err := s.accountRepo.FindByID(req.VloggerID)
	if err != nil {
		return err
	}
	if vlogger == nil {
		return account.ErrAccountNotFound
	}

	existing, err := s.repo.FindByPair(followerID, req.VloggerID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrAlreadyFollowed
	}

	// 兜底：未接入 MQ 时沿用同步写逻辑。
	if s.publisher == nil {
		return s.repo.Create(&SocialRelation{FollowerID: followerID, VloggerID: req.VloggerID})
	}

	// 异步路径：发布关注事件后快速返回，由 Worker 落库。
	event, err := mq.NewEnvelope(mq.EventTypeSocialFollowed, mq.ProducerAPIServer, mq.SocialPayload{
		FollowerID: followerID,
		VloggerID:  req.VloggerID,
	})
	if err != nil {
		return fmt.Errorf("build social.followed event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.publisher.Publish(ctx, event)
}

func (s *Service) Unfollow(followerID uint64, req FollowRequest) error {
	existing, err := s.repo.FindByPair(followerID, req.VloggerID)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrFollowNotFound
	}

	// 兜底：未接入 MQ 时沿用同步写逻辑。
	if s.publisher == nil {
		rowsAffected, err := s.repo.DeleteByPair(followerID, req.VloggerID)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return ErrFollowNotFound
		}
		return nil
	}

	// 异步路径：发布取关事件后快速返回，由 Worker 落库。
	event, err := mq.NewEnvelope(mq.EventTypeSocialUnfollowed, mq.ProducerAPIServer, mq.SocialPayload{
		FollowerID: followerID,
		VloggerID:  req.VloggerID,
	})
	if err != nil {
		return fmt.Errorf("build social.unfollowed event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.publisher.Publish(ctx, event)
}

func (s *Service) GetAllFollowers(vloggerID uint64) ([]SocialRelation, error) {
	return s.repo.FindAllFollowers(vloggerID)
}

func (s *Service) GetAllVloggers(followerID uint64) ([]SocialRelation, error) {
	return s.repo.FindAllVloggers(followerID)
}
