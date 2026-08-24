package video

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/audit"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
)

// AuditStore 让 video 模块满足 audit.StatusStore 接口。
//
// 单独成文件是为了让「审核相关的仓储能力」聚在一处：
// audit 包只认这个窄接口，不会反向依赖 video，避免包循环。
type AuditStore struct {
	repo *Repo
	db   *gorm.DB
}

func NewAuditStore(db *gorm.DB) *AuditStore {
	return &AuditStore{repo: NewRepo(db), db: db}
}

// LoadForAudit 读取待审视频的字段快照。视频不存在时返回 nil。
func (s *AuditStore) LoadForAudit(targetID uint64) (*audit.Target, error) {
	video, err := s.repo.FindByID(targetID)
	if err != nil {
		return nil, err
	}
	if video == nil {
		return nil, nil
	}
	return toAuditTarget(*video), nil
}

// UpdateStatus 在事务内更新审核状态。
func (s *AuditStore) UpdateStatus(tx *gorm.DB, targetID uint64, status audit.Status) error {
	db := tx
	if db == nil {
		db = s.db
	}
	return db.Model(&Video{}).
		Where("id = ?", targetID).
		Update("audit_status", status).Error
}

// ListByStatus 按状态列出视频，供人工复审队列使用。
// 用 id 游标分页而非 offset，避免审核过程中列表变动导致漏审或重复。
func (s *AuditStore) ListByStatus(status audit.Status, limit int, offsetID uint64) ([]audit.Target, error) {
	query := scopeReviewable(s.db.Model(&Video{}).Where("audit_status = ?", status))
	if offsetID > 0 {
		query = query.Where("id > ?", offsetID)
	}

	var videos []Video
	if err := query.Order("id ASC").Limit(limit).Find(&videos).Error; err != nil {
		return nil, err
	}

	targets := make([]audit.Target, 0, len(videos))
	for _, v := range videos {
		targets = append(targets, *toAuditTarget(v))
	}
	return targets, nil
}

func toAuditTarget(v Video) *audit.Target {
	return &audit.Target{
		ID:          v.ID,
		AuthorID:    v.AuthorID,
		Username:    v.Username,
		Title:       v.Title,
		Description: v.Description,
		PlayURL:     v.PlayURL,
		CoverURL:    v.CoverURL,
		Status:      v.AuditStatus,
	}
}

// ApprovalPublisher 实现 audit.ApprovalPublisher，在内容通过审核后
// 把「让内容公开可见」的两件事投递出去。
//
// 走 outbox 而不是直接发消息：事件与审核状态在同一事务里提交，
// 不会出现状态已通过但事件丢失的情况。机审与人工复审共用这条出口，
// 因此两条路径的行为天然一致。
type ApprovalPublisher struct {
	outboxRepo *outbox.Repo
}

func NewApprovalPublisher(db *gorm.DB) *ApprovalPublisher {
	return &ApprovalPublisher{outboxRepo: outbox.NewRepo(db)}
}

func (p *ApprovalPublisher) EnqueueApproved(tx *gorm.DB, videoID uint64, authorID uint64) error {
	return p.enqueuePublic(tx, videoID, authorID, "audit.approved")
}

// EnqueueOnPublish 在审核关闭时于发布事务内直接公开内容。
func (p *ApprovalPublisher) EnqueueOnPublish(tx *gorm.DB, videoID uint64, authorID uint64) error {
	return p.enqueuePublic(tx, videoID, authorID, "video.published")
}

func (p *ApprovalPublisher) enqueuePublic(tx *gorm.DB, videoID uint64, authorID uint64, reason string) error {
	// 推入全局时间线，内容自此出现在最新流中。
	timelineEvent, err := mq.NewEnvelope(mq.EventTypeVideoTimelinePush, mq.ProducerAPIServer, mq.VideoTimelinePayload{
		VideoID:   videoID,
		AuthorID:  authorID,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("build timeline event after %s: %w", reason, err)
	}
	if err := p.outboxRepo.Enqueue(tx, timelineEvent); err != nil {
		return fmt.Errorf("enqueue timeline event after %s: %w", reason, err)
	}

	// 计入热度榜。未过审内容不参与热度竞争，因此只在公开这一刻写入。
	popularityEvent, err := mq.NewEnvelope(mq.EventTypePopularityChanged, mq.ProducerAPIServer, mq.PopularityChangedPayload{
		VideoID: videoID,
		Delta:   int64(popularity.PublishWeight),
		Reason:  reason,
	})
	if err != nil {
		return fmt.Errorf("build popularity event after %s: %w", reason, err)
	}
	if err := p.outboxRepo.Enqueue(tx, popularityEvent); err != nil {
		return fmt.Errorf("enqueue popularity event after %s: %w", reason, err)
	}

	// 标题/描述向量与时间线、热度同一出口投递，机审和人工共用。
	embedEvent, err := mq.NewEnvelope(mq.EventTypeVideoEmbedRequested, mq.ProducerAPIServer, mq.VideoEmbedPayload{
		VideoID: videoID,
	})
	if err != nil {
		return fmt.Errorf("build embed event after %s: %w", reason, err)
	}
	if err := p.outboxRepo.Enqueue(tx, embedEvent); err != nil {
		return fmt.Errorf("enqueue embed event after %s: %w", reason, err)
	}

	return nil
}
