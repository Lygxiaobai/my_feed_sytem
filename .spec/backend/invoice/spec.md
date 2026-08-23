---
title: invoice
status: active
code:
  - backend/internal/invoice/service.go
related:
  - backend/internal/invoice/handler.go
  - backend/internal/invoice/entity.go
  - backend/internal/invoice/rules.go
  - backend/internal/invoice/repo.go
  - backend/internal/invoice/wallet_orders.go
  - backend/internal/wallet/service.go
  - backend/internal/http/router.go
---
# invoice

## raw source
A signed-in account can save a personal invoice header and apply for one invoice per paid recharge. A successful application is issued immediately as a site receipt. Issuing an invoice never credits coins. Company or VAT applications are refused.

## expanded spec
Invoice is a post-payment document, not a payment method and not a fourth unpaid-order `method`. The wallet still credits coins only from a verified Stripe payment, never from `invoice.paid`, a hosted invoice URL, or this leaf. Amounts on an invoice are copied from the paid recharge at apply time and do not change afterward.

An application is accepted only for a recharge the caller owns that is already `paid`. Missing, unpaid, closed, or another account's orders are answered as not found so the apply path cannot enumerate orders. Each paid recharge has at most one issued invoice. An issued invoice cannot be applied again.

The header is personal only. A title of two to eighty visible characters and a reachable email are required. The application becomes `issued` in the same write. A company header is refused. There is no reviewer queue, issue action, or reject action.

Saving a profile stores the default header for later apply. Apply also upserts that profile. Listing eligible orders returns paid recharges that have no issued invoice. A caller may list and read only their own invoices.

This leaf does not call Stripe Invoices, does not create a Checkout invoice, and does not treat a Stripe receipt URL as a credit signal. The document it issues is a site receipt, not a government-issued VAT invoice.
