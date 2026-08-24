package video

import (
	"errors"

	"gorm.io/gorm"
)

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) Create(tx *gorm.DB, video *Video) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	return db.Create(video).Error
}

// FindByAuthorID 返回作者的作品列表。
//
// viewerID 是当前查看者：作者本人能看到自己已发布/已下架/待审/被拒的作品，
// 看不到草稿和已删除（草稿走 FindDraftsByAuthorID）。其他人只看得到公开在架的。
func (r *Repo) FindByAuthorID(authorID uint64, viewerID uint64) ([]Video, error) {
	query := r.db.Where("author_id = ?", authorID)
	if viewerID != authorID {
		query = ScopePublic(query)
	} else {
		query = scopeAuthorOwned(query)
	}

	var videos []Video
	if err := query.Order("created_at DESC, id DESC").Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

func (r *Repo) FindDraftsByAuthorID(authorID uint64) ([]Video, error) {
	var videos []Video
	if err := scopeDrafts(r.db.Where("author_id = ?", authorID)).
		Order("updated_at DESC, id DESC").
		Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

func (r *Repo) CountDrafts(authorID uint64) (int64, error) {
	var n int64
	err := scopeDrafts(r.db.Model(&Video{}).Where("author_id = ?", authorID)).Count(&n).Error
	return n, err
}

func (r *Repo) FindDraftByPlayURL(authorID uint64, playURL string) (*Video, error) {
	var video Video
	err := scopeDrafts(r.db.Where("author_id = ? AND play_url = ?", authorID, playURL)).
		Order("id DESC").
		First(&video).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &video, nil
}

func (r *Repo) UpdateFields(tx *gorm.DB, id uint64, fields map[string]any) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	return db.Model(&Video{}).Where("id = ?", id).Updates(fields).Error
}

// FindByAuthorIDAll 返回作者的全部作品，不过滤审核状态。
// 只给管理面用：公开列表必须继续走 FindByAuthorID 的可见性规则。
func (r *Repo) FindByAuthorIDAll(authorID uint64, limit int) ([]Video, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var videos []Video
	if err := r.db.Where("author_id = ? AND deleted_at IS NULL", authorID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

// FindByID 按主键查询，不带审核过滤。
// 调用方负责判断查看者是否有权看到未过审内容。
func (r *Repo) FindByID(id uint64) (*Video, error) {
	var video Video
	if err := r.db.Where("id = ?", id).First(&video).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &video, nil
}

// FindLikedByAccountID 返回账号点赞过的视频。
// 只返回已过审的：否则点赞列表会成为绕过审核查看内容的旁路。
func (r *Repo) FindLikedByAccountID(accountID uint64) ([]Video, error) {
	var videos []Video
	if err := r.db.
		Table("videos").
		Select("videos.*").
		Joins("JOIN video_likes ON video_likes.video_id = videos.id").
		Where("video_likes.account_id = ?", accountID).
		Scopes(ScopePublic).
		Order("video_likes.created_at DESC, video_likes.id DESC").
		Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}

func (r *Repo) AdjustCounters(tx *gorm.DB, videoID uint64, likesDelta int64, commentDelta int64, popularityDelta int64) error {
	db := tx
	if db == nil {
		db = r.db
	}

	updates := map[string]any{}
	if likesDelta != 0 {
		updates["likes_count"] = gorm.Expr(
			"CASE WHEN likes_count + ? < 0 THEN 0 ELSE likes_count + ? END",
			likesDelta,
			likesDelta,
		)
	}
	if commentDelta != 0 {
		updates["comment_count"] = gorm.Expr(
			"CASE WHEN comment_count + ? < 0 THEN 0 ELSE comment_count + ? END",
			commentDelta,
			commentDelta,
		)
	}
	if popularityDelta != 0 {
		updates["popularity"] = gorm.Expr(
			"CASE WHEN popularity + ? < 0 THEN 0 ELSE popularity + ? END",
			popularityDelta,
			popularityDelta,
		)
	}
	if len(updates) == 0 {
		return nil
	}

	return db.Model(&Video{}).Where("id = ?", videoID).Updates(updates).Error
}
