package invoice

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	taxIDPattern = regexp.MustCompile(`^[0-9A-Z]{15,20}$`)
	emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
	phonePattern = regexp.MustCompile(`^[0-9+\-() ]{6,32}$`)
)

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
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

func normalizeKind(raw string) (string, error) {
	kind := strings.ToLower(strings.TrimSpace(raw))
	if kind == "" {
		kind = KindPersonal
	}
	// 只开个人消费凭证，企业增值税申请不在本产品范围内。
	if kind != KindPersonal {
		return "", ErrInvalidKind
	}
	return kind, nil
}

func compactRunes(raw string, max int) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
		if utf8.RuneCountInString(b.String()) >= max {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func normalizeHeader(req Header) (Header, error) {
	kind, err := normalizeKind(req.Kind)
	if err != nil {
		return Header{}, err
	}
	title := compactRunes(req.Title, TitleMaxRunes)
	if utf8.RuneCountInString(title) < TitleMinRunes {
		return Header{}, ErrInvalidTitle
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || utf8.RuneCountInString(email) > EmailMaxRunes || !emailPattern.MatchString(email) {
		return Header{}, ErrInvalidEmail
	}
	taxID := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(req.TaxID), " ", ""))
	if taxID != "" && !taxIDPattern.MatchString(taxID) {
		return Header{}, ErrInvalidTaxID
	}
	phone := strings.TrimSpace(req.Phone)
	if phone != "" && !phonePattern.MatchString(phone) {
		return Header{}, ErrInvalidHeader
	}
	out := Header{
		Kind:        kind,
		Title:       title,
		TaxID:       taxID,
		Email:       email,
		BankName:    compactRunes(req.BankName, BankNameMax),
		BankAccount: compactRunes(req.BankAccount, BankAccountMax),
		Address:     compactRunes(req.Address, AddressMax),
		Phone:       phone,
	}
	return out, nil
}

func headerFromApply(req ApplyRequest) Header {
	return Header{
		Kind:        req.Kind,
		Title:       req.Title,
		TaxID:       req.TaxID,
		Email:       req.Email,
		BankName:    req.BankName,
		BankAccount: req.BankAccount,
		Address:     req.Address,
		Phone:       req.Phone,
	}
}

func headerFromSave(req SaveProfileRequest) Header {
	return Header{
		Kind:        req.Kind,
		Title:       req.Title,
		TaxID:       req.TaxID,
		Email:       req.Email,
		BankName:    req.BankName,
		BankAccount: req.BankAccount,
		Address:     req.Address,
		Phone:       req.Phone,
	}
}

func newInvoiceNo(now time.Time) (string, error) {
	var buf [5]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "INV" + now.In(shanghai()).Format("20060102") + strings.ToUpper(hex.EncodeToString(buf[:])), nil
}
