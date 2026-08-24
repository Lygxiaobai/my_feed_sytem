---
scenarios:
  - name: reviewer-can-open-admin
    description: A signed-in reviewer asks whether administration is allowed, then loads the overview.
    expected: Access is granted and the overview reports the current outstanding-notice count.
    tags:
      - backend-api
  - name: ordinary-account-cannot-administer
    description: A signed-in account that is not on the reviewer whitelist asks for access, then attempts a lookup or a takedown.
    expected: Access answers that administration is not allowed; the other calls are refused and no content or account record is returned.
    tags:
      - backend-api
  - name: test-email-is-not-admin
    description: An account bound to a digits-only test-domain email, but not on the reviewer whitelist, asks for administration or operations.
    expected: Administration and operations are both refused.
    tags:
      - backend-api
  - name: reviewer-sees-rejected-video
    description: A reviewer looks up a rejected video by identifier, then a non-author requests the public detail of the same video.
    expected: The reviewer receives the record and its rejected state. The public request answers as though the video does not exist.
    tags:
      - backend-api
  - name: admin-takedown-requires-reason-and-closes-notices
    description: A reviewer takes down an approved video that has outstanding reports, first without a reason and then with one.
    expected: The empty reason is refused and the video stays public. The reasoned request rejects the video, records the operator and reason, hides the video from non-authors including cached reads, and leaves no outstanding notice for that item.
    tags:
      - backend-api
  - name: account-lookup-is-singular-and-redacted
    description: A reviewer looks up an account by email, then retries with two identifiers at once.
    expected: The email lookup returns the public identity, the bound email, follower count, and works including unapproved ones, and never a password. The combined lookup is refused.
    tags:
      - backend-api
  - name: reviewer-can-list-videos-and-accounts
    description: A reviewer lists videos and accounts without a single-item lookup, then opens one rejected video and one account that has liked another work.
    expected: The video list includes approved and rejected rows, does not hide unapproved works, and shows each work's stored tags. The account list includes every seeded account, never a password, and the liking account shows the stored interest tag written from that work. A non-reviewer is refused.
    tags:
      - backend-api
  - name: reviewer-can-read-finance-boards
    description: A reviewer lists invoices, recharge orders, and spendable balances, then opens one invoice and one account balance.
    expected: The lists include other accounts' issued receipts, paid and unpaid orders, and remaining coins. Opening an invoice returns the frozen yuan and header. Opening a balance returns spendable coins and remaining lots. Coin totals and order statuses are unchanged.
    tags:
      - backend-api
  - name: ordinary-account-cannot-read-finance
    description: A signed-in account that is not on the reviewer whitelist asks for the invoice, payment, or balance boards.
    expected: Each call is refused and no invoice, order, or lot row is returned.
    tags:
      - backend-api
