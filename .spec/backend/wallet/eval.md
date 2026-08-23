---
scenarios:
  - name: register-gift-once
    description: Creating a new account grants a registration gift.
    expected: The new account has ten spendable coins and a later grant attempt does not add another ten.
    tags:
      - backend-api
  - name: spend-expiring-first
    description: A wallet with soon-to-expire, later-expiring, and recharge coins spends fifteen coins.
    expected: The soonest-expiring lot is emptied first, the later-expiring lot is reduced next, and recharge is untouched.
    tags:
      - backend-api
  - name: checkin-once-per-day
    description: An account checks in twice on the same Beijing calendar day and once after midnight.
    expected: The second same-day check-in is rejected as already claimed and the next-day check-in succeeds.
    tags:
      - backend-api
  - name: checkin-random-prize
    description: An account checks in with a controlled draw bucket.
    expected: The granted coins are between 1 and 20, DailyAction.Prize stores that amount, and P(coins > 10) is 0.05 under the 0–999 mapping.
    tags:
      - backend-api
  - name: checkin-month-beijing
    description: An account lists this Beijing month's check-ins.
    expected: The payload reports year, month, today, claimed_today, and only this month's check-in days with their coins.
    tags:
      - backend-api
  - name: lottery-six-tiers
    description: Lottery draws use the six published tiers and return prize_index 0–5.
    expected: Coins match the table 0/2/5/10/20/50 with probabilities 50/25/12/8/4/1, and a zero prize still writes a lottery ledger row.
    tags:
      - backend-api
  - name: video-tips-author-and-viewer
    description: The author and two viewers list tips on the same video; a third signed-in account has never tipped.
    expected: The author sees every tip, each viewer sees only their own rows, and the third account gets an empty list rather than a forbidden error.
    tags:
      - backend-api
  - name: tip-cut-and-rules
    description: A viewer tips an approved video ten coins, then immediately tips again, and the author tries to tip that video.
    expected: The author receives seven, the platform records three, the rapid second tip is rejected, and self-tip is rejected.
    tags:
      - backend-api
  - name: recharge-credit-once
    description: A paid recharge notification is applied twice for the same order.
    expected: Coins are granted once and the order stays paid.
    tags:
      - backend-api
  - name: recharge-stripe-checkout
    description: Creating a recharge with no method or method=stripe.
    expected: The API returns a Stripe Checkout URL and credits only after a paid session query or verified webhook. A request for another payment method is rejected.
    tags:
      - backend-api
