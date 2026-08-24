package admin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"my_feed_system/internal/account"
	"my_feed_system/internal/invoice"
	"my_feed_system/internal/response"
	"my_feed_system/internal/video"
)

// Handler 暴露管理后台接口。全部需要登录，除 /access 外都必须是审核员。
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup, readMW []gin.HandlerFunc, writeMW []gin.HandlerFunc) {
	rg.POST("/access", h.Access)
	rg.POST("/overview", append(append([]gin.HandlerFunc{}, readMW...), h.Overview)...)
	rg.POST("/videos/lookup", append(append([]gin.HandlerFunc{}, readMW...), h.LookupVideo)...)
	rg.POST("/accounts/lookup", append(append([]gin.HandlerFunc{}, readMW...), h.LookupAccount)...)
	rg.POST("/videos/takedown", append(append([]gin.HandlerFunc{}, writeMW...), h.Takedown)...)
	rg.POST("/invoices/list", append(append([]gin.HandlerFunc{}, readMW...), h.ListInvoices)...)
	rg.POST("/invoices/get", append(append([]gin.HandlerFunc{}, readMW...), h.GetInvoice)...)
	rg.POST("/payments/list", append(append([]gin.HandlerFunc{}, readMW...), h.ListPayments)...)
	rg.POST("/balances/list", append(append([]gin.HandlerFunc{}, readMW...), h.ListBalances)...)
	rg.POST("/balances/get", append(append([]gin.HandlerFunc{}, readMW...), h.GetBalance)...)
	rg.POST("/videos/list", append(append([]gin.HandlerFunc{}, readMW...), h.ListVideos)...)
	rg.POST("/accounts/list", append(append([]gin.HandlerFunc{}, readMW...), h.ListAccounts)...)
}

// Access 告诉前端当前账号能不能进管理后台。
//
// 未授权也回 200 + allowed=false：这是入口探测，不应当成一次失败处置。
func (h *Handler) Access(c *gin.Context) {
	result := h.service.Access(c.GetUint64("account_id"))
	response.OK(c, result)
}

func (h *Handler) Overview(c *gin.Context) {
	overview, err := h.service.Overview(c.GetUint64("account_id"), c.GetString("account_username"))
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, overview)
}

func (h *Handler) LookupVideo(c *gin.Context) {
	var req LookupVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	item, err := h.service.LookupVideo(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"video": item})
}

func (h *Handler) Takedown(c *gin.Context) {
	var req TakedownRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	if err := h.service.Takedown(c.Request.Context(), c.GetUint64("account_id"), req); err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *Handler) LookupAccount(c *gin.Context) {
	var req LookupAccountRequest
	_ = c.ShouldBindJSON(&req)

	acc, videos, err := h.service.LookupAccount(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"account": acc, "videos": videos})
}

func (h *Handler) ListVideos(c *gin.Context) {
	var req ListVideosRequest
	_ = c.ShouldBindJSON(&req)
	board, err := h.service.ListVideos(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, board)
}

func (h *Handler) ListAccounts(c *gin.Context) {
	var req ListAccountsRequest
	_ = c.ShouldBindJSON(&req)
	board, err := h.service.ListAccounts(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, board)
}

func (h *Handler) ListInvoices(c *gin.Context) {
	var req ListInvoicesRequest
	_ = c.ShouldBindJSON(&req)
	board, err := h.service.ListInvoices(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, board)
}

func (h *Handler) GetInvoice(c *gin.Context) {
	var req GetInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	item, err := h.service.GetInvoice(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"invoice": item})
}

func (h *Handler) ListPayments(c *gin.Context) {
	var req ListPaymentsRequest
	_ = c.ShouldBindJSON(&req)
	board, err := h.service.ListPayments(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, board)
}

func (h *Handler) ListBalances(c *gin.Context) {
	var req ListBalancesRequest
	_ = c.ShouldBindJSON(&req)
	board, err := h.service.ListBalances(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, board)
}

func (h *Handler) GetBalance(c *gin.Context) {
	var req GetBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	item, err := h.service.GetBalance(c.GetUint64("account_id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, gin.H{"balance": item})
}

func (h *Handler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotReviewer):
		response.Fail(c, http.StatusForbidden, response.AccessDenied, err)
	case errors.Is(err, ErrNoteRequired):
		response.FailTip(c, http.StatusBadRequest, response.ParamMissing, "下架必须填写处置说明", err)
	case errors.Is(err, ErrNoteTooLong):
		response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "处置说明过长", err)
	case errors.Is(err, ErrLookupMissing):
		response.FailTip(c, http.StatusBadRequest, response.ParamMissing, "请填写账号 ID、用户名或邮箱其中一项", err)
	case errors.Is(err, ErrLookupAmbiguous):
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "请只使用一种查询方式", err)
	case errors.Is(err, ErrInvalidOrderStatus):
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "订单状态不正确", err)
	case errors.Is(err, ErrInvalidAuditStatus):
		response.FailTip(c, http.StatusBadRequest, response.ParamError, "审核状态不正确", err)
	case errors.Is(err, invoice.ErrInvoiceNotFound):
		response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "发票不存在", err)
	case errors.Is(err, video.ErrVideoNotFound):
		response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "内容不存在或已被删除", err)
	case errors.Is(err, account.ErrAccountNotFound):
		response.Fail(c, http.StatusNotFound, response.AccountNotFound, err)
	default:
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
	}
}
