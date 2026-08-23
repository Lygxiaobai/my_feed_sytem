package invoice

import "time"

const (
	KindPersonal = "personal"

	StatusIssued = "issued"

	DefaultListLimit = 20
	MaxListLimit     = 50
	TitleMinRunes    = 2
	TitleMaxRunes    = 80
	EmailMaxRunes    = 120
	TaxIDMinLen      = 15
	TaxIDMaxLen      = 20
	BankNameMax      = 64
	BankAccountMax   = 64
	AddressMax       = 160
	PhoneMax         = 32
)

// Profile 是账号默认开票抬头。申请时会回填，也可单独保存。
type Profile struct {
	AccountID   uint64    `gorm:"primaryKey" json:"account_id"`
	Kind        string    `gorm:"size:16;not null" json:"kind"`
	Title       string    `gorm:"size:160;not null" json:"title"`
	TaxID       string    `gorm:"size:32" json:"tax_id"`
	Email       string    `gorm:"size:160;not null" json:"email"`
	BankName    string    `gorm:"size:64" json:"bank_name,omitempty"`
	BankAccount string    `gorm:"size:64" json:"bank_account,omitempty"`
	Address     string    `gorm:"size:160" json:"address,omitempty"`
	Phone       string    `gorm:"size:32" json:"phone,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Profile) TableName() string { return "invoice_profiles" }

// Invoice 是一笔已支付充值对应的开票单据。金额在申请时从订单快照，开具后不可改。
type Invoice struct {
	ID              uint64     `gorm:"primaryKey" json:"id"`
	InvoiceNo       string     `gorm:"size:32;not null;uniqueIndex" json:"invoice_no"`
	AccountID       uint64     `gorm:"not null;index:idx_invoices_account_created,priority:1" json:"account_id"`
	OutTradeNo      string     `gorm:"size:64;not null;uniqueIndex" json:"out_trade_no"`
	Yuan            int64      `gorm:"not null" json:"yuan"`
	Coins           int64      `gorm:"not null" json:"coins"`
	Bonus           int64      `gorm:"not null" json:"bonus"`
	PayMethod       string     `gorm:"size:16" json:"pay_method"`
	PaidAt          time.Time  `json:"paid_at"`
	Kind            string     `gorm:"size:16;not null" json:"kind"`
	Title           string     `gorm:"size:160;not null" json:"title"`
	TaxID           string     `gorm:"size:32" json:"tax_id"`
	Email           string     `gorm:"size:160;not null" json:"email"`
	BankName        string     `gorm:"size:64" json:"bank_name,omitempty"`
	BankAccount     string     `gorm:"size:64" json:"bank_account,omitempty"`
	Address         string     `gorm:"size:160" json:"address,omitempty"`
	Phone           string     `gorm:"size:32" json:"phone,omitempty"`
	Status          string     `gorm:"size:16;not null;index:idx_invoices_status_id,priority:1" json:"status"`
	IssuedAt        *time.Time `json:"issued_at,omitempty"`
	IssuerAccountID uint64     `gorm:"not null;default:0" json:"issuer_account_id"`
	RejectReason    string     `gorm:"size:400" json:"reject_reason,omitempty"`
	CreatedAt       time.Time  `gorm:"index:idx_invoices_account_created,priority:2" json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (Invoice) TableName() string { return "invoices" }

type Header struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	TaxID       string `json:"tax_id"`
	Email       string `json:"email"`
	BankName    string `json:"bank_name"`
	BankAccount string `json:"bank_account"`
	Address     string `json:"address"`
	Phone       string `json:"phone"`
}

type ApplyRequest struct {
	OutTradeNo  string `json:"out_trade_no" binding:"required"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	TaxID       string `json:"tax_id"`
	Email       string `json:"email"`
	BankName    string `json:"bank_name"`
	BankAccount string `json:"bank_account"`
	Address     string `json:"address"`
	Phone       string `json:"phone"`
}

type SaveProfileRequest struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	TaxID       string `json:"tax_id"`
	Email       string `json:"email"`
	BankName    string `json:"bank_name"`
	BankAccount string `json:"bank_account"`
	Address     string `json:"address"`
	Phone       string `json:"phone"`
}

type ListRequest struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type GetRequest struct {
	InvoiceNo string `json:"invoice_no" binding:"required"`
}

type EligibleOrder struct {
	OutTradeNo string    `json:"out_trade_no"`
	Yuan       int64     `json:"yuan"`
	Coins      int64     `json:"coins"`
	Bonus      int64     `json:"bonus"`
	PayMethod  string    `json:"pay_method"`
	PaidAt     time.Time `json:"paid_at"`
}

type PaidOrder struct {
	OutTradeNo string
	AccountID  uint64
	Yuan       int64
	Coins      int64
	Bonus      int64
	PayMethod  string
	PaidAt     time.Time
}

// PaidOrderStore 由钱包实现。发票只读已入账订单，绝不回写入账或改订单状态。
type PaidOrderStore interface {
	FindPaid(accountID uint64, outTradeNo string) (*PaidOrder, error)
	ListPaid(accountID uint64, limit, offset int) ([]PaidOrder, error)
}
