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

func (r *Repo) FindByAuthorID(authorID uint64) ([]Video, error) {
	var videos []Video
	if err := r.db.Where("author_id = ?", authorID).Order("created_at DESC, id DESC").Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

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

func (r *Repo) FindLikedByAccountID(accountID uint64) ([]Video, error) {
	var videos []Video
	if err := r.db.
		Table("videos").
		Select("videos.*").
		Joins("JOIN video_likes ON video_likes.video_id = videos.id").
		Where("video_likes.account_id = ?", accountID).
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
