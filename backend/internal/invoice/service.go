package invoice

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type Service struct {
	db     *gorm.DB
	repo   *Repo
	orders PaidOrderStore
	now    func() time.Time
}

func NewService(db *gorm.DB, orders PaidOrderStore) *Service {
	return &Service{
		db:     db,
		repo:   NewRepo(db),
		orders: orders,
		now:    time.Now,
	}
}

func (s *Service) Profile(accountID uint64) (*Profile, error) {
	row, err := s.repo.loadProfile(s.db, accountID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &Profile{AccountID: accountID, Kind: KindPersonal}, nil
	}
	return row, nil
}

func (s *Service) SaveProfile(accountID uint64, req SaveProfileRequest) (*Profile, error) {
	header, err := normalizeHeader(headerFromSave(req))
	if err != nil {
		return nil, err
	}
	now := s.now()
	row := profileFromHeader(accountID, header, now)
	if err := s.repo.upsertProfile(s.db, row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) Eligible(accountID uint64, req ListRequest) ([]EligibleOrder, error) {
	limit, offset := clampList(req.Limit, req.Offset)
	paid, err := s.orders.ListPaid(accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	nos := make([]string, 0, len(paid))
	for _, item := range paid {
		nos = append(nos, item.OutTradeNo)
	}
	existing, err := s.repo.listByOutTradeNos(s.db, accountID, nos)
	if err != nil {
		return nil, err
	}
	issued := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		if row.Status == StatusIssued {
			issued[row.OutTradeNo] = struct{}{}
		}
	}
	out := make([]EligibleOrder, 0, len(paid))
	for _, item := range paid {
		if _, ok := issued[item.OutTradeNo]; ok {
			continue
		}
		out = append(out, EligibleOrder{
			OutTradeNo: item.OutTradeNo,
			Yuan:       item.Yuan,
			Coins:      item.Coins,
			Bonus:      item.Bonus,
			PayMethod:  item.PayMethod,
			PaidAt:     item.PaidAt,
		})
	}
	return out, nil
}

func (s *Service) Apply(accountID uint64, req ApplyRequest) (*Invoice, error) {
	header, err := normalizeHeader(headerFromApply(req))
	if err != nil {
		return nil, err
	}
	order, err := s.orders.FindPaid(accountID, strings.TrimSpace(req.OutTradeNo))
	if err != nil {
		return nil, err
	}
	if order == nil || order.AccountID != accountID {
		return nil, ErrOrderNotFound
	}

	now := s.now()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.upsertProfile(tx, profileFromHeader(accountID, header, now)); err != nil {
			return err
		}
		row, err := s.repo.findByOutTradeNo(tx, order.OutTradeNo)
		if err != nil {
			return err
		}
		if row != nil && row.Status == StatusIssued {
			return ErrAlreadyIssued
		}
		if row == nil {
			row = &Invoice{CreatedAt: now}
		}
		no, err := newInvoiceNo(now)
		if err != nil {
			return err
		}
		row.InvoiceNo = no
		applyHeader(row, header, order, now)
		row.RejectReason = ""
		row.IssuerAccountID = 0
		issued := now
		row.Status = StatusIssued
		row.IssuedAt = &issued
		if row.ID == 0 {
			return s.repo.create(tx, row)
		}
		return s.repo.save(tx, row)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.findByOutTradeNo(s.db, order.OutTradeNo)
}

func (s *Service) ListMine(accountID uint64, req ListRequest) ([]Invoice, error) {
	limit, offset := clampList(req.Limit, req.Offset)
	return s.repo.listByAccount(s.db, accountID, limit, offset)
}

func (s *Service) Get(accountID uint64, invoiceNo string) (*Invoice, error) {
	row, err := s.lookup(strings.TrimSpace(invoiceNo))
	if err != nil {
		return nil, err
	}
	if row.AccountID != accountID {
		return nil, ErrNotOwner
	}
	return row, nil
}

func (s *Service) lookup(invoiceNo string) (*Invoice, error) {
	if invoiceNo == "" {
		return nil, ErrInvoiceNotFound
	}
	row, err := s.repo.findByInvoiceNo(s.db, invoiceNo)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrInvoiceNotFound
	}
	return row, nil
}

func profileFromHeader(accountID uint64, header Header, now time.Time) *Profile {
	return &Profile{
		AccountID:   accountID,
		Kind:        header.Kind,
		Title:       header.Title,
		TaxID:       header.TaxID,
		Email:       header.Email,
		BankName:    header.BankName,
		BankAccount: header.BankAccount,
		Address:     header.Address,
		Phone:       header.Phone,
		UpdatedAt:   now,
	}
}
