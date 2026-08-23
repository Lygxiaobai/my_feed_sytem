---
scenarios:
  - name: wallet-opens-invoice-surface
    description: A signed-in user opens the wallet and then the invoice page.
    expected: The invoice page shows a personal header form, eligible paid orders, and the user's invoices. There is no company toggle. An empty eligible list mentions finishing a Stripe test recharge first. A signed-out visitor is sent to the account page.
    tags:
      - frontend-e2e
      - desktop
  - name: personal-apply-shows-receipt
    description: A signed-in user applies a personal header to an eligible paid recharge and confirms.
    expected: The page reports an issued site receipt with the frozen yuan amount. The same order leaves the eligible list.
    tags:
      - frontend-e2e
      - desktop
