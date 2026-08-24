---
scenarios:
  - name: account-opens-wallet
    description: A signed-in user opens the wallet from the account hub.
    expected: The wallet shows spendable coins and a recharge surface; a signed-out visitor is sent to the account page.
    tags:
      - frontend-e2e
      - desktop
  - name: wallet-recharge-opens-stripe-checkout
    description: A signed-in user starts a packaged recharge.
    expected: The wallet creates a Stripe Checkout session and redirects there. Success or closed comes from the server query, not from a browser redirect. A paid query offers to open the invoice surface. An unpaid or canceled return shows that payment was not completed and does not keep 「支付处理中」. There is no Alipay choice.
    tags:
      - frontend-e2e
      - desktop
  - name: feed-tip
    description: A signed-in viewer opens the feed or a video page and confirms a tip on another person's public video.
    expected: A successful tip shows a confirmation and a failure shows the server message without changing another viewer's video.
    tags:
      - frontend-e2e
      - desktop
  - name: feed-tip-custom-coins
    description: A signed-in viewer opens the tip sheet and types a custom amount whose first digit is below 10, such as 15 or 50.
    expected: The digits stay in the field, the author-receives line follows 70% of that amount, and confirm spends the typed coins rather than the highlighted preset.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: wallet-checkin-lottery
    description: A signed-in user opens the check-in and lottery pages from the account hub or wallet.
    expected: Check-in shows this month's Monday–Sunday calendar with claimed-day coins, claims 1–20 coins once per Beijing day, and then shows today's prize. Lottery shows the six published tiers with the draw control in the hub, spins to the server prize_index after a successful draw, and does not spin when already claimed.
    tags:
      - frontend-e2e
      - desktop
  - name: author-sees-video-tips
    description: The author opens their own video and opens the tip list.
    expected: The sheet title is 「本视频打赏」 and the list shows every tipper name and coin amount for that video.
    tags:
      - frontend-e2e
      - desktop
  - name: viewer-sees-own-video-tips
    description: A signed-in viewer who has tipped a video opens the tip list from the feed or video page.
    expected: The sheet title is 「我的打赏」 and only that viewer's tips on the video appear; a viewer who has not tipped sees an empty own-tip list rather than a forbidden error.
    tags:
      - frontend-e2e
      - desktop
