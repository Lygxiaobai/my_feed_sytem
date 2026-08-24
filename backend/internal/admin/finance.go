package admin

import (
	"strings"

	"my_feed_system/internal/account"
	"my_feed_system/internal/invoice"
	"my_feed_system/internal/wallet"
)

func (s *Service) ListInvoices(operatorID uint64, req ListInvoicesRequest) (*InvoiceBoard, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	summary, err := s.invoices.ReviewSummary()
	if err != nil {
		return nil, err
	}
	rows, err := s.invoices.ListForReview(invoice.ReviewListRequest{
		InvoiceNo:  strings.TrimSpace(req.InvoiceNo),
		OutTradeNo: strings.TrimSpace(req.OutTradeNo),
		AccountID:  req.AccountID,
		Limit:      req.Limit,
		Offset:     req.Offset,
	})
	if err != nil {
		return nil, err
	}
	names, err := s.usernamesFromInvoiceRows(rows)
	if err != nil {
		return nil, err
	}
	items := make([]InvoiceView, 0, len(rows))
	for i := range rows {
		items = append(items, invoiceView(&rows[i], names[rows[i].AccountID]))
	}
	return &InvoiceBoard{
		Summary: InvoiceBoardSummary{
			IssuedCount: summary.IssuedCount,
			YuanTotal:   summary.YuanTotal,
			CoinsTotal:  summary.CoinsTotal,
		},
		Invoices: items,
		HasMore:  listHasMore(req.Limit, len(items)),
	}, nil
}

func (s *Service) GetInvoice(operatorID uint64, req GetInvoiceRequest) (*InvoiceView, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	row, err := s.invoices.GetForReview(strings.TrimSpace(req.InvoiceNo))
	if err != nil {
		return nil, err
	}
	names, err := s.accounts.UsernamesByIDs([]uint64{row.AccountID})
	if err != nil {
		return nil, err
	}
	view := invoiceView(row, names[row.AccountID])
	return &view, nil
}

func (s *Service) ListPayments(operatorID uint64, req ListPaymentsRequest) (*PaymentBoard, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	status, err := normalizeOrderStatus(req.Status)
	if err != nil {
		return nil, err
	}
	summary, err := s.wallets.OrderReviewSummary()
	if err != nil {
		return nil, err
	}
	rows, err := s.wallets.ListOrdersForReview(wallet.OrderReviewListRequest{
		Status:     status,
		OutTradeNo: strings.TrimSpace(req.OutTradeNo),
		AccountID:  req.AccountID,
		Limit:      req.Limit,
		Offset:     req.Offset,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].AccountID)
	}
	names, err := s.accounts.UsernamesByIDs(ids)
	if err != nil {
		return nil, err
	}
	items := make([]PaymentView, 0, len(rows))
	for i := range rows {
		items = append(items, paymentView(&rows[i], names[rows[i].AccountID]))
	}
	return &PaymentBoard{
		Summary: PaymentBoardSummary{
			PaidCount:        summary.PaidCount,
			PaidYuan:         summary.PaidYuan,
			PendingCount:     summary.PendingCount,
			ClosedCount:      summary.ClosedCount,
			PlatformCutCoins: summary.PlatformCutCoins,
		},
		Orders:  items,
		HasMore: listHasMore(req.Limit, len(items)),
	}, nil
}

func (s *Service) ListBalances(operatorID uint64, req ListBalancesRequest) (*BalanceBoard, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	accountID, err := s.resolveBalanceAccount(req)
	if err != nil {
		return nil, err
	}
	summary, err := s.wallets.BalanceReviewSummary()
	if err != nil {
		return nil, err
	}
	rows, err := s.wallets.ListBalancesForReview(accountID, req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].AccountID)
	}
	names, err := s.accounts.UsernamesByIDs(ids)
	if err != nil {
		return nil, err
	}
	items := make([]BalanceView, 0, len(rows))
	for i := range rows {
		items = append(items, BalanceView{
			AccountID:         rows[i].AccountID,
			Username:          names[rows[i].AccountID],
			AvailableCoins:    rows[i].AvailableCoins,
			ExpiringSoonCoins: rows[i].ExpiringSoonCoins,
			NextExpireAt:      rows[i].NextExpireAt,
		})
	}
	return &BalanceBoard{
		Summary: BalanceBoardSummary{
			AccountsWithBalance: summary.AccountsWithBalance,
			AvailableCoins:      summary.AvailableCoins,
			ExpiringSoonCoins:   summary.ExpiringSoonCoins,
		},
		Balances: items,
		HasMore:  listHasMore(req.Limit, len(items)),
	}, nil
}

func (s *Service) GetBalance(operatorID uint64, req GetBalanceRequest) (*BalanceDetail, error) {
	if !s.IsReviewer(operatorID) {
		return nil, ErrNotReviewer
	}
	acc, err := s.accounts.FindByID(account.FindByIDRequest{ID: req.ID})
	if err != nil {
		return nil, err
	}
	summary, err := s.wallets.SummaryForReview(acc.ID)
	if err != nil {
		return nil, err
	}
	lots, err := s.wallets.LotsForReview(acc.ID)
	if err != nil {
		return nil, err
	}
	out := &BalanceDetail{
		AccountID: acc.ID,
		Username:  acc.Username,
		Lots:      make([]LotView, 0, len(lots)),
	}
	out.Summary.AvailableCoins = summary.AvailableCoins
	out.Summary.ExpiringSoonCoins = summary.ExpiringSoonCoins
	out.Summary.NextExpireAt = summary.NextExpireAt
	out.Summary.NextExpireCoins = summary.NextExpireCoins
	for i := range lots {
		out.Lots = append(out.Lots, LotView{
			Source:    lots[i].Source,
			Remaining: lots[i].Remaining,
			ExpireAt:  lots[i].ExpireAt,
			CreatedAt: lots[i].CreatedAt,
		})
	}
	return out, nil
}

func (s *Service) resolveBalanceAccount(req ListBalancesRequest) (uint64, error) {
	hasID := req.ID > 0
	hasName := strings.TrimSpace(req.Username) != ""
	if hasID && hasName {
		return 0, ErrLookupAmbiguous
	}
	if hasName {
		acc, err := s.accounts.FindByUsername(account.FindByUsernameRequest{Username: strings.TrimSpace(req.Username)})
		if err != nil {
			return 0, err
		}
		return acc.ID, nil
	}
	return req.ID, nil
}

func (s *Service) usernamesFromInvoiceRows(rows []invoice.Invoice) (map[uint64]string, error) {
	ids := make([]uint64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].AccountID)
	}
	return s.accounts.UsernamesByIDs(ids)
}

func invoiceView(row *invoice.Invoice, username string) InvoiceView {
	return InvoiceView{
		InvoiceNo:   row.InvoiceNo,
		OutTradeNo:  row.OutTradeNo,
		AccountID:   row.AccountID,
		Username:    username,
		Yuan:        row.Yuan,
		Coins:       row.Coins,
		Bonus:       row.Bonus,
		PayMethod:   row.PayMethod,
		PaidAt:      row.PaidAt,
		Kind:        row.Kind,
		Title:       row.Title,
		TaxID:       row.TaxID,
		Email:       row.Email,
		BankName:    row.BankName,
		BankAccount: row.BankAccount,
		Address:     row.Address,
		Phone:       row.Phone,
		Status:      row.Status,
		IssuedAt:    row.IssuedAt,
		CreatedAt:   row.CreatedAt,
	}
}

func paymentView(row *wallet.RechargeOrder, username string) PaymentView {
	return PaymentView{
		OutTradeNo: row.OutTradeNo,
		AccountID:  row.AccountID,
		Username:   username,
		Yuan:       row.Yuan,
		Coins:      row.Coins,
		Bonus:      row.Bonus,
		Status:     row.Status,
		PayMethod:  row.PayMethod,
		PayRef:     wallet.PayRef(row),
		PaidAt:     row.PaidAt,
		ClosedAt:   row.ClosedAt,
		ExpireAt:   row.ExpireAt,
		CreatedAt:  row.CreatedAt,
	}
}

func normalizeOrderStatus(raw string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	if status == "" {
		return "", nil
	}
	switch status {
	case wallet.OrderPending, wallet.OrderPaid, wallet.OrderClosed:
		return status, nil
	default:
		return "", ErrInvalidOrderStatus
	}
}

func listHasMore(limit, n int) bool {
	if limit <= 0 {
		return n >= invoice.DefaultListLimit
	}
	if limit > invoice.MaxListLimit {
		limit = invoice.MaxListLimit
	}
	return n >= limit
}
