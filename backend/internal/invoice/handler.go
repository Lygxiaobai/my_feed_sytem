package invoice

import (
	"errors"
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

func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup, applyMW ...gin.HandlerFunc) {
	rg.POST("/profile", h.Profile)
	rg.POST("/profile/save", append(applyMW, h.SaveProfile)...)
	rg.POST("/eligible", h.Eligible)
	rg.POST("/apply", append(applyMW, h.Apply)...)
	rg.POST("/list", h.ListMine)
	rg.POST("/get", h.Get)
}

func (h *Handler) Profile(c *gin.Context) {
	row, err := h.service.Profile(c.GetUint64("account_id"))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.DatabaseError, err)
		return
	}
	response.OK(c, gin.H{"profile": profileView(row)})
}

func (h *Handler) SaveProfile(c *gin.Context) {
	var req SaveProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	row, err := h.service.SaveProfile(c.GetUint64("account_id"), req)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	response.OK(c, gin.H{"profile": profileView(row)})
}

func (h *Handler) Eligible(c *gin.Context) {
	var req ListRequest
	_ = c.ShouldBindJSON(&req)
	items, err := h.service.Eligible(c.GetUint64("account_id"), req)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.DatabaseError, err)
		return
	}
	response.OK(c, gin.H{"orders": items})
}

func (h *Handler) Apply(c *gin.Context) {
	var req ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	row, err := h.service.Apply(c.GetUint64("account_id"), req)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	response.OK(c, gin.H{"invoice": publicView(row)})
}

func (h *Handler) ListMine(c *gin.Context) {
	var req ListRequest
	_ = c.ShouldBindJSON(&req)
	rows, err := h.service.ListMine(c.GetUint64("account_id"), req)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, response.DatabaseError, err)
		return
	}
	items := make([]gin.H, 0, len(rows))
	for i := range rows {
		items = append(items, publicView(&rows[i]))
	}
	response.OK(c, gin.H{"invoices": items})
}

func (h *Handler) Get(c *gin.Context) {
	var req GetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, response.ParamError, err)
		return
	}
	row, err := h.service.Get(c.GetUint64("account_id"), req.InvoiceNo)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	response.OK(c, gin.H{"invoice": publicView(row)})
}

func writeInvoiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidKind):
		response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "目前只支持个人发票", err)
	case errors.Is(err, ErrInvalidTitle):
		response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "请填写有效的发票抬头", err)
	case errors.Is(err, ErrInvalidTaxID):
		response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "请填写有效的纳税人识别号", err)
	case errors.Is(err, ErrInvalidEmail):
		response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "请填写有效的接收邮箱", err)
	case errors.Is(err, ErrInvalidHeader):
		response.FailTip(c, http.StatusBadRequest, response.ParamFormatError, "开票资料格式不正确", err)
	case errors.Is(err, ErrOrderNotFound):
		response.FailTip(c, http.StatusNotFound, response.ResourceNotFound, "没有可开票的已支付订单", err)
	case errors.Is(err, ErrAlreadyIssued):
		response.FailTip(c, http.StatusConflict, response.DuplicatedRequest, "该订单已经开过发票", err)
	case errors.Is(err, ErrInvoiceNotFound), errors.Is(err, ErrNotOwner):
		response.Fail(c, http.StatusNotFound, response.ResourceNotFound, err)
	default:
		response.Fail(c, http.StatusInternalServerError, response.SystemError, err)
	}
}

func profileView(row *Profile) gin.H {
	if row == nil {
		return gin.H{"kind": KindPersonal}
	}
	return gin.H{
		"kind":         row.Kind,
		"title":        row.Title,
		"tax_id":       row.TaxID,
		"email":        row.Email,
		"bank_name":    row.BankName,
		"bank_account": row.BankAccount,
		"address":      row.Address,
		"phone":        row.Phone,
	}
}

func publicView(row *Invoice) gin.H {
	if row == nil {
		return nil
	}
	return gin.H{
		"invoice_no":    row.InvoiceNo,
		"out_trade_no":  row.OutTradeNo,
		"yuan":          row.Yuan,
		"coins":         row.Coins,
		"bonus":         row.Bonus,
		"pay_method":    row.PayMethod,
		"paid_at":       row.PaidAt,
		"kind":          row.Kind,
		"title":         row.Title,
		"tax_id":        row.TaxID,
		"email":         row.Email,
		"bank_name":     row.BankName,
		"bank_account":  row.BankAccount,
		"address":       row.Address,
		"phone":         row.Phone,
		"status":        row.Status,
		"issued_at":     row.IssuedAt,
		"reject_reason": row.RejectReason,
		"created_at":    row.CreatedAt,
	}
}
