package invoice

import "strings"

type ReviewListRequest struct {
	InvoiceNo  string
	OutTradeNo string
	AccountID  uint64
	Limit      int
	Offset     int
}

type ReviewSummary struct {
	IssuedCount int64
	YuanTotal   int64
	CoinsTotal  int64
}

// ListForReview 列出任意账号的发票。门禁由管理面检查，这里只做只读过滤。
func (s *Service) ListForReview(req ReviewListRequest) ([]Invoice, error) {
	limit, offset := clampList(req.Limit, req.Offset)
	q := s.db.Model(&Invoice{}).Order("id DESC")
	if no := strings.TrimSpace(req.InvoiceNo); no != "" {
		q = q.Where("invoice_no = ?", no)
	}
	if trade := strings.TrimSpace(req.OutTradeNo); trade != "" {
		q = q.Where("out_trade_no = ?", trade)
	}
	if req.AccountID > 0 {
		q = q.Where("account_id = ?", req.AccountID)
	}
	var rows []Invoice
	err := q.Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}

// GetForReview 按发票号读取任意一张，不校验申请人。
func (s *Service) GetForReview(invoiceNo string) (*Invoice, error) {
	return s.lookup(strings.TrimSpace(invoiceNo))
}

func (s *Service) ReviewSummary() (ReviewSummary, error) {
	var row ReviewSummary
	err := s.db.Model(&Invoice{}).
		Select("COUNT(*) AS issued_count, COALESCE(SUM(yuan),0) AS yuan_total, COALESCE(SUM(coins + bonus),0) AS coins_total").
		Where("status = ?", StatusIssued).
		Scan(&row).Error
	return row, err
}
