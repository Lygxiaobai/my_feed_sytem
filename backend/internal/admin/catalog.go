package admin

import (
	"strconv"
	"strings"

	"my_feed_system/internal/account"
	"my_feed_system/internal/audit"
	"my_feed_system/internal/video"
)

func (s *Service) ListVideos(operatorID uint64, req ListVideosRequest) (*VideoBoard, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	status, err := normalizeAuditStatus(req.AuditStatus)
	if err != nil {
		return nil, err
	}
	summary, err := s.videos.ReviewSummary()
	if err != nil {
		return nil, err
	}
	filter := parseVideoQuery(req.Query)
	filter.AuthorID = req.AuthorID
	filter.AuditStatus = status
	filter.Limit = req.Limit
	filter.Offset = req.Offset
	rows, err := s.videos.ListForReview(filter)
	if err != nil {
		return nil, err
	}
	items := make([]VideoView, 0, len(rows))
	for i := range rows {
		items = append(items, *videoView(&rows[i], 0))
	}
	return &VideoBoard{
		Summary: VideoBoardSummary{
			Total:     summary.Total,
			Pending:   summary.Pending,
			Reviewing: summary.Reviewing,
			Approved:  summary.Approved,
			Rejected:  summary.Rejected,
		},
		Videos:  items,
		HasMore: listHasMore(req.Limit, len(items)),
	}, nil
}

func (s *Service) ListAccounts(operatorID uint64, req ListAccountsRequest) (*AccountBoard, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	summary, err := s.accounts.ReviewSummary()
	if err != nil {
		return nil, err
	}
	filter, err := parseAccountQuery(req.Query)
	if err != nil {
		return nil, err
	}
	filter.Limit = req.Limit
	filter.Offset = req.Offset
	rows, err := s.accounts.ListForReview(filter)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	emails, err := s.accounts.EmailsByIDs(ids)
	if err != nil {
		return nil, err
	}
	items := make([]AccountView, 0, len(rows))
	for i := range rows {
		items = append(items, AccountView{
			ID:            rows[i].ID,
			Username:      rows[i].Username,
			Email:         emails[rows[i].ID],
			FollowerCount: rows[i].FollowerCount,
			CreatedAt:     rows[i].CreatedAt,
			InterestTags:  interestTagViews(rows[i].Interests),
		})
	}
	return &AccountBoard{
		Summary:  AccountBoardSummary{Total: summary.Total},
		Accounts: items,
		HasMore:  listHasMore(req.Limit, len(items)),
	}, nil
}

func interestTagViews(labels []string) []InterestTag {
	if len(labels) == 0 {
		return nil
	}
	out := make([]InterestTag, 0, len(labels))
	for _, label := range labels {
		out = append(out, InterestTag{Label: label})
	}
	return out
}

func parseVideoQuery(raw string) video.ReviewListRequest {
	text := strings.TrimSpace(raw)
	if text == "" {
		return video.ReviewListRequest{}
	}
	if id, ok := parsePositiveID(text); ok {
		return video.ReviewListRequest{VideoID: id}
	}
	return video.ReviewListRequest{Query: text}
}

func parseAccountQuery(raw string) (account.ReviewListRequest, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return account.ReviewListRequest{}, nil
	}
	if strings.Contains(text, "@") {
		return account.ReviewListRequest{Email: strings.ToLower(text)}, nil
	}
	if id, ok := parsePositiveID(text); ok {
		return account.ReviewListRequest{ID: id}, nil
	}
	return account.ReviewListRequest{Username: text}, nil
}

func parsePositiveID(raw string) (uint64, bool) {
	if raw == "" {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

func normalizeAuditStatus(raw string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	if status == "" {
		return "", nil
	}
	if !audit.Status(status).Valid() {
		return "", ErrInvalidAuditStatus
	}
	return status, nil
}
