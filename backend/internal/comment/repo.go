package comment

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

func (r *Repo) Create(comment *VideoComment) error {
	return r.db.Create(comment).Error
}

func (r *Repo) FindRootCommentsByVideoID(videoID uint64) ([]VideoComment, error) {
	var comments []VideoComment
	if err := r.db.Where("video_id = ? AND root_comment_id = 0", videoID).Order("created_at ASC, id ASC").Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

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

func (r *Repo) FindByResolvableID(id uint64) (*VideoComment, error) {
	return r.findByResolvableID(r.db, id)
}

func (r *Repo) FindByVideoIDAndResolvableID(videoID uint64, id uint64) (*VideoComment, error) {
	return r.findByResolvableID(r.db.Where("video_id = ?", videoID), id)
}

func (r *Repo) findByResolvableID(db *gorm.DB, id uint64) (*VideoComment, error) {
	base := db.Session(&gorm.Session{})

	var comment VideoComment
	if err := base.Where("id = ?", id).First(&comment).Error; err == nil {
		return &comment, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if id <= maxJSSafeInteger {
		return nil, nil
	}

	lower, upper := commentIDLookupRange(id)
	var candidates []VideoComment
	if err := base.Where("id BETWEEN ? AND ?", lower, upper).Order("id ASC").Find(&candidates).Error; err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	best := candidates[0]
	bestDistance := uint64Distance(best.ID, id)
	for _, candidate := range candidates[1:] {
		distance := uint64Distance(candidate.ID, id)
		if distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}

	return &best, nil
}

func (r *Repo) DeleteByIDOrRootID(tx *gorm.DB, id uint64) (int64, error) {
	db := tx
	if db == nil {
		db = r.db
	}

	result := db.Where("id = ? OR root_comment_id = ?", id, id).Delete(&VideoComment{})
	return result.RowsAffected, result.Error
}
