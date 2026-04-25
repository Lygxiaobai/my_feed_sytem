package social

import (
	"errors"

	"gorm.io/gorm"
)

// Repo 负责关注关系表的查询和写入。
type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) FindByPair(followerID uint64, vloggerID uint64) (*SocialRelation, error) {
	var relation SocialRelation
	if err := r.db.Where("follower_id = ? AND vlogger_id = ?", followerID, vloggerID).First(&relation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &relation, nil
}

func (r *Repo) Create(relation *SocialRelation) error {
	return r.db.Create(relation).Error
}

func (r *Repo) DeleteByPair(followerID uint64, vloggerID uint64) (int64, error) {
	result := r.db.Where("follower_id = ? AND vlogger_id = ?", followerID, vloggerID).Delete(&SocialRelation{})
	return result.RowsAffected, result.Error
}

func (r *Repo) FindAllFollowers(vloggerID uint64) ([]SocialRelation, error) {
	var relations []SocialRelation
	if err := r.db.
		Table("social_relations").
		Select("social_relations.*, follower.username AS follower_username").
		Joins("LEFT JOIN accounts AS follower ON follower.id = social_relations.follower_id").
		Where("social_relations.vlogger_id = ?", vloggerID).
		Order("social_relations.created_at DESC, social_relations.id DESC").
		Find(&relations).Error; err != nil {
		return nil, err
	}
	return relations, nil
}

func (r *Repo) FindAllVloggers(followerID uint64) ([]SocialRelation, error) {
	var relations []SocialRelation
	if err := r.db.
		Table("social_relations").
		Select("social_relations.*, vlogger.username AS vlogger_username").
		Joins("LEFT JOIN accounts AS vlogger ON vlogger.id = social_relations.vlogger_id").
		Where("social_relations.follower_id = ?", followerID).
		Order("social_relations.created_at DESC, social_relations.id DESC").
		Find(&relations).Error; err != nil {
		return nil, err
	}
	return relations, nil
}
