package account

import (
	"errors"

	"gorm.io/gorm"

	"my_feed_system/internal/tag"
)

const MaxInterestTags = tag.Max

func ParseInterests(raw string) []string { return tag.Parse(raw) }

func EncodeInterests(tags []string) string { return tag.Encode(tags) }

func MergeInterestTags(existing, incoming []string) []string {
	return tag.Merge(existing, incoming)
}

// RecordFromVideo 把作品上已存的 tag 写入账号兴趣字段。没有 tag 就不改。
func (s *Service) RecordFromVideo(accountID uint64, tags []string) error {
	return s.AppendInterestTags(accountID, tags)
}

func (s *Service) AppendInterestTags(accountID uint64, incoming []string) error {
	if accountID == 0 || len(incoming) == 0 {
		return nil
	}
	return s.repo.db.Transaction(func(tx *gorm.DB) error {
		var row Account
		err := tx.Select("id, interests").Where("id = ?", accountID).First(&row).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		merged := tag.Merge(row.Interests, incoming)
		encoded := EncodeInterests(merged)
		if encoded == tag.Encode(row.Interests) {
			return nil
		}
		return tx.Model(&Account{}).Where("id = ?", accountID).Update("interests", encoded).Error
	})
}

func (s *Service) InterestsByIDs(ids []uint64) (map[uint64][]string, error) {
	out := make(map[uint64][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Account
	if err := s.repo.db.Select("id, interests").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		if tags := []string(rows[i].Interests); len(tags) > 0 {
			out[rows[i].ID] = tags
		}
	}
	return out, nil
}
