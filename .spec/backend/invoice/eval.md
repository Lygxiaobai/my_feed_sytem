---
scenarios:
  - name: personal-apply-issues-receipt
    description: A signed-in account applies a personal header to a paid recharge they own.
    expected: The invoice is issued immediately with the order's yuan and coins frozen on the row. A second apply for the same order is refused as already issued.
    tags:
      - backend-api
  - name: company-apply-is-refused
    description: A signed-in account applies a company header to a paid recharge they own.
    expected: The apply is refused as an invalid kind and no invoice row is created.
    tags:
      - backend-api
  - name: apply-requires-owned-paid-order
    description: An account applies against a pending order of their own and against another account's paid order.
    expected: Both calls are answered as though no payable order exists. No invoice row is created.
    tags:
      - backend-api
  - name: apply-does-not-credit-wallet
    description: An account applies a personal invoice to a paid recharge that has not granted extra lots in this test fixture.
    expected: After the invoice is issued the spendable coin balance is unchanged.
    tags:
      - backend-api
