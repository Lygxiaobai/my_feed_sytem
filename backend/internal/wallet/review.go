package wallet

import (
	"strings"
	"time"
)

type OrderReviewListRequest struct {
	Status     string
	OutTradeNo string
	AccountID  uint64
	Limit      int
	Offset     int
}

type OrderReviewSummary struct {
	PaidCount        int64
	PaidYuan         int64
	PendingCount     int64
	ClosedCount      int64
	PlatformCutCoins int64
}

type BalanceRow struct {
	AccountID         uint64
	AvailableCoins    int64
	ExpiringSoonCoins int64
	NextExpireAt      *time.Time
}

// sqlite 的 MIN(datetime) 会扫成字符串，MySQL 则是 time.Time。
type balanceScan struct {
	AccountID         uint64 `gorm:"column:account_id"`
	AvailableCoins    int64  `gorm:"column:available_coins"`
	ExpiringSoonCoins int64  `gorm:"column:expiring_soon_coins"`
	NextExpireRaw     any    `gorm:"column:next_expire_at"`
}

type BalanceReviewSummary struct {
	AccountsWithBalance int64
	AvailableCoins      int64
	ExpiringSoonCoins   int64
}

// ListOrdersForReview 列出任意账号的充值单。不查 Stripe，也不改订单状态。
func (s *Service) ListOrdersForReview(req OrderReviewListRequest) ([]RechargeOrder, error) {
	limit, offset := clampList(req.Limit, req.Offset)
	q := s.db.Model(&RechargeOrder{}).Order("id DESC")
	if status := strings.TrimSpace(req.Status); status != "" {
		q = q.Where("status = ?", status)
	}
	if trade := strings.TrimSpace(req.OutTradeNo); trade != "" {
		q = q.Where("out_trade_no = ?", trade)
	}
	if req.AccountID > 0 {
		q = q.Where("account_id = ?", req.AccountID)
	}
	var rows []RechargeOrder
	err := q.Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}

func (s *Service) OrderReviewSummary() (OrderReviewSummary, error) {
	type agg struct {
		Status string
		Count  int64
		Yuan   int64
	}
	var rows []agg
	if err := s.db.Model(&RechargeOrder{}).
		Select("status, COUNT(*) AS count, COALESCE(SUM(yuan),0) AS yuan").
		Group("status").
		Scan(&rows).Error; err != nil {
		return OrderReviewSummary{}, err
	}
	var out OrderReviewSummary
	for _, row := range rows {
		switch row.Status {
		case OrderPaid:
			out.PaidCount = row.Count
			out.PaidYuan = row.Yuan
		case OrderPending:
			out.PendingCount = row.Count
		case OrderClosed:
			out.ClosedCount = row.Count
		}
	}
	if err := s.db.Model(&PlatformEntry{}).Select("COALESCE(SUM(amount),0)").Scan(&out.PlatformCutCoins).Error; err != nil {
		return OrderReviewSummary{}, err
	}
	return out, nil
}

// ListBalancesForReview 按当前可花余额聚合。不过期批次，避免管理面读一眼就改账本。
func (s *Service) ListBalancesForReview(accountID uint64, limit, offset int) ([]BalanceRow, error) {
	limit, offset = clampList(limit, offset)
	now := s.now()
	warn := now.Add(ExpireWarnWindow)
	availableExpr := "CASE WHEN remaining > 0 AND (expire_at IS NULL OR expire_at > ?) THEN remaining ELSE 0 END"
	expiringExpr := "CASE WHEN remaining > 0 AND expire_at IS NOT NULL AND expire_at > ? AND expire_at <= ? THEN remaining ELSE 0 END"
	nextExpireExpr := "CASE WHEN remaining > 0 AND expire_at IS NOT NULL AND expire_at > ? THEN expire_at END"

	q := s.db.Model(&Lot{}).
		Select("account_id, SUM("+availableExpr+") AS available_coins, SUM("+expiringExpr+") AS expiring_soon_coins, MIN("+nextExpireExpr+") AS next_expire_at",
			now, now, warn, now).
		Group("account_id")
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	} else {
		q = q.Having("SUM("+availableExpr+") > 0", now)
	}
	var scans []balanceScan
	if err := q.Order("available_coins DESC").Limit(limit).Offset(offset).Scan(&scans).Error; err != nil {
		return nil, err
	}
	rows := make([]BalanceRow, 0, len(scans))
	for i := range scans {
		rows = append(rows, BalanceRow{
			AccountID:         scans[i].AccountID,
			AvailableCoins:    scans[i].AvailableCoins,
			ExpiringSoonCoins: scans[i].ExpiringSoonCoins,
			NextExpireAt:      parseReviewTime(scans[i].NextExpireRaw),
		})
	}
	return rows, nil
}

func parseReviewTime(raw any) *time.Time {
	switch v := raw.(type) {
	case nil:
		return nil
	case time.Time:
		if v.IsZero() {
			return nil
		}
		t := v
		return &t
	case *time.Time:
		return v
	case string:
		return parseReviewTimeString(v)
	case []byte:
		return parseReviewTimeString(string(v))
	default:
		return nil
	}
}

func parseReviewTimeString(raw string) *time.Time {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999+00:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func (s *Service) BalanceReviewSummary() (BalanceReviewSummary, error) {
	now := s.now()
	warn := now.Add(ExpireWarnWindow)
	var out BalanceReviewSummary
	err := s.db.Model(&Lot{}).
		Select(`COUNT(DISTINCT CASE WHEN remaining > 0 AND (expire_at IS NULL OR expire_at > ?) THEN account_id END) AS accounts_with_balance,
			COALESCE(SUM(CASE WHEN remaining > 0 AND (expire_at IS NULL OR expire_at > ?) THEN remaining ELSE 0 END),0) AS available_coins,
			COALESCE(SUM(CASE WHEN remaining > 0 AND expire_at IS NOT NULL AND expire_at > ? AND expire_at <= ? THEN remaining ELSE 0 END),0) AS expiring_soon_coins`,
			now, now, now, warn).
		Scan(&out).Error
	return out, err
}

func (s *Service) SummaryForReview(accountID uint64) (Summary, error) {
	return s.repo.summary(s.db, accountID, s.now())
}

func (s *Service) LotsForReview(accountID uint64) ([]Lot, error) {
	now := s.now()
	var lots []Lot
	err := s.db.Where("account_id = ? AND remaining > 0 AND (expire_at IS NULL OR expire_at > ?)", accountID, now).
		Order("CASE WHEN expire_at IS NULL THEN 1 ELSE 0 END ASC, expire_at ASC, created_at ASC, id ASC").
		Find(&lots).Error
	return lots, err
}

func PayRef(order *RechargeOrder) string {
	if order == nil {
		return ""
	}
	if strings.TrimSpace(order.StripeSession) != "" {
		return order.StripeSession
	}
	return order.AlipayTradeNo
}
