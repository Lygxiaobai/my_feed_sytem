package admin

import "time"

// NoteMaxLength 与举报补充说明同一上限，避免处置依据把列撑爆。
const NoteMaxLength = 500

type AccessResult struct {
	Allowed bool `json:"allowed"`
}

type Overview struct {
	PendingReports int64  `json:"pending_reports"`
	AccountID      uint64 `json:"account_id"`
	Username       string `json:"username"`
	IssuedInvoices int64  `json:"issued_invoices"`
	PaidYuan       int64  `json:"paid_yuan"`
	PaidOrders     int64  `json:"paid_orders"`
	PendingOrders  int64  `json:"pending_orders"`
	AvailableCoins int64  `json:"available_coins"`
	VideoCount     int64  `json:"video_count"`
	AccountCount   int64  `json:"account_count"`
}

type ListVideosRequest struct {
	Query       string `json:"query"`
	AuditStatus string `json:"audit_status"`
	AuthorID    uint64 `json:"author_id"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
}

type ListAccountsRequest struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type VideoBoard struct {
	Summary VideoBoardSummary `json:"summary"`
	Videos  []VideoView       `json:"videos"`
	HasMore bool              `json:"has_more"`
}

type VideoBoardSummary struct {
	Total     int64 `json:"total"`
	Pending   int64 `json:"pending"`
	Reviewing int64 `json:"reviewing"`
	Approved  int64 `json:"approved"`
	Rejected  int64 `json:"rejected"`
}

type AccountBoard struct {
	Summary  AccountBoardSummary `json:"summary"`
	Accounts []AccountView       `json:"accounts"`
	HasMore  bool                `json:"has_more"`
}

type AccountBoardSummary struct {
	Total int64 `json:"total"`
}

type InterestTag struct {
	Label   string `json:"label"`
	VideoID uint64 `json:"video_id"`
	Source  string `json:"source"`
}

type ListInvoicesRequest struct {
	InvoiceNo  string `json:"invoice_no"`
	OutTradeNo string `json:"out_trade_no"`
	AccountID  uint64 `json:"account_id"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

type GetInvoiceRequest struct {
	InvoiceNo string `json:"invoice_no" binding:"required"`
}

type ListPaymentsRequest struct {
	Status     string `json:"status"`
	OutTradeNo string `json:"out_trade_no"`
	AccountID  uint64 `json:"account_id"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

type ListBalancesRequest struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

type GetBalanceRequest struct {
	ID uint64 `json:"id" binding:"required"`
}

type InvoiceBoard struct {
	Summary  InvoiceBoardSummary `json:"summary"`
	Invoices []InvoiceView       `json:"invoices"`
	HasMore  bool                `json:"has_more"`
}

type InvoiceBoardSummary struct {
	IssuedCount int64 `json:"issued_count"`
	YuanTotal   int64 `json:"yuan_total"`
	CoinsTotal  int64 `json:"coins_total"`
}

type InvoiceView struct {
	InvoiceNo   string     `json:"invoice_no"`
	OutTradeNo  string     `json:"out_trade_no"`
	AccountID   uint64     `json:"account_id"`
	Username    string     `json:"username"`
	Yuan        int64      `json:"yuan"`
	Coins       int64      `json:"coins"`
	Bonus       int64      `json:"bonus"`
	PayMethod   string     `json:"pay_method"`
	PaidAt      time.Time  `json:"paid_at"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	TaxID       string     `json:"tax_id,omitempty"`
	Email       string     `json:"email"`
	BankName    string     `json:"bank_name,omitempty"`
	BankAccount string     `json:"bank_account,omitempty"`
	Address     string     `json:"address,omitempty"`
	Phone       string     `json:"phone,omitempty"`
	Status      string     `json:"status"`
	IssuedAt    *time.Time `json:"issued_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type PaymentBoard struct {
	Summary PaymentBoardSummary `json:"summary"`
	Orders  []PaymentView       `json:"orders"`
	HasMore bool                `json:"has_more"`
}

type PaymentBoardSummary struct {
	PaidCount        int64 `json:"paid_count"`
	PaidYuan         int64 `json:"paid_yuan"`
	PendingCount     int64 `json:"pending_count"`
	ClosedCount      int64 `json:"closed_count"`
	PlatformCutCoins int64 `json:"platform_cut_coins"`
}

type PaymentView struct {
	OutTradeNo string     `json:"out_trade_no"`
	AccountID  uint64     `json:"account_id"`
	Username   string     `json:"username"`
	Yuan       int64      `json:"yuan"`
	Coins      int64      `json:"coins"`
	Bonus      int64      `json:"bonus"`
	Status     string     `json:"status"`
	PayMethod  string     `json:"pay_method"`
	PayRef     string     `json:"pay_ref,omitempty"`
	PaidAt     *time.Time `json:"paid_at,omitempty"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	ExpireAt   time.Time  `json:"expire_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type BalanceBoard struct {
	Summary  BalanceBoardSummary `json:"summary"`
	Balances []BalanceView       `json:"balances"`
	HasMore  bool                `json:"has_more"`
}

type BalanceBoardSummary struct {
	AccountsWithBalance int64 `json:"accounts_with_balance"`
	AvailableCoins      int64 `json:"available_coins"`
	ExpiringSoonCoins   int64 `json:"expiring_soon_coins"`
}

type BalanceView struct {
	AccountID         uint64     `json:"account_id"`
	Username          string     `json:"username"`
	AvailableCoins    int64      `json:"available_coins"`
	ExpiringSoonCoins int64      `json:"expiring_soon_coins"`
	NextExpireAt      *time.Time `json:"next_expire_at,omitempty"`
}

type BalanceDetail struct {
	AccountID uint64 `json:"account_id"`
	Username  string `json:"username"`
	Summary   struct {
		AvailableCoins    int64      `json:"available_coins"`
		ExpiringSoonCoins int64      `json:"expiring_soon_coins"`
		NextExpireAt      *time.Time `json:"next_expire_at,omitempty"`
		NextExpireCoins   int64      `json:"next_expire_coins,omitempty"`
	} `json:"summary"`
	Lots []LotView `json:"lots"`
}

type LotView struct {
	Source    string     `json:"source"`
	Remaining int64      `json:"remaining"`
	ExpireAt  *time.Time `json:"expire_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type LookupVideoRequest struct {
	VideoID uint64 `json:"video_id" binding:"required"`
}

type TakedownRequest struct {
	VideoID uint64 `json:"video_id" binding:"required"`
	Note    string `json:"note"`
}

type LookupAccountRequest struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type VideoView struct {
	ID             uint64    `json:"id"`
	AuthorID       uint64    `json:"author_id"`
	Username       string    `json:"username"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Tags           []string  `json:"tags,omitempty"`
	PlayURL        string    `json:"play_url"`
	CoverURL       string    `json:"cover_url"`
	LikesCount     int64     `json:"likes_count"`
	CommentCount   int64     `json:"comment_count"`
	AuditStatus    string    `json:"audit_status"`
	CreatedAt      time.Time `json:"created_at"`
	PendingReports int64     `json:"pending_reports"`
}

type AccountView struct {
	ID            uint64        `json:"id"`
	Username      string        `json:"username"`
	Email         string        `json:"email,omitempty"`
	FollowerCount int64         `json:"follower_count"`
	CreatedAt     time.Time     `json:"created_at"`
	InterestTags  []InterestTag `json:"interest_tags,omitempty"`
}
