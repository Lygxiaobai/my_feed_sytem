package account

import (
	"errors"
	"strings"
)

const (
	DefaultListLimit = 20
	MaxListLimit     = 50
)

type ReviewListRequest struct {
	ID       uint64
	Username string
	Email    string
	Limit    int
	Offset   int
}

type ReviewSummary struct {
	Total int64
}

// ListForReview 只取公开字段，不读密码或 token。
func (s *Service) ListForReview(req ReviewListRequest) ([]Account, error) {
	limit, offset := clampReviewList(req.Limit, req.Offset)
	if email := strings.ToLower(strings.TrimSpace(req.Email)); email != "" {
		row, err := s.FindByIdentity(ProviderEmail, email)
		if errors.Is(err, ErrAccountNotFound) {
			return []Account{}, nil
		}
		if err != nil {
			return nil, err
		}
		return []Account{redactAccount(row)}, nil
	}

	q := s.repo.db.Model(&Account{}).
		Select("id, username, follower_count, interests, created_at").
		Order("id DESC")
	if req.ID > 0 {
		q = q.Where("id = ?", req.ID)
	}
	if name := strings.TrimSpace(req.Username); name != "" {
		q = q.Where("username LIKE ?", "%"+sanitizeLike(name)+"%")
	}
	var rows []Account
	err := q.Limit(limit).Offset(offset).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Password = ""
		rows[i].Token = ""
	}
	return rows, nil
}

func (s *Service) ReviewSummary() (ReviewSummary, error) {
	var out ReviewSummary
	err := s.repo.db.Model(&Account{}).Count(&out.Total).Error
	return out, err
}

// EmailsByIDs 批量取绑定邮箱，未绑定的账号不出现在 map 里。
func (s *Service) EmailsByIDs(ids []uint64) (map[uint64]string, error) {
	out := make(map[uint64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Identity
	err := s.repo.db.Select("account_id, subject").
		Where("provider = ? AND account_id IN ?", ProviderEmail, ids).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].AccountID] = rows[i].Subject
	}
	return out, nil
}

func redactAccount(row *Account) Account {
	if row == nil {
		return Account{}
	}
	return Account{
		ID:            row.ID,
		Username:      row.Username,
		FollowerCount: row.FollowerCount,
		Interests:     row.Interests,
		CreatedAt:     row.CreatedAt,
	}
}

func clampReviewList(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func sanitizeLike(raw string) string {
	s := strings.ReplaceAll(raw, `\`, "")
	s = strings.ReplaceAll(s, "%", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}
