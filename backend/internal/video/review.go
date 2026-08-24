package video

import (
	"strings"

	"my_feed_system/internal/audit"
)

const (
	DefaultListLimit = 20
	MaxListLimit     = 50
)

type ReviewListRequest struct {
	VideoID     uint64
	AuthorID    uint64
	Username    string
	Title       string
	Query       string
	AuditStatus string
	Limit       int
	Offset      int
}

type ReviewSummary struct {
	Total     int64
	Pending   int64
	Reviewing int64
	Approved  int64
	Rejected  int64
}

// ListForReview 列出任意审核状态的作品。不走公开详情，也不写公开缓存。
func (s *Service) ListForReview(req ReviewListRequest) ([]Video, error) {
	limit, offset := clampReviewList(req.Limit, req.Offset)
	q := s.db.Model(&Video{}).Order("id DESC")
	if req.VideoID > 0 {
		q = q.Where("id = ?", req.VideoID)
	} else {
		q = scopeReviewable(q)
	}
	if req.AuthorID > 0 {
		q = q.Where("author_id = ?", req.AuthorID)
	}
	if name := strings.TrimSpace(req.Username); name != "" {
		q = q.Where("username LIKE ?", likeContains(name))
	}
	if query := strings.TrimSpace(req.Query); query != "" {
		like := likeContains(query)
		q = q.Where("(title LIKE ? OR username LIKE ? OR tags LIKE ?)", like, like, like)
	}
	if title := strings.TrimSpace(req.Title); title != "" {
		q = q.Where("title LIKE ?", likeContains(title))
	}
	if status := strings.TrimSpace(req.AuditStatus); status != "" {
		q = q.Where("audit_status = ?", status)
	}
	var rows []Video
	err := q.Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}

func (s *Service) ReviewSummary() (ReviewSummary, error) {
	var out ReviewSummary
	if err := scopeReviewable(s.db.Model(&Video{})).Count(&out.Total).Error; err != nil {
		return ReviewSummary{}, err
	}
	type agg struct {
		AuditStatus string
		Count       int64
	}
	var rows []agg
	if err := scopeReviewable(s.db.Model(&Video{})).Select("audit_status, COUNT(*) AS count").Group("audit_status").Scan(&rows).Error; err != nil {
		return ReviewSummary{}, err
	}
	for _, row := range rows {
		switch audit.Status(row.AuditStatus) {
		case audit.StatusPending:
			out.Pending = row.Count
		case audit.StatusReviewing:
			out.Reviewing = row.Count
		case audit.StatusApproved:
			out.Approved = row.Count
		case audit.StatusRejected:
			out.Rejected = row.Count
		}
	}
	return out, nil
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

func likeContains(raw string) string {
	return "%" + sanitizeLike(raw) + "%"
}

func sanitizeLike(raw string) string {
	s := strings.ReplaceAll(raw, `\`, "")
	s = strings.ReplaceAll(s, "%", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}
