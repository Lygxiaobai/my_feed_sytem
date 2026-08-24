package video

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"my_feed_system/internal/audit"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/tag"
)

const (
	maxDraftsPerAuthor  = 20
	maxTitleRunes       = 128
	maxDescriptionRunes = 1000
)

// SaveDraft 把未点发布的成片收进草稿箱。
//
// 只在媒体已经可播时落库：上传中的分段没有 play_url，存进去只能得到一条打不开的草稿。
// 同一作者 + 同一 play_url 复用一行，避免每次离开页面都堆一条重复草稿。
func (s *Service) SaveDraft(accountID uint64, username string, req SaveDraftRequest) (*Video, error) {
	playURL, coverURL, err := s.mediaValidator.NormalizePublishURLs(req.PlayURL, req.CoverURL)
	if err != nil {
		return nil, err
	}

	title := clipRunes(req.Title, maxTitleRunes)
	description := clipRunes(req.Description, maxDescriptionRunes)
	tags := tag.ForPublish(req.Tags, title, description)

	var (
		existing *Video
		created  bool
	)
	if req.ID > 0 {
		existing, err = s.ownedMutable(accountID, req.ID)
		if err != nil {
			return nil, err
		}
		if !existing.IsDraft() {
			return nil, ErrNotDraft
		}
	} else {
		existing, err = s.repo.FindDraftByPlayURL(accountID, playURL)
		if err != nil {
			return nil, err
		}
	}

	if existing == nil {
		n, err := s.repo.CountDrafts(accountID)
		if err != nil {
			return nil, err
		}
		if n >= maxDraftsPerAuthor {
			return nil, ErrDraftLimitReached
		}
		created = true
		existing = &Video{
			AuthorID:    accountID,
			Username:    username,
			Popularity:  0,
			AuditStatus: audit.StatusPending,
			Lifecycle:   LifecycleDraft,
		}
	}

	existing.Username = username
	existing.Title = title
	existing.Description = description
	existing.Tags = tags
	existing.PlayURL = playURL
	existing.CoverURL = coverURL
	existing.Lifecycle = LifecycleDraft
	existing.AuditStatus = audit.StatusPending

	if created {
		if err := s.repo.Create(nil, existing); err != nil {
			return nil, err
		}
	} else {
		if err := s.repo.UpdateFields(nil, existing.ID, map[string]any{
			"username":     existing.Username,
			"title":        existing.Title,
			"description":  existing.Description,
			"tags":         existing.Tags,
			"play_url":     existing.PlayURL,
			"cover_url":    existing.CoverURL,
			"lifecycle":    LifecycleDraft,
			"audit_status": audit.StatusPending,
		}); err != nil {
			return nil, err
		}
	}

	s.invalidateDetailCache(existing.ID)
	return existing, nil
}

func (s *Service) ListDrafts(accountID uint64) ([]Video, error) {
	videos, err := s.repo.FindDraftsByAuthorID(accountID)
	if err != nil {
		return nil, err
	}
	return s.mediaValidator.FilterPlayable(videos), nil
}

// Unpublish 是作者自己把已发布内容从公开表面拿下来。
//
// 不改 AuditStatus：下架是作者意愿，不是审核拒绝。重新上架时仍沿用原来的审核结论。
func (s *Service) Unpublish(ctx context.Context, accountID uint64, req AuthorVideoRequest) (*Video, error) {
	item, err := s.ownedMutable(accountID, req.ID)
	if err != nil {
		return nil, err
	}
	if item.IsDraft() {
		return nil, ErrCannotUnpublish
	}
	if item.IsUnpublished() {
		return item, nil
	}
	if item.EffectiveLifecycle() != LifecyclePublished {
		return nil, ErrCannotUnpublish
	}

	if err := s.repo.UpdateFields(nil, item.ID, map[string]any{
		"lifecycle": LifecycleUnpublished,
	}); err != nil {
		return nil, err
	}
	item.Lifecycle = LifecycleUnpublished
	s.invalidateDetailCache(item.ID)

	slog.InfoContext(ctx, "video unpublished by author",
		slog.Uint64("video_id", item.ID),
		slog.Uint64("author_id", accountID))
	return item, nil
}

// Relist 把作者下架的内容重新上架。
//
// 已过审的要再走一遍公开化出口（时间线 / 热度），因为下架期间它已经离开公开表面。
// 待审或被拒的只恢复 lifecycle，不重新投递审核：作者没有改稿，结论仍然有效。
func (s *Service) Relist(ctx context.Context, accountID uint64, req AuthorVideoRequest) (*Video, error) {
	item, err := s.ownedMutable(accountID, req.ID)
	if err != nil {
		return nil, err
	}
	if item.IsDraft() {
		return nil, ErrCannotRelist
	}
	if item.EffectiveLifecycle() == LifecyclePublished {
		return item, nil
	}
	if !item.IsUnpublished() {
		return nil, ErrCannotRelist
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateFields(tx, item.ID, map[string]any{
			"lifecycle": LifecyclePublished,
		}); err != nil {
			return err
		}
		if item.AuditStatus.IsPublic() {
			return s.approval.EnqueueOnPublish(tx, item.ID, item.AuthorID)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	item.Lifecycle = LifecyclePublished
	s.invalidateDetailCache(item.ID)

	slog.InfoContext(ctx, "video relisted by author",
		slog.Uint64("video_id", item.ID),
		slog.Uint64("author_id", accountID))
	return item, nil
}

// Delete 软删除。媒体文件不在这里立刻清掉：删除必须可回看流水，
// 也给误操作留窗口；磁盘回收是另一条离线任务。
func (s *Service) Delete(ctx context.Context, accountID uint64, req AuthorVideoRequest) error {
	item, err := s.ownedMutable(accountID, req.ID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if err := s.repo.UpdateFields(nil, item.ID, map[string]any{
		"deleted_at": now,
	}); err != nil {
		return err
	}
	s.invalidateDetailCache(item.ID)
	s.setDetailNotFound(item.ID)

	slog.InfoContext(ctx, "video soft-deleted by author",
		slog.Uint64("video_id", item.ID),
		slog.Uint64("author_id", accountID),
		slog.String("lifecycle", string(item.EffectiveLifecycle())))
	return nil
}

func (s *Service) ownedMutable(accountID uint64, videoID uint64) (*Video, error) {
	item, err := s.repo.FindByID(videoID)
	if err != nil {
		return nil, err
	}
	// 非作者和已删除一律当不存在：区分「没权限」会泄漏这条内容确实在库里。
	if item == nil || item.IsDeleted() || item.AuthorID != accountID {
		return nil, ErrVideoNotFound
	}
	return item, nil
}

func (s *Service) findPromotableDraft(tx *gorm.DB, accountID uint64, draftID uint64, playURL string) (*Video, error) {
	if draftID > 0 {
		var item Video
		if err := tx.Where("id = ?", draftID).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrVideoNotFound
			}
			return nil, err
		}
		if item.IsDeleted() || item.AuthorID != accountID {
			return nil, ErrVideoNotFound
		}
		if !item.IsDraft() {
			return nil, ErrNotDraft
		}
		return &item, nil
	}

	var item Video
	err := scopeDrafts(tx.Where("author_id = ? AND play_url = ?", accountID, playURL)).
		Order("id DESC").
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (s *Service) promoteDraft(tx *gorm.DB, draft *Video, username, title, description string, tags tag.List, playURL, coverURL string) (*Video, error) {
	now := time.Now().UTC()
	fields := map[string]any{
		"username":     username,
		"title":        title,
		"description":  description,
		"tags":         tags,
		"play_url":     playURL,
		"cover_url":    coverURL,
		"lifecycle":    LifecyclePublished,
		"audit_status": s.initialAuditStatus(),
		"popularity":   int64(popularity.PublishWeight),
		"created_at":   now,
	}
	if err := s.repo.UpdateFields(tx, draft.ID, fields); err != nil {
		return nil, err
	}
	draft.Username = username
	draft.Title = title
	draft.Description = description
	draft.Tags = tags
	draft.PlayURL = playURL
	draft.CoverURL = coverURL
	draft.Lifecycle = LifecyclePublished
	draft.AuditStatus = s.initialAuditStatus()
	draft.Popularity = int64(popularity.PublishWeight)
	draft.CreatedAt = now
	return draft, nil
}

func clipRunes(raw string, max int) string {
	s := strings.TrimSpace(raw)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
