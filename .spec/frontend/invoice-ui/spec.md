---
title: invoice-ui
status: active
code:
  - frontend/src/views/InvoiceView.vue
related:
  - frontend/src/api/invoice.ts
  - frontend/src/views/WalletView.vue
  - frontend/src/router/index.ts
---
# invoice-ui

## raw source
A signed-in user opens invoices from the wallet, saves a personal header, applies against a paid recharge, and reads the issued receipt.

## expanded spec
The invoice page is a separate surface at `/invoice`, reached from the wallet. Signed-out visitors are sent to the account page. The page states that the issue is a site receipt for a personal header, not a government VAT invoice, that company applications are not offered, and that applying never credits coins. In the test environment it tells the user a paid Stripe test checkout is enough; Stripe's own Invoices product is not required.

The user can save a personal header and see paid recharges that are still eligible. An empty eligible list tells the user to finish a Stripe test recharge first. Applying asks for confirmation and then shows the issued receipt, which can be opened and printed. The administration workbench can list and open issued receipts. It still has no issue or reject queue.
