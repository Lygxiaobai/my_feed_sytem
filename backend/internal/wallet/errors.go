package wallet

import "errors"

var (
	ErrInvalidAmount       = errors.New("invalid wallet amount")
	ErrInsufficient        = errors.New("insufficient coins")
	ErrTipSelf             = errors.New("cannot tip own video")
	ErrTipTooFrequent      = errors.New("tip too frequent")
	ErrAlreadyClaimed      = errors.New("daily action already claimed")
	ErrOrderNotFound       = errors.New("recharge order not found")
	ErrOrderNotOwned       = errors.New("recharge order not owned")
	ErrVideoNotTippable    = errors.New("video is not tippable")
	ErrNotifyInvalid       = errors.New("payment notify invalid")
	ErrInvalidPayMethod    = errors.New("invalid pay method")
	ErrStripeNotConfigured = errors.New("stripe is not configured")
)
