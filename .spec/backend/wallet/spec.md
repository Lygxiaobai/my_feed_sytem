---
title: wallet
status: active
code:
  - backend/internal/wallet/service.go
related:
  - backend/internal/wallet/handler.go
  - backend/internal/wallet/entity.go
  - backend/internal/wallet/rules.go
  - backend/internal/wallet/repo.go
  - backend/internal/wallet/stripe.go
  - backend/internal/wallet/expire.go
  - backend/internal/account/repo.go
  - backend/internal/account/service.go
---
# wallet

## raw source
Accounts hold integer coins across four internal sources: check-in/lottery grants that expire, recharge principal and recharge bonuses that never expire, a one-time registration gift that expires, and received tips that expire. Coins can be spent only to tip a public video. Recharge uses Stripe Checkout only.

## expanded spec
The wallet never shows the four sources to the caller of summary. Summary reports currently spendable coins, coins that expire within three days, and the nearest expiry. Spend deducts lots with the soonest expiry first and uses non-expiring recharge last. Expired remaining amounts become zero and appear as expire ledger rows.

A newly created account receives ten coins once, in the same transaction as account creation. Existing accounts are not backfilled. Check-in and lottery each succeed at most once per Beijing calendar day. Check-in draws 1–20 coins: 95% of draws are uniformly 1–10 and 5% are uniformly 11–20, so P(coins > 10) = 0.05. The drawn amount is stored on that day's `DailyAction.Prize` and granted into the check-in source. `POST /wallet/checkin/month` returns the current Beijing year and month, today's date, whether today is already claimed, and each claimed day as `{biz_date, coins}`. Lottery is free and draws one of six distinct prize tiers, returned as `{coins, prize_index}` where `prize_index` is 0–5 in this order: 0 coins at 50%, 2 at 25%, 5 at 12%, 10 at 8%, 20 at 4%, and 50 at 1%. Those grants expire in fifteen days. The registration gift expires in thirty days. Received tips expire in ninety days.

One yuan buys ten coins. Packaged recharges of 6, 30, 68, and 128 yuan also grant 0, 30, 100, and 250 bonus coins into the same non-expiring recharge source. A custom whole-yuan amount of at least one yuan buys coins at the same rate with no bonus. Only one unpaid recharge order exists at a time; creating another closes the previous unpaid order. Unpaid orders close after thirty minutes. Creating a recharge always opens Stripe Checkout. The cancel URL includes `canceled=1` so the wallet can tell a back-out from a paid return; that flag never credits coins. Stripe test mode charges `yuan` dollars (6 yuan → $6.00) and still credits the order's yuan in coins. Credit happens only after a verified Stripe webhook or a server-side Checkout Session query reports success, and never from the browser return alone. The credited amount is the order's yuan, not the provider's fee settlement. Missing Stripe configuration makes recharge unavailable. Other wallet actions keep working. A request that asks for another payment method is rejected.

Stripe webhooks answer with the plain text `ok` or `fail` and do not use the API envelope. Secrets stay in the environment.

Stripe Invoices may be enabled on the Stripe account. Recharge still uses Checkout Sessions only: creating an unpaid order does not create an invoice, and `invoice.paid` or a hosted invoice URL must not credit coins. Site invoices for paid recharge are owned by `invoice/spec.md` and are not a fourth `method` on the unpaid recharge order.

A tip requires at least ten coins, a publicly approved video, and a viewer who is not the author. The same transaction writes a tip notice for the author; that inbox row is owned by `notification/spec.md`. The whole tip fails when the balance is too low. The same viewer cannot tip the same video again within ten seconds. Thirty percent of the tipped coins go to a platform ledger the user cannot see; the author receives the rest after integer truncation toward the author being rounded down. Deleted videos do not refund unspent received tips. Popularity increases by the coins the tipper spent.

Listing tips for a video is authenticated. The author receives every tip on that video. A signed-in non-author receives only rows where `from_account_id` is themselves; if they have never tipped that video the list is empty and is not a 403. Unsigned callers keep the existing authentication requirement.
