package wallet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/audit"
	"my_feed_system/internal/config"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/notification"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
)

type Service struct {
	db          *gorm.DB
	repo        *Repo
	videoRepo   *video.Repo
	stripe      StripeGateway
	publisher   *mq.Publisher
	outboxRepo  *outbox.Repo
	popularity  *popularity.Service
	now         func() time.Time
	draw        func() (int, error)
	drawCheckin func() (int, error)
	notify      *notification.Writer
	interest    interestInvalidator
}

type interestInvalidator interface {
	InvalidateUser(accountID uint64)
}

func (s *Service) SetNotifier(n *notification.Writer) {
	s.notify = n
}

func (s *Service) SetInterestInvalidator(v interestInvalidator) {
	s.interest = v
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		db:          db,
		repo:        NewRepo(db),
		videoRepo:   video.NewRepo(db),
		now:         time.Now,
		draw:        randomLotteryBucket,
		drawCheckin: randomCheckinBucket,
	}
}

func (s *Service) ConfigureStripe(cfg config.StripeConfig) {
	if client, err := NewStripeClient(cfg); err == nil {
		s.stripe = client
	}
}

func (s *Service) SetStripe(g StripeGateway) { s.stripe = g }

func (s *Service) SetPublisher(publisher *mq.Publisher, popularityService *popularity.Service) {
	s.publisher = publisher
	s.popularity = popularityService
	if publisher != nil {
		s.outboxRepo = outbox.NewRepo(s.db)
	}
}

func (s *Service) GrantRegisterGiftTx(tx *gorm.DB, accountID uint64) error {
	exists, err := s.repo.hasRegisterGift(tx, accountID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.grantInTx(tx, accountID, SourceRegister, RegisterGiftCoins, LedgerGrantRegister, "account", strconv.FormatUint(accountID, 10))
}

func (s *Service) Summary(accountID uint64) (Summary, error) {
	now := s.now()
	if err := s.expireAccount(accountID, now); err != nil {
		return Summary{}, err
	}
	return s.repo.summary(s.db, accountID, now)
}

func (s *Service) ListLedger(accountID uint64, req ListRequest) ([]Ledger, error) {
	limit, offset := clampList(req.Limit, req.Offset)
	var items []Ledger
	err := s.db.Where("account_id = ?", accountID).
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error
	return items, err
}

func (s *Service) Checkin(accountID uint64) (int64, error) {
	n, err := s.drawCheckin()
	if err != nil {
		return 0, err
	}
	prize := drawCheckinPrize(n)
	now := s.now()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.claimDaily(tx, accountID, ActionCheckin, prize, now); err != nil {
			return err
		}
		return s.grantInTx(tx, accountID, SourceCheckin, prize, LedgerGrantCheckin, "checkin", bizDate(now))
	})
	return prize, err
}

func (s *Service) CheckinMonth(accountID uint64) (CheckinMonth, error) {
	now := s.now().In(shanghai())
	year, month, _ := now.Date()
	start := time.Date(year, month, 1, 0, 0, 0, 0, shanghai()).Format("2006-01-02")
	end := time.Date(year, month+1, 0, 0, 0, 0, 0, shanghai()).Format("2006-01-02")
	today := bizDate(now)

	actions, err := s.repo.checkinDaysInRange(s.db, accountID, start, end)
	if err != nil {
		return CheckinMonth{}, err
	}
	days := make([]CheckinDay, 0, len(actions))
	claimedToday := false
	for _, action := range actions {
		days = append(days, CheckinDay{BizDate: action.BizDate, Coins: action.Prize})
		if action.BizDate == today {
			claimedToday = true
		}
	}
	return CheckinMonth{
		Year:         year,
		Month:        int(month),
		Today:        today,
		ClaimedToday: claimedToday,
		Days:         days,
	}, nil
}

func (s *Service) Lottery(accountID uint64) (LotteryResult, error) {
	n, err := s.draw()
	if err != nil {
		return LotteryResult{}, err
	}
	prize, prizeIndex := drawLottery(n)
	now := s.now()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.claimDaily(tx, accountID, ActionLottery, prize, now); err != nil {
			return err
		}
		if prize == 0 {
			return s.writeLedger(tx, accountID, 0, 0, LedgerGrantLottery, "lottery", bizDate(now))
		}
		return s.grantInTx(tx, accountID, SourceLottery, prize, LedgerGrantLottery, "lottery", bizDate(now))
	})
	if err != nil {
		return LotteryResult{}, err
	}
	return LotteryResult{Coins: prize, PrizeIndex: prizeIndex}, nil
}

func (s *Service) claimDaily(tx *gorm.DB, accountID uint64, action string, prize int64, now time.Time) error {
	actionRow := DailyAction{
		AccountID: accountID,
		Action:    action,
		BizDate:   bizDate(now),
		Prize:     prize,
		CreatedAt: now,
	}
	if err := tx.Create(&actionRow).Error; err != nil {
		if isDuplicateKey(err) {
			return ErrAlreadyClaimed
		}
		return err
	}
	return nil
}

func (s *Service) Tip(accountID uint64, username string, req TipRequest) (*TipRecord, error) {
	if req.Coins < TipMinCoins {
		return nil, fmt.Errorf("%w: 最少 %d 积分", ErrInvalidAmount, TipMinCoins)
	}
	current, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.AuditStatus != audit.StatusApproved {
		return nil, ErrVideoNotTippable
	}
	if current.AuthorID == accountID {
		return nil, ErrTipSelf
	}

	now := s.now()
	received, cut := splitTip(req.Coins)
	var rec TipRecord
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.expireAccountTx(tx, accountID, now); err != nil {
			return err
		}
		last, err := s.repo.lastTip(tx, accountID, req.VideoID)
		if err != nil {
			return err
		}
		if last != nil && now.Sub(last.CreatedAt) < TipDebounce {
			return ErrTipTooFrequent
		}
		rec = TipRecord{
			FromAccountID: accountID,
			FromUsername:  username,
			ToAccountID:   current.AuthorID,
			VideoID:       req.VideoID,
			Coins:         req.Coins,
			Received:      received,
			Cut:           cut,
			CreatedAt:     now,
		}
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		if err := s.notify.ApplyTip(tx, rec.ID, accountID, current.AuthorID, req.VideoID, req.Coins); err != nil {
			return err
		}
		ref := strconv.FormatUint(rec.ID, 10)
		if err := s.spendInTx(tx, accountID, req.Coins, LedgerConsumeTip, "tip", ref); err != nil {
			return err
		}
		if received > 0 {
			if err := s.grantInTx(tx, current.AuthorID, SourceTipIn, received, LedgerGrantTip, "tip", ref); err != nil {
				return err
			}
		}
		if cut > 0 {
			if err := tx.Create(&PlatformEntry{Amount: cut, RefType: "tip", RefID: ref, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		if s.publisher != nil && s.outboxRepo != nil {
			event, err := mq.NewEnvelope(mq.EventTypePopularityChanged, mq.ProducerAPIServer, mq.PopularityChangedPayload{
				VideoID: req.VideoID,
				Delta:   req.Coins,
				Reason:  mq.EventTypeTipCreated,
			})
			if err != nil {
				return err
			}
			if err := s.outboxRepo.Enqueue(tx, event); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if s.publisher == nil && s.popularity != nil {
		_ = s.popularity.Record(context.Background(), req.VideoID, float64(req.Coins), now)
	}
	if s.interest != nil {
		s.interest.InvalidateUser(accountID)
	}
	return &rec, nil
}

func (s *Service) ListMyTips(accountID uint64, req ListRequest) ([]TipRecord, error) {
	limit, offset := clampList(req.Limit, req.Offset)
	var items []TipRecord
	err := s.db.Where("from_account_id = ?", accountID).
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error
	return items, err
}

func (s *Service) ListVideoTips(viewerID uint64, req ListVideoTipsRequest) ([]TipRecord, error) {
	current, err := s.videoRepo.FindByID(req.VideoID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrVideoNotTippable
	}
	limit, offset := clampList(req.Limit, req.Offset)
	q := s.db.Where("video_id = ?", req.VideoID)
	// 作者看全量；登录观众只看自己打过的，没打过返回空列表而不是 403。
	if current.AuthorID != viewerID {
		q = q.Where("from_account_id = ?", viewerID)
	}
	var items []TipRecord
	err = q.Order("id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, err
}

func (s *Service) CreateRecharge(accountID uint64, req CreateRechargeRequest) (*RechargeOrder, RechargeCheckout, error) {
	coins, bonus, err := resolveRecharge(req.Yuan)
	if err != nil {
		return nil, RechargeCheckout{}, err
	}
	method, err := normalizePayMethod(req.Method)
	if err != nil {
		return nil, RechargeCheckout{}, err
	}
	if s.stripe == nil {
		return nil, RechargeCheckout{}, ErrStripeNotConfigured
	}
	now := s.now()
	outTradeNo, err := newOutTradeNo(accountID, now)
	if err != nil {
		return nil, RechargeCheckout{}, err
	}
	order := RechargeOrder{
		AccountID:  accountID,
		OutTradeNo: outTradeNo,
		Yuan:       req.Yuan,
		Coins:      coins,
		Bonus:      bonus,
		Status:     OrderPending,
		PayMethod:  method,
		ExpireAt:   now.Add(OrderTTL),
		CreatedAt:  now,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		pending, err := s.repo.pendingOrders(tx, accountID)
		if err != nil {
			return err
		}
		for i := range pending {
			closed := now
			if err := tx.Model(&pending[i]).Updates(map[string]any{
				"status":    OrderClosed,
				"closed_at": closed,
			}).Error; err != nil {
				return err
			}
		}
		return tx.Create(&order).Error
	}); err != nil {
		return nil, RechargeCheckout{}, err
	}

	session, err := s.stripe.CreateCheckout(StripeCheckoutRequest{
		OutTradeNo: order.OutTradeNo,
		Yuan:       order.Yuan,
		Subject:    fmt.Sprintf("积分充值 %d 元", order.Yuan),
		ExpireAt:   order.ExpireAt,
	})
	if err != nil {
		_ = s.db.Model(&order).Updates(map[string]any{"status": OrderClosed, "closed_at": now}).Error
		return nil, RechargeCheckout{}, err
	}
	if err := s.db.Model(&order).Update("stripe_session", session.SessionID).Error; err != nil {
		_ = s.db.Model(&order).Updates(map[string]any{"status": OrderClosed, "closed_at": now}).Error
		return nil, RechargeCheckout{}, err
	}
	order.StripeSession = session.SessionID
	return &order, RechargeCheckout{Method: method, CheckoutURL: session.URL}, nil
}

func (s *Service) QueryOrder(accountID uint64, outTradeNo string) (*RechargeOrder, error) {
	var order RechargeOrder
	if err := s.db.Where("out_trade_no = ?", outTradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	if order.AccountID != accountID {
		return nil, ErrOrderNotOwned
	}
	if order.Status == OrderPending && s.stripe != nil {
		if err := s.refreshStripePending(&order); err != nil {
			slog.Warn("stripe query failed", slog.String("out_trade_no", outTradeNo), slog.String("error", err.Error()))
		}
	}
	return &order, nil
}

// FindPaidRecharge 只返回本账号已入账的充值单。未支付或未拥有都当不存在，
// 避免发票申请变成订单枚举旁路。
func (s *Service) FindPaidRecharge(accountID uint64, outTradeNo string) (*PaidRecharge, error) {
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return nil, nil
	}
	var order RechargeOrder
	err := s.db.Where("out_trade_no = ? AND account_id = ? AND status = ?", outTradeNo, accountID, OrderPaid).
		First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return paidFromOrder(&order), nil
}

func (s *Service) ListPaidRecharges(accountID uint64, limit, offset int) ([]PaidRecharge, error) {
	limit, offset = clampList(limit, offset)
	var orders []RechargeOrder
	err := s.db.Where("account_id = ? AND status = ?", accountID, OrderPaid).
		Order("paid_at DESC, id DESC").
		Limit(limit).
		Offset(offset).
		Find(&orders).Error
	if err != nil {
		return nil, err
	}
	out := make([]PaidRecharge, 0, len(orders))
	for i := range orders {
		out = append(out, *paidFromOrder(&orders[i]))
	}
	return out, nil
}

func paidFromOrder(order *RechargeOrder) *PaidRecharge {
	paidAt := order.CreatedAt
	if order.PaidAt != nil {
		paidAt = *order.PaidAt
	}
	return &PaidRecharge{
		OutTradeNo: order.OutTradeNo,
		AccountID:  order.AccountID,
		Yuan:       order.Yuan,
		Coins:      order.Coins,
		Bonus:      order.Bonus,
		PayMethod:  order.PayMethod,
		PaidAt:     paidAt,
	}
}

func (s *Service) HandleStripeNotify(payload []byte, sigHeader string) error {
	if s.stripe == nil {
		return ErrStripeNotConfigured
	}
	event, err := s.stripe.VerifyEvent(payload, sigHeader, s.now())
	if err != nil {
		return err
	}
	if event.Type != "checkout.session.completed" && event.Type != "checkout.session.async_payment_succeeded" {
		return nil
	}
	if !stripePaid(event.Checkout.PaymentStatus) && event.Type != "checkout.session.completed" {
		return nil
	}
	return s.creditStripe(event.OutTradeNo, event.Checkout)
}

func (s *Service) refreshStripePending(order *RechargeOrder) error {
	if strings.TrimSpace(order.StripeSession) == "" {
		return nil
	}
	session, err := s.stripe.QuerySession(order.StripeSession)
	if err != nil {
		return err
	}
	if stripePaid(session.PaymentStatus) {
		if err := s.creditStripe(order.OutTradeNo, session); err != nil {
			return err
		}
		return s.db.Where("out_trade_no = ?", order.OutTradeNo).First(order).Error
	}
	if s.now().After(order.ExpireAt) {
		if err := s.closeOrder(order.OutTradeNo); err != nil {
			return err
		}
		return s.db.Where("out_trade_no = ?", order.OutTradeNo).First(order).Error
	}
	return nil
}

func (s *Service) creditStripe(outTradeNo string, session StripeCheckout) error {
	order, err := s.repo.findOrderByOutTradeNo(s.db, outTradeNo)
	if err != nil {
		return err
	}
	if order == nil {
		return ErrOrderNotFound
	}
	if session.AmountTotal != order.Yuan*100 {
		return fmt.Errorf("%w: amount mismatch", ErrNotifyInvalid)
	}
	ref := firstNonEmpty(session.PaymentRef, session.SessionID)
	return s.creditPaid(outTradeNo, ref, formatYuan(order.Yuan))
}

func (s *Service) creditPaid(outTradeNo, tradeNo, totalAmount string) error {
	yuan, err := parseYuanAmount(totalAmount)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		order, err := s.repo.findOrderByOutTradeNo(tx, outTradeNo)
		if err != nil {
			return err
		}
		if order == nil {
			return ErrOrderNotFound
		}
		if order.Status == OrderPaid {
			return nil
		}
		if order.Yuan != yuan {
			return fmt.Errorf("%w: amount mismatch", ErrNotifyInvalid)
		}
		now := s.now()
		if err := tx.Model(order).Updates(map[string]any{
			"status":          OrderPaid,
			"alipay_trade_no": tradeNo,
			"paid_at":         now,
		}).Error; err != nil {
			return err
		}
		if err := s.grantInTx(tx, order.AccountID, SourceRecharge, order.Coins, LedgerGrantRecharge, "recharge", order.OutTradeNo); err != nil {
			return err
		}
		if order.Bonus > 0 {
			if err := s.grantInTx(tx, order.AccountID, SourceRecharge, order.Bonus, LedgerGrantRechargeBonus, "recharge", order.OutTradeNo); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) closeOrder(outTradeNo string) error {
	now := s.now()
	return s.db.Transaction(func(tx *gorm.DB) error {
		order, err := s.repo.findOrderByOutTradeNo(tx, outTradeNo)
		if err != nil || order == nil {
			return err
		}
		if order.Status != OrderPending {
			return nil
		}
		return tx.Model(order).Updates(map[string]any{
			"status":    OrderClosed,
			"closed_at": now,
		}).Error
	})
}

func (s *Service) Sweep(ctx context.Context) {
	now := s.now()
	if err := s.sweepExpired(now); err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "sweep expired lots failed", slog.String("error", err.Error()))
	}
	if err := s.sweepExpiredOrders(now); err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "sweep expired orders failed", slog.String("error", err.Error()))
	}
}

func (s *Service) sweepExpired(now time.Time) error {
	for {
		var n int
		err := s.db.Transaction(func(tx *gorm.DB) error {
			lots, err := s.repo.expiredLots(tx, now, 100)
			if err != nil {
				return err
			}
			n = len(lots)
			for i := range lots {
				if err := s.expireLot(tx, &lots[i], now); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || n == 0 {
			return err
		}
	}
}

func (s *Service) sweepExpiredOrders(now time.Time) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		orders, err := s.repo.duePendingOrders(tx, now, 100)
		if err != nil {
			return err
		}
		for i := range orders {
			if err := tx.Model(&orders[i]).Updates(map[string]any{
				"status":    OrderClosed,
				"closed_at": now,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) expireAccount(accountID uint64, now time.Time) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		return s.expireAccountTx(tx, accountID, now)
	})
}

func (s *Service) expireAccountTx(tx *gorm.DB, accountID uint64, now time.Time) error {
	lots, err := s.repo.expiredLotsForAccount(tx, accountID, now)
	if err != nil {
		return err
	}
	for i := range lots {
		if err := s.expireLot(tx, &lots[i], now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) expireLot(tx *gorm.DB, lot *Lot, now time.Time) error {
	if lot.Remaining <= 0 {
		return nil
	}
	amount := lot.Remaining
	if err := tx.Model(lot).Update("remaining", 0).Error; err != nil {
		return err
	}
	return tx.Create(&Ledger{
		AccountID: lot.AccountID,
		LotID:     lot.ID,
		BizType:   LedgerExpire,
		Amount:    -amount,
		RefType:   lot.Source,
		RefID:     strconv.FormatUint(lot.ID, 10),
		CreatedAt: now,
	}).Error
}

func (s *Service) grantInTx(tx *gorm.DB, accountID uint64, source string, amount int64, bizType, refType, refID string) error {
	if amount <= 0 {
		return nil
	}
	now := s.now()
	lot := Lot{
		AccountID: accountID,
		Source:    source,
		Remaining: amount,
		ExpireAt:  expireFor(source, now),
		CreatedAt: now,
	}
	if err := tx.Create(&lot).Error; err != nil {
		return err
	}
	return s.writeLedger(tx, accountID, lot.ID, amount, bizType, refType, refID)
}

func (s *Service) spendInTx(tx *gorm.DB, accountID uint64, amount int64, bizType, refType, refID string) error {
	if amount <= 0 {
		return fmt.Errorf("%w: spend must be positive", ErrInvalidAmount)
	}
	now := s.now()
	lots, err := s.repo.availableLots(tx, accountID, now)
	if err != nil {
		return err
	}
	var total int64
	for _, lot := range lots {
		total += lot.Remaining
	}
	if total < amount {
		return ErrInsufficient
	}
	left := amount
	for i := range lots {
		if left == 0 {
			break
		}
		take := lots[i].Remaining
		if take > left {
			take = left
		}
		if err := tx.Model(&lots[i]).Update("remaining", lots[i].Remaining-take).Error; err != nil {
			return err
		}
		if err := s.writeLedger(tx, accountID, lots[i].ID, -take, bizType, refType, refID); err != nil {
			return err
		}
		left -= take
	}
	return nil
}

func (s *Service) writeLedger(tx *gorm.DB, accountID, lotID uint64, amount int64, bizType, refType, refID string) error {
	return tx.Create(&Ledger{
		AccountID: accountID,
		LotID:     lotID,
		BizType:   bizType,
		Amount:    amount,
		RefType:   refType,
		RefID:     refID,
		CreatedAt: s.now(),
	}).Error
}

func newOutTradeNo(accountID uint64, now time.Time) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("WR%d%d%s", accountID, now.Unix(), hex.EncodeToString(b[:])), nil
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate") ||
		strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "unique constraint")
}
