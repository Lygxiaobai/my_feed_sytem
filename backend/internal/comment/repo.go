package comment

import (
	"errors"

	"gorm.io/gorm"
)

// Repo 负责视频评论表的数据库读写。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建评论仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// Create 插入一条新的评论记录。
func (r *Repo) Create(comment *VideoComment) error {
	return r.db.Create(comment).Error
}

// FindRootCommentsByVideoID 查询某个视频下的根评论列表。
func (r *Repo) FindRootCommentsByVideoID(videoID uint64) ([]VideoComment, error) {
	var comments []VideoComment
	if err := r.db.Where("video_id = ? AND root_comment_id = 0", videoID).Order("created_at ASC, id ASC").Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

// FindRepliesByRootCommentIDs 查询一组根评论下挂载的回复列表。
func (r *Repo) FindRepliesByRootCommentIDs(videoID uint64, rootCommentIDs []uint64) ([]VideoComment, error) {
	if len(rootCommentIDs) == 0 {
		return []VideoComment{}, nil
	}

	var comments []VideoComment
	if err := r.db.Where("video_id = ? AND root_comment_id IN ?", videoID, rootCommentIDs).Order("created_at ASC, id ASC").Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

// FindByID 按主键查询评论，未命中时返回 nil。
func (r *Repo) FindByID(id uint64) (*VideoComment, error) {
	var comment VideoComment
	if err := r.db.Where("id = ?", id).First(&comment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &comment, nil
}

// DeleteByIDOrRootID 删除指定评论，以及它作为根评论时挂载的整棵回复树。
func (r *Repo) DeleteByIDOrRootID(tx *gorm.DB, id uint64) (int64, error) {
	db := tx
	if db == nil {
		db = r.db
	}

	result := db.Where("id = ? OR root_comment_id = ?", id, id).Delete(&VideoComment{})
	return result.RowsAffected, result.Error
}
