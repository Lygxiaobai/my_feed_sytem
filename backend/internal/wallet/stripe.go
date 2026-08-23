package wallet

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"my_feed_system/internal/config"
)

type StripeGateway interface {
	CreateCheckout(req StripeCheckoutRequest) (StripeCheckout, error)
	QuerySession(sessionID string) (StripeCheckout, error)
	VerifyEvent(payload []byte, sigHeader string, now time.Time) (StripeEvent, error)
}

type StripeCheckoutRequest struct {
	OutTradeNo string
	Yuan       int64
	Subject    string
	SuccessURL string
	CancelURL  string
	ExpireAt   time.Time
}

type StripeCheckout struct {
	SessionID     string
	URL           string
	PaymentStatus string
	PaymentRef    string
	AmountTotal   int64
	Currency      string
}

type StripeEvent struct {
	Type       string
	Checkout   StripeCheckout
	OutTradeNo string
}

type StripeClient struct {
	cfg        config.StripeConfig
	httpClient *http.Client
	now        func() time.Time
	apiBase    string
}

func NewStripeClient(cfg config.StripeConfig) (*StripeClient, error) {
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, ErrStripeNotConfigured
	}
	currency := strings.ToLower(strings.TrimSpace(cfg.Currency))
	if currency == "" {
		currency = "usd"
	}
	cfg.Currency = currency
	if strings.TrimSpace(cfg.SuccessURL) == "" {
		cfg.SuccessURL = "https://lvmouren.indevs.in/wallet"
	}
	return &StripeClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		now:        time.Now,
		apiBase:    "https://api.stripe.com/v1",
	}, nil
}

func (c *StripeClient) CreateCheckout(req StripeCheckoutRequest) (StripeCheckout, error) {
	success := firstNonEmpty(req.SuccessURL, c.cfg.SuccessURL)
	cancel := firstNonEmpty(req.CancelURL, success)
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("client_reference_id", req.OutTradeNo)
	form.Set("success_url", withWalletReturn(success, req.OutTradeNo, false))
	form.Set("cancel_url", withWalletReturn(cancel, req.OutTradeNo, true))
	form.Set("metadata[out_trade_no]", req.OutTradeNo)
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", c.cfg.Currency)
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(req.Yuan*100, 10))
	form.Set("line_items[0][price_data][product_data][name]", req.Subject)
	if !req.ExpireAt.IsZero() {
		expire := req.ExpireAt
		// Checkout 要求至少 30 分钟，留一点余量避免卡在边界被拒。
		if min := c.now().Add(31 * time.Minute); expire.Before(min) {
			expire = min
		}
		form.Set("expires_at", strconv.FormatInt(expire.Unix(), 10))
	}
	var raw struct {
		ID            string `json:"id"`
		URL           string `json:"url"`
		PaymentStatus string `json:"payment_status"`
		PaymentIntent string `json:"payment_intent"`
		AmountTotal   int64  `json:"amount_total"`
		Currency      string `json:"currency"`
		Error         *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.stripeForm(http.MethodPost, "/checkout/sessions", form, &raw); err != nil {
		return StripeCheckout{}, err
	}
	if raw.Error != nil && raw.Error.Message != "" {
		return StripeCheckout{}, fmt.Errorf("stripe checkout: %s", raw.Error.Message)
	}
	if strings.TrimSpace(raw.ID) == "" || strings.TrimSpace(raw.URL) == "" {
		return StripeCheckout{}, fmt.Errorf("stripe checkout: empty session")
	}
	return StripeCheckout{
		SessionID:     raw.ID,
		URL:           raw.URL,
		PaymentStatus: raw.PaymentStatus,
		PaymentRef:    raw.PaymentIntent,
		AmountTotal:   raw.AmountTotal,
		Currency:      raw.Currency,
	}, nil
}

func (c *StripeClient) QuerySession(sessionID string) (StripeCheckout, error) {
	var raw struct {
		ID                string `json:"id"`
		PaymentStatus     string `json:"payment_status"`
		PaymentIntent     string `json:"payment_intent"`
		AmountTotal       int64  `json:"amount_total"`
		Currency          string `json:"currency"`
		ClientReferenceID string `json:"client_reference_id"`
		Error             *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.stripeForm(http.MethodGet, "/checkout/sessions/"+url.PathEscape(sessionID), nil, &raw); err != nil {
		return StripeCheckout{}, err
	}
	if raw.Error != nil && raw.Error.Message != "" {
		return StripeCheckout{}, fmt.Errorf("stripe query: %s", raw.Error.Message)
	}
	return StripeCheckout{
		SessionID:     raw.ID,
		PaymentStatus: raw.PaymentStatus,
		PaymentRef:    raw.PaymentIntent,
		AmountTotal:   raw.AmountTotal,
		Currency:      raw.Currency,
	}, nil
}

func (c *StripeClient) VerifyEvent(payload []byte, sigHeader string, now time.Time) (StripeEvent, error) {
	if strings.TrimSpace(c.cfg.WebhookSecret) == "" {
		return StripeEvent{}, fmt.Errorf("%w: missing webhook secret", ErrNotifyInvalid)
	}
	if err := verifyStripeSignature(payload, sigHeader, c.cfg.WebhookSecret, now, 5*time.Minute); err != nil {
		return StripeEvent{}, fmt.Errorf("%w: %v", ErrNotifyInvalid, err)
	}
	var raw struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string `json:"id"`
				Object            string `json:"object"`
				PaymentStatus     string `json:"payment_status"`
				PaymentIntent     string `json:"payment_intent"`
				AmountTotal       int64  `json:"amount_total"`
				Currency          string `json:"currency"`
				ClientReferenceID string `json:"client_reference_id"`
				Metadata          struct {
					OutTradeNo string `json:"out_trade_no"`
				} `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return StripeEvent{}, fmt.Errorf("%w: decode event", ErrNotifyInvalid)
	}
	outTradeNo := firstNonEmpty(raw.Data.Object.Metadata.OutTradeNo, raw.Data.Object.ClientReferenceID)
	return StripeEvent{
		Type:       raw.Type,
		OutTradeNo: outTradeNo,
		Checkout: StripeCheckout{
			SessionID:     raw.Data.Object.ID,
			PaymentStatus: raw.Data.Object.PaymentStatus,
			PaymentRef:    raw.Data.Object.PaymentIntent,
			AmountTotal:   raw.Data.Object.AmountTotal,
			Currency:      raw.Data.Object.Currency,
		},
	}, nil
}

func (c *StripeClient) stripeForm(method, path string, form url.Values, dest any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.apiBase+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.cfg.SecretKey, "")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Stripe-Version", "2024-06-20")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode stripe: %w", err)
	}
	return nil
}

func verifyStripeSignature(payload []byte, header, secret string, now time.Time, tol time.Duration) error {
	var ts int64
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch name {
		case "t":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("bad timestamp")
			}
			ts = n
		case "v1":
			signatures = append(signatures, value)
		}
	}
	if ts == 0 || len(signatures) == 0 {
		return fmt.Errorf("missing signature")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if d := now.Sub(time.Unix(ts, 0)); d > tol || d < -tol {
		return fmt.Errorf("timestamp outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strconv.FormatInt(ts, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	expect := hex.EncodeToString(mac.Sum(nil))
	for _, got := range signatures {
		if hmac.Equal([]byte(expect), []byte(got)) {
			return nil
		}
	}
	return fmt.Errorf("signature mismatch")
}

func withOutTradeNo(raw, outTradeNo string) string {
	return withWalletReturn(raw, outTradeNo, false)
}

func withWalletReturn(raw, outTradeNo string, canceled bool) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" {
		return strings.TrimSpace(raw)
	}
	q := u.Query()
	q.Set("out_trade_no", outTradeNo)
	if canceled {
		q.Set("canceled", "1")
	} else {
		q.Del("canceled")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func stripePaid(status string) bool {
	return status == "paid"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
