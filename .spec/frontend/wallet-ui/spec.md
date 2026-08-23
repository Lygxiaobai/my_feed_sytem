---
title: wallet-ui
status: active
code:
  - frontend/src/views/WalletView.vue
related:
  - frontend/src/api/wallet.ts
  - frontend/src/views/AccountView.vue
  - frontend/src/views/HomeView.vue
  - frontend/src/views/VideoDetailView.vue
  - frontend/src/views/CheckinView.vue
  - frontend/src/views/LotteryView.vue
  - frontend/src/components/TipSheet.vue
  - frontend/src/router/index.ts
---
# wallet-ui

## raw source
A signed-in user opens a wallet from the account hub or the desktop top-bar 充积分 entry, sees spendable coins and soon-to-expire amounts, reads a ledger, and starts a recharge via Stripe Checkout. Feed and video pages can send a tip or open that video's tip list. Dedicated pages cover daily check-in and lottery.

## expanded spec
The wallet page uses coins for balances and the ledger, and yuan only on the recharge controls. Starting a recharge creates a Stripe Checkout session and redirects there; the page can reopen that URL. Returning from Checkout with an order number queries the server; credit never comes from the redirect itself. A cancel return (`canceled=1`) or a still-pending order is shown as unpaid so the processing banner does not stay. A paid query offers to open the invoice surface. Recharge failures show the server message. The four internal sources are not listed as separate balances. Invoices are opened from this page onto the surface owned by `invoice-ui`.

Tipping is offered only on another person's public video from the feed or video page. Preset amounts are shown as yuan with their coin equivalents, and a custom amount is entered in coins. Any signed-in user can open that video's tip list: the author sees every tip under the title 「本视频打赏」, and a non-author sees only their own tips under 「我的打赏」. An empty author list says nobody has tipped the video; an empty viewer list says the viewer has not tipped it. Authors still cannot tip their own video.

Check-in and lottery each have a signed-in page. Check-in shows this Beijing month as a Monday–Sunday calendar, marks claimed days with that day's coins, and after a successful claim immediately shows 「今日获得 X 积分」 and refreshes the calendar. The claim button is disabled once today is claimed. Lottery shows a six-sector wheel whose labels match the backend tiers (0 / 2 / 5 / 10 / 20 / 50 coins at 50% / 25% / 12% / 8% / 4% / 1%). The draw control sits in the hub so it does not rotate with the sectors. A click calls the server first, then spins to the returned `prize_index`; an already-claimed day shows the server error and does not spin.
