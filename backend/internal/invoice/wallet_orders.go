package invoice

import "my_feed_system/internal/wallet"

type walletOrderStore struct {
	wallet *wallet.Service
}

// OrdersFromWallet 把钱包的已支付查询接到发票的只读端口。
func OrdersFromWallet(w *wallet.Service) PaidOrderStore {
	return walletOrderStore{wallet: w}
}

func (s walletOrderStore) FindPaid(accountID uint64, outTradeNo string) (*PaidOrder, error) {
	row, err := s.wallet.FindPaidRecharge(accountID, outTradeNo)
	if err != nil || row == nil {
		return nil, err
	}
	return paidFromWallet(row), nil
}

func (s walletOrderStore) ListPaid(accountID uint64, limit, offset int) ([]PaidOrder, error) {
	rows, err := s.wallet.ListPaidRecharges(accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]PaidOrder, 0, len(rows))
	for i := range rows {
		out = append(out, *paidFromWallet(&rows[i]))
	}
	return out, nil
}

func paidFromWallet(row *wallet.PaidRecharge) *PaidOrder {
	return &PaidOrder{
		OutTradeNo: row.OutTradeNo,
		AccountID:  row.AccountID,
		Yuan:       row.Yuan,
		Coins:      row.Coins,
		Bonus:      row.Bonus,
		PayMethod:  row.PayMethod,
		PaidAt:     row.PaidAt,
	}
}
