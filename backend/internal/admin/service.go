package admin

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"my_feed_system/internal/account"
	"my_feed_system/internal/invoice"
	"my_feed_system/internal/recommend"
	"my_feed_system/internal/report"
	"my_feed_system/internal/video"
	"my_feed_system/internal/wallet"
)

var (
	ErrNotReviewer        = errors.New("account is not a reviewer")
	ErrNoteRequired       = errors.New("takedown note is required")
	ErrNoteTooLong        = errors.New("takedown note is too long")
	ErrLookupMissing      = errors.New("lookup identifier is required")
	ErrLookupAmbiguous    = errors.New("only one lookup identifier is allowed")
	ErrInvalidOrderStatus = errors.New("invalid order status")
	ErrInvalidAuditStatus = errors.New("invalid audit status")
)

// Service 是管理后台的编排层。
//
// 不另立权限体系：能不能进后台，完全复用审核员白名单。
// 也不复制处置逻辑：下架走 video，结案走 report，这里只负责把它们按
// 「先改内容、再关工单」的顺序串起来。
type Service struct {
	reports   *report.Service
	videos    *video.Service
	accounts  *account.Service
	invoices  *invoice.Service
	wallets   *wallet.Service
	interests *recommend.Service
}

func NewService(reports *report.Service, videos *video.Service, accounts *account.Service, invoices *invoice.Service, wallets *wallet.Service) *Service {
	return &Service{reports: reports, videos: videos, accounts: accounts, invoices: invoices, wallets: wallets}
}

func (s *Service) SetInterests(interests *recommend.Service) {
	s.interests = interests
}

func (s *Service) IsReviewer(accountID uint64) bool {
	return s.reports.IsReviewer(accountID)
}

func (s *Service) Access(accountID uint64) AccessResult {
	return AccessResult{Allowed: s.IsReviewer(accountID)}
}

func (s *Service) Overview(accountID uint64, username string) (*Overview, error) {
	if !s.IsReviewer(accountID) {
		return nil, ErrNotReviewer
	}
	pending, err := s.reports.CountPending(accountID)
	if err != nil {
		return nil, err
	}
	invoiceSum, err := s.invoices.ReviewSummary()
	if err != nil {
		return nil, err
	}
	paySum, err := s.wallets.OrderReviewSummary()
	if err != nil {
		return nil, err
	}
	balanceSum, err := s.wallets.BalanceReviewSummary()
	if err != nil {
		return nil, err
	}
	videoSum, err := s.videos.ReviewSummary()
	if err != nil {
		return nil, err
	}
	accountSum, err := s.accounts.ReviewSummary()
	if err != nil {
		return nil, err
	}
	return &Overview{
		PendingReports: pending,
		AccountID:      accountID,
		Username:       username,
		IssuedInvoices: invoiceSum.IssuedCount,
		PaidYuan:       paySum.PaidYuan,
		PaidOrders:     paySum.PaidCount,
		PendingOrders:  paySum.PendingCount,
		AvailableCoins: balanceSum.AvailableCoins,
		VideoCount:     videoSum.Total,
		AccountCount:   accountSum.Total,
	}, nil
}

func (s *Service) LookupVideo(operatorID uint64, req LookupVideoRequest) (*VideoView, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	item, err := s.videos.LookupForReview(req.VideoID)
	if err != nil {
		return nil, err
	}
	pending, err := s.reports.CountPendingByTarget(operatorID, item.ID)
	if err != nil {
		return nil, err
	}
	return videoView(item, pending), nil
}

func (s *Service) Takedown(ctx context.Context, operatorID uint64, req TakedownRequest) error {
	if !s.IsReviewer(operatorID) {
		return ErrNotReviewer
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		return ErrNoteRequired
	}
	if utf8.RuneCountInString(note) > NoteMaxLength {
		return ErrNoteTooLong
	}

	// 先下架再结案：反过来会把工单从队列拿掉，内容却还在线上。
	if err := s.videos.AdminTakedown(ctx, req.VideoID, operatorID, note); err != nil {
		return err
	}
	closed, err := s.reports.ClosePendingAsAccepted(operatorID, req.VideoID, note)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "admin video taken down",
		slog.Uint64("video_id", req.VideoID),
		slog.Uint64("operator_id", operatorID),
		slog.Int64("closed_reports", closed))
	return nil
}

func (s *Service) LookupAccount(operatorID uint64, req LookupAccountRequest) (*AccountView, []VideoView, error) {
	if !s.IsReviewer(operatorID) {
		return nil, nil, ErrNotReviewer
	}

	kindCount := 0
	if req.ID > 0 {
		kindCount++
	}
	if strings.TrimSpace(req.Username) != "" {
		kindCount++
	}
	if strings.TrimSpace(req.Email) != "" {
		kindCount++
	}
	if kindCount == 0 {
		return nil, nil, ErrLookupMissing
	}
	if kindCount > 1 {
		return nil, nil, ErrLookupAmbiguous
	}

	var (
		row *account.Account
		err error
	)
	switch {
	case req.ID > 0:
		row, err = s.accounts.FindByID(account.FindByIDRequest{ID: req.ID})
	case strings.TrimSpace(req.Username) != "":
		row, err = s.accounts.FindByUsername(account.FindByUsernameRequest{Username: strings.TrimSpace(req.Username)})
	default:
		row, err = s.accounts.FindByIdentity(account.ProviderEmail, strings.ToLower(strings.TrimSpace(req.Email)))
	}
	if err != nil {
		return nil, nil, err
	}

	email, err := s.accounts.FindEmailSubject(row.ID)
	if err != nil {
		return nil, nil, err
	}

	videos, err := s.videos.ListByAuthorForReview(row.ID)
	if err != nil {
		return nil, nil, err
	}

	items := make([]VideoView, 0, len(videos))
	for i := range videos {
		items = append(items, *videoView(&videos[i], 0))
	}

	return &AccountView{
		ID:            row.ID,
		Username:      row.Username,
		Email:         email,
		FollowerCount: row.FollowerCount,
		CreatedAt:     row.CreatedAt,
		InterestTags:  interestTagViews(row.Interests),
	}, items, nil
}

func videoView(item *video.Video, pending int64) *VideoView {
	return &VideoView{
		ID:             item.ID,
		AuthorID:       item.AuthorID,
		Username:       item.Username,
		Title:          item.Title,
		Description:    item.Description,
		Tags:           []string(item.Tags),
		PlayURL:        item.PlayURL,
		CoverURL:       item.CoverURL,
		LikesCount:     item.LikesCount,
		CommentCount:   item.CommentCount,
		AuditStatus:    string(item.AuditStatus),
		CreatedAt:      item.CreatedAt,
		PendingReports: pending,
	}
}
