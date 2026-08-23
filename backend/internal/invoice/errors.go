package invoice

import "errors"

var (
	ErrInvalidKind     = errors.New("invalid invoice kind")
	ErrInvalidTitle    = errors.New("invalid invoice title")
	ErrInvalidTaxID    = errors.New("invalid taxpayer id")
	ErrInvalidEmail    = errors.New("invalid invoice email")
	ErrInvalidHeader   = errors.New("invalid invoice header")
	ErrOrderNotFound   = errors.New("paid recharge not found")
	ErrAlreadyIssued   = errors.New("invoice already issued")
	ErrInvoiceNotFound = errors.New("invoice not found")
	ErrNotOwner        = errors.New("invoice not owned")
)
