package like

import (
	"errors"

	"gorm.io/gorm"
)

// Repo 负责点赞记录表的查询操作。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建点赞仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// FindByVideoAndAccount 查询某个用户对某个视频的点赞记录。
func (r *Repo) FindByVideoAndAccount(videoID uint64, accountID uint64) (*VideoLike, error) {
	var record VideoLike
	if err := r.db.Where("video_id = ? AND account_id = ?", videoID, accountID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &record, nil
}

// FindLikedVideoIDs 批量查询某个用户已点赞的视频 ID。
func (r *Repo) FindLikedVideoIDs(accountID uint64, videoIDs []uint64) ([]uint64, error) {
	var ids []uint64
	if err := r.db.
		Model(&VideoLike{}).
		Where("account_id = ? AND video_id IN ?", accountID, videoIDs).
		Pluck("video_id", &ids).Error; err != nil {
		return nil, err
	}

	return ids, nil
}
