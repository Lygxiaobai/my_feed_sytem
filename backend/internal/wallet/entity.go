package wallet

import "time"

const (
	SourceCheckin  = "checkin"
	SourceLottery  = "lottery"
	SourceRecharge = "recharge"
	SourceRegister = "register"
	SourceTipIn    = "tip_in"
)

const (
	LedgerGrantRegister      = "grant_register"
	LedgerGrantCheckin       = "grant_checkin"
	LedgerGrantLottery       = "grant_lottery"
	LedgerGrantRecharge      = "grant_recharge"
	LedgerGrantRechargeBonus = "grant_recharge_bonus"
	LedgerGrantTip           = "grant_tip"
	LedgerConsumeTip         = "consume_tip"
	LedgerExpire             = "expire"
	LedgerPlatformCut        = "platform_cut"
)

const (
	ActionCheckin = "checkin"
	ActionLottery = "lottery"
)

const (
	OrderPending = "pending"
	OrderPaid    = "paid"
	OrderClosed  = "closed"
)

// Lot 是一笔尚未花完的积分批次。充值桶的 ExpireAt 为空。
type Lot struct {
	ID        uint64     `gorm:"primaryKey" json:"id"`
	AccountID uint64     `gorm:"not null;index:idx_wallet_lots_account_remain,priority:1" json:"account_id"`
	Source    string     `gorm:"size:32;not null;index:idx_wallet_lots_account_source,priority:2" json:"source"`
	Remaining int64      `gorm:"not null;index:idx_wallet_lots_account_remain,priority:2" json:"remaining"`
	ExpireAt  *time.Time `gorm:"index" json:"expire_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (Lot) TableName() string { return "wallet_lots" }

// Ledger 是用户可见的积分流水。平台抽成记在 PlatformEntry，不进用户流水。
type Ledger struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	AccountID uint64    `gorm:"not null;index:idx_wallet_ledgers_account_created,priority:1" json:"account_id"`
	LotID     uint64    `gorm:"index" json:"lot_id,omitempty"`
	BizType   string    `gorm:"size:32;not null" json:"biz_type"`
	Amount    int64     `gorm:"not null" json:"amount"`
	RefType   string    `gorm:"size:32" json:"ref_type,omitempty"`
	RefID     string    `gorm:"size:64;index" json:"ref_id,omitempty"`
	CreatedAt time.Time `gorm:"index:idx_wallet_ledgers_account_created,priority:2" json:"created_at"`
}

func (Ledger) TableName() string { return "wallet_ledgers" }

// DailyAction 保证签到/抽奖每个北京时间自然日只能成功一次。
type DailyAction struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	AccountID uint64    `gorm:"not null;uniqueIndex:uk_wallet_daily" json:"account_id"`
	Action    string    `gorm:"size:16;not null;uniqueIndex:uk_wallet_daily" json:"action"`
	BizDate   string    `gorm:"size:10;not null;uniqueIndex:uk_wallet_daily" json:"biz_date"`
	Prize     int64     `gorm:"not null" json:"prize"`
	CreatedAt time.Time `json:"created_at"`
}

func (DailyAction) TableName() string { return "wallet_daily_actions" }

// RechargeOrder 是一笔充值单。AlipayTradeNo 是历史列，现在存 Stripe 支付引用。
type RechargeOrder struct {
	ID            uint64     `gorm:"primaryKey" json:"id"`
	AccountID     uint64     `gorm:"not null;index:idx_wallet_orders_account_status,priority:1" json:"account_id"`
	OutTradeNo    string     `gorm:"size:64;not null;uniqueIndex" json:"out_trade_no"`
	Yuan          int64      `gorm:"not null" json:"yuan"`
	Coins         int64      `gorm:"not null" json:"coins"`
	Bonus         int64      `gorm:"not null" json:"bonus"`
	Status        string     `gorm:"size:16;not null;index:idx_wallet_orders_account_status,priority:2" json:"status"`
	PayMethod     string     `gorm:"size:16" json:"pay_method,omitempty"`
	AlipayTradeNo string     `gorm:"size:64" json:"alipay_trade_no,omitempty"`
	StripeSession string     `gorm:"size:128" json:"stripe_session,omitempty"`
	PaidAt        *time.Time `json:"paid_at,omitempty"`
	ClosedAt      *time.Time `json:"closed_at,omitempty"`
	ExpireAt      time.Time  `gorm:"index" json:"expire_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (RechargeOrder) TableName() string { return "wallet_recharge_orders" }

// PaidRecharge 是已入账充值单的只读快照。发票模块只读这个形状，不回写入账。
type PaidRecharge struct {
	OutTradeNo string
	AccountID  uint64
	Yuan       int64
	Coins      int64
	Bonus      int64
	PayMethod  string
	PaidAt     time.Time
}

// TipRecord 记录一次视频打赏。Coins 是打赏人扣掉的积分。
type TipRecord struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	FromAccountID uint64    `gorm:"not null;index:idx_wallet_tips_from_video,priority:1" json:"from_account_id"`
	FromUsername  string    `gorm:"size:64;not null" json:"from_username"`
	ToAccountID   uint64    `gorm:"not null;index" json:"to_account_id"`
	VideoID       uint64    `gorm:"not null;index:idx_wallet_tips_video_created,priority:1;index:idx_wallet_tips_from_video,priority:2" json:"video_id"`
	Coins         int64     `gorm:"not null" json:"coins"`
	Received      int64     `gorm:"not null" json:"received"`
	Cut           int64     `gorm:"not null" json:"cut"`
	CreatedAt     time.Time `gorm:"index:idx_wallet_tips_video_created,priority:2" json:"created_at"`
}

func (TipRecord) TableName() string { return "wallet_tips" }

// PlatformEntry 记录平台抽成，用户不可见。
type PlatformEntry struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Amount    int64     `gorm:"not null" json:"amount"`
	RefType   string    `gorm:"size:32;not null" json:"ref_type"`
	RefID     string    `gorm:"size:64;not null;index" json:"ref_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (PlatformEntry) TableName() string { return "wallet_platform_entries" }

type Summary struct {
	AvailableCoins    int64      `json:"available_coins"`
	ExpiringSoonCoins int64      `json:"expiring_soon_coins"`
	NextExpireAt      *time.Time `json:"next_expire_at,omitempty"`
	NextExpireCoins   int64      `json:"next_expire_coins,omitempty"`
}

const (
	PayMethodQR     = "qr"
	PayMethodPage   = "page"
	PayMethodStripe = "stripe"
)

type RechargeCheckout struct {
	Method      string `json:"method"`
	CheckoutURL string `json:"checkout_url,omitempty"`
}

type CreateRechargeRequest struct {
	Yuan   int64  `json:"yuan" binding:"required"`
	Method string `json:"method"`
}

type TipRequest struct {
	VideoID uint64 `json:"video_id" binding:"required"`
	Coins   int64  `json:"coins" binding:"required"`
}

type QueryOrderRequest struct {
	OutTradeNo string `json:"out_trade_no" binding:"required"`
}

type ListRequest struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type ListVideoTipsRequest struct {
	VideoID uint64 `json:"video_id" binding:"required"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

type LotteryResult struct {
	Coins      int64 `json:"coins"`
	PrizeIndex int   `json:"prize_index"`
}

type CheckinDay struct {
	BizDate string `json:"biz_date"`
	Coins   int64  `json:"coins"`
}

type CheckinMonth struct {
	Year         int          `json:"year"`
	Month        int          `json:"month"`
	Today        string       `json:"today"`
	ClaimedToday bool         `json:"claimed_today"`
	Days         []CheckinDay `json:"days"`
}
