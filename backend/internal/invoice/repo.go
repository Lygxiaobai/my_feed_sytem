package invoice

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) loadProfile(tx *gorm.DB, accountID uint64) (*Profile, error) {
	var row Profile
	err := tx.Where("account_id = ?", accountID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repo) upsertProfile(tx *gorm.DB, row *Profile) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"kind", "title", "tax_id", "email", "bank_name", "bank_account", "address", "phone", "updated_at",
		}),
	}).Create(row).Error
}

func (r *Repo) findByOutTradeNo(tx *gorm.DB, outTradeNo string) (*Invoice, error) {
	var row Invoice
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("out_trade_no = ?", outTradeNo).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repo) findByInvoiceNo(tx *gorm.DB, invoiceNo string) (*Invoice, error) {
	var row Invoice
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("invoice_no = ?", invoiceNo).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repo) listByAccount(tx *gorm.DB, accountID uint64, limit, offset int) ([]Invoice, error) {
	var rows []Invoice
	err := tx.Where("account_id = ?", accountID).
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error
	return rows, err
}

func (r *Repo) listByOutTradeNos(tx *gorm.DB, accountID uint64, nos []string) ([]Invoice, error) {
	if len(nos) == 0 {
		return nil, nil
	}
	var rows []Invoice
	err := tx.Where("account_id = ? AND out_trade_no IN ?", accountID, nos).Find(&rows).Error
	return rows, err
}

func (r *Repo) create(tx *gorm.DB, row *Invoice) error {
	return tx.Create(row).Error
}

func (r *Repo) save(tx *gorm.DB, row *Invoice) error {
	return tx.Save(row).Error
}

func applyHeader(row *Invoice, header Header, order *PaidOrder, now time.Time) {
	row.AccountID = order.AccountID
	row.OutTradeNo = order.OutTradeNo
	row.Yuan = order.Yuan
	row.Coins = order.Coins
	row.Bonus = order.Bonus
	row.PayMethod = order.PayMethod
	row.PaidAt = order.PaidAt
	row.Kind = header.Kind
	row.Title = header.Title
	row.TaxID = header.TaxID
	row.Email = header.Email
	row.BankName = header.BankName
	row.BankAccount = header.BankAccount
	row.Address = header.Address
	row.Phone = header.Phone
	row.UpdatedAt = now
}
