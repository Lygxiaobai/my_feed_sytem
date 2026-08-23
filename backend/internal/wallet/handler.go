package wallet

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"my_feed_system/internal/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup) {
	rg.POST("/stripe/notify", h.StripeNotify)
}

func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup) {
	rg.POST("/summary", h.Summary)
	rg.POST("/ledger", h.ListLedger)
	rg.POST("/checkin", h.Checkin)
	rg.POST("/checkin/month", h.CheckinMonth)
	rg.POST("/lottery", h.Lottery)
	rg.POST("/recharge/create", h.CreateRecharge)
	rg.POST("/recharge/query", h.QueryOrder)
	rg.POST("/tip", h.Tip)
	rg.POST("/tips/mine", h.ListMyTips)
	rg.POST("/tips/byVideo", h.ListVideoTips)
}

func (h *Handler) Summary(c *gin.Context) {
	summary, err := h.service.Summary(c.GetUint64("account_id"))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.DatabaseError, err)
		return
	}
	response.OK(c, gin.H{"summary": summary, "packages": RechargePackages()})
}

func (h *Handler) ListLedger(c *gin.Context) {
	var req ListRequest
	_ = c.ShouldBindJSON(&req)
	items, err := h.service.ListLedger(c.GetUint64("account_id"), req)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.DatabaseError, err)
		return
	}
	response.OK(c, gin.H{"ledgers": items})
}

func (h *Handler) Checkin(c *gin.Context) {
	prize, err := h.service.Checkin(c.GetUint64("account_id"))
	if err != nil {
		writeWalletError(c, err)
		return
	}
	response.OK(c, gin.H{"coins": prize})
}

func (h *Handler) CheckinMonth(c *gin.Context) {
	out, err := h.service.CheckinMonth(c.GetUint64("account_id"))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.DatabaseError, err)
		return
	}
	response.OK(c, out)
}

func (h *Handler) Lottery(c *gin.Context) {
	result, err := h.service.Lottery(c.GetUint64("account_id"))
	if err != nil {
		writeWalletError(c, err)
		return
	}
	response.OK(c, gin.H{"coins": result.Coins, "prize_index": result.PrizeIndex})
}

func (h *Handler) CreateRecharge(c *gin.Context) {
	var req CreateRechargeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	order, checkout, err := h.service.CreateRecharge(c.GetUint64("account_id"), req)
	if err != nil {
		writeWalletError(c, err)
		return
	}
	out := gin.H{
		"order":  orderView(order),
		"method": checkout.Method,
	}
	if checkout.CheckoutURL != "" {
		out["checkout_url"] = checkout.CheckoutURL
	}
	response.OK(c, out)
}

func (h *Handler) QueryOrder(c *gin.Context) {
	var req QueryOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	order, err := h.service.QueryOrder(c.GetUint64("account_id"), req.OutTradeNo)
	if err != nil {
		writeWalletError(c, err)
		return
	}
	response.OK(c, gin.H{"order": orderView(order)})
}

func (h *Handler) Tip(c *gin.Context) {
	var req TipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	rec, err := h.service.Tip(c.GetUint64("account_id"), c.GetString("account_username"), req)
	if err != nil {
		writeWalletError(c, err)
		return
	}
	response.OK(c, gin.H{"tip": rec})
}

func (h *Handler) ListMyTips(c *gin.Context) {
	var req ListRequest
	_ = c.ShouldBindJSON(&req)
	items, err := h.service.ListMyTips(c.GetUint64("account_id"), req)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.DatabaseError, err)
		return
	}
	response.OK(c, gin.H{"tips": items})
}

func (h *Handler) ListVideoTips(c *gin.Context) {
	var req ListVideoTipsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	items, err := h.service.ListVideoTips(c.GetUint64("account_id"), req)
	if err != nil {
		writeWalletError(c, err)
		return
	}
	response.OK(c, gin.H{"tips": items})
}

// StripeNotify 必须读原始 body 验签，不能走统一信封。
func (h *Handler) StripeNotify(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		slog.Warn("stripe notify read failed", slog.String("error", err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if err := h.service.HandleStripeNotify(body, c.GetHeader("Stripe-Signature")); err != nil {
		slog.Warn("stripe notify rejected", slog.String("error", err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	c.String(http.StatusOK, "ok")
}

func writeWalletError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInsufficient):
		response.FailTip(c, http.StatusBadRequest, response.InsufficientBalance, "积分不足", err)
	case errors.Is(err, ErrTipSelf):
		response.FailTip(c, http.StatusForbidden, response.AccessDenied, "不能给自己的视频打赏", err)
	case errors.Is(err, ErrTipTooFrequent):
		response.FailTip(c, http.StatusTooManyRequests, response.DuplicatedRequest, "操作过于频繁，请稍后再试", err)
	case errors.Is(err, ErrAlreadyClaimed):
		response.FailTip(c, http.StatusConflict, response.DuplicatedRequest, "今天已经领取过了", err)
	case errors.Is(err, ErrStripeNotConfigured):
		response.FailTip(c, http.StatusServiceUnavailable, response.ThirdPartyError, "Stripe 未配置", err)
	case errors.Is(err, ErrOrderNotFound), errors.Is(err, ErrOrderNotOwned), errors.Is(err, ErrVideoNotTippable):
		response.Fail(c, http.StatusNotFound, response.ResourceNotFound, err)
	case errors.Is(err, ErrInvalidAmount):
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "金额不正确", err)
	case errors.Is(err, ErrInvalidPayMethod):
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "支付方式不正确", err)
	default:
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
	}
}

func orderView(order *RechargeOrder) gin.H {
	if order == nil {
		return nil
	}
	return gin.H{
		"out_trade_no": order.OutTradeNo,
		"yuan":         order.Yuan,
		"coins":        order.Coins,
		"bonus":        order.Bonus,
		"status":       order.Status,
		"expire_at":    order.ExpireAt,
		"paid_at":      order.PaidAt,
	}
}
