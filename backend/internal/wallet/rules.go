package wallet

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const (
	YuanToCoins       int64 = 10
	RegisterGiftCoins int64 = 10
	TipMinCoins       int64 = 10
	CutPercent        int64 = 30
	CheckinBucketMod        = 1000
	LotteryBucketMod        = 100
	OrderTTL                = 30 * time.Minute
	TipDebounce             = 10 * time.Second
	ExpireWarnWindow        = 72 * time.Hour
	RegisterGiftTTL         = 30 * 24 * time.Hour
	PromoTTL                = 15 * 24 * time.Hour
	TipInTTL                = 90 * 24 * time.Hour
	DefaultListLimit        = 20
	MaxListLimit            = 50
)

// LotteryPrizes 是抽奖六档，下标即接口返回的 prize_index。
var LotteryPrizes = []int64{0, 2, 5, 10, 20, 50}

// Package 是充值档位。未命中档位的整元按自定义处理，无赠送。
type Package struct {
	Yuan  int64 `json:"yuan"`
	Bonus int64 `json:"bonus"`
}

var rechargePackages = []Package{
	{Yuan: 6, Bonus: 0},
	{Yuan: 30, Bonus: 30},
	{Yuan: 68, Bonus: 100},
	{Yuan: 128, Bonus: 250},
}

func RechargePackages() []Package {
	out := make([]Package, len(rechargePackages))
	copy(out, rechargePackages)
	return out
}

func resolveRecharge(yuan int64) (coins int64, bonus int64, err error) {
	if yuan < 1 {
		return 0, 0, fmt.Errorf("%w: 充值金额至少 1 元", ErrInvalidAmount)
	}
	coins = yuan * YuanToCoins
	for _, pkg := range rechargePackages {
		if pkg.Yuan == yuan {
			return coins, pkg.Bonus, nil
		}
	}
	return coins, 0, nil
}

func splitTip(coins int64) (received int64, cut int64) {
	received = coins * (100 - CutPercent) / 100
	cut = coins - received
	return received, cut
}

func expireFor(source string, now time.Time) *time.Time {
	var ttl time.Duration
	switch source {
	case SourceRegister:
		ttl = RegisterGiftTTL
	case SourceCheckin, SourceLottery:
		ttl = PromoTTL
	case SourceTipIn:
		ttl = TipInTTL
	default:
		return nil
	}
	t := now.Add(ttl)
	return &t
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func bizDate(now time.Time) string {
	return now.In(shanghai()).Format("2006-01-02")
}

func modRange(n, m int) int {
	if m <= 0 {
		return 0
	}
	n %= m
	if n < 0 {
		n += m
	}
	return n
}

// drawCheckinPrize 把 0–999 映射到 1–20 积分：前 950 档均匀对应 1–10，后 50 档均匀对应 11–20。
func drawCheckinPrize(n int) int64 {
	n = modRange(n, CheckinBucketMod)
	if n < 950 {
		return int64(n/95) + 1
	}
	return int64((n-950)/5) + 11
}

// drawLottery 把 0–99 映射到六档：50% / 25% / 12% / 8% / 4% / 1%，对应 LotteryPrizes。
func drawLottery(n int) (coins int64, prizeIndex int) {
	n = modRange(n, LotteryBucketMod)
	switch {
	case n < 50:
		return LotteryPrizes[0], 0
	case n < 75:
		return LotteryPrizes[1], 1
	case n < 87:
		return LotteryPrizes[2], 2
	case n < 95:
		return LotteryPrizes[3], 3
	case n < 99:
		return LotteryPrizes[4], 4
	default:
		return LotteryPrizes[5], 5
	}
}

func randomLotteryBucket() (int, error) {
	return randomBucket(LotteryBucketMod)
}

func randomCheckinBucket() (int, error) {
	return randomBucket(CheckinBucketMod)
}

func randomBucket(mod int) (int, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint64(buf[:]) % uint64(mod)), nil
}

func clampList(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func parseYuanAmount(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("%w: empty amount", ErrInvalidAmount)
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	whole, frac, ok := strings.Cut(s, ".")
	if !ok {
		n, err := parseDigits(whole)
		if err != nil {
			return 0, err
		}
		if neg {
			return -n, nil
		}
		return n, nil
	}
	if frac == "" {
		n, err := parseDigits(whole)
		if err != nil {
			return 0, err
		}
		if neg {
			return -n, nil
		}
		return n, nil
	}
	if len(frac) > 2 {
		return 0, fmt.Errorf("%w: too many decimals", ErrInvalidAmount)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if frac != "00" {
		return 0, fmt.Errorf("%w: amount must be whole yuan", ErrInvalidAmount)
	}
	n, err := parseDigits(whole)
	if err != nil {
		return 0, err
	}
	if neg {
		return -n, nil
	}
	return n, nil
}

func parseDigits(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("%w: empty digits", ErrInvalidAmount)
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%w: not a number", ErrInvalidAmount)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

func formatYuan(yuan int64) string {
	return fmt.Sprintf("%d.00", yuan)
}

func normalizePayMethod(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", PayMethodStripe, "card", "test":
		return PayMethodStripe, nil
	default:
		return "", fmt.Errorf("%w: unknown pay method", ErrInvalidPayMethod)
	}
}
