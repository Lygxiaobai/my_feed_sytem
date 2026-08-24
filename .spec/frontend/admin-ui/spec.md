---
title: admin-ui
status: active
code:
  - frontend/src/views/admin/AdminShell.vue
related:
  - frontend/src/views/admin/AdminOverviewView.vue
  - frontend/src/views/admin/AdminReportsView.vue
  - frontend/src/views/admin/AdminVideosView.vue
  - frontend/src/views/admin/AdminUsersView.vue
  - frontend/src/views/admin/AdminInvoicesView.vue
  - frontend/src/views/admin/AdminPaymentsView.vue
  - frontend/src/views/admin/AdminBalancesView.vue
  - frontend/src/views/admin/AdminOpsView.vue
  - frontend/src/components/VideoTags.vue
  - frontend/src/api/admin.ts
  - frontend/src/api/report.ts
  - frontend/src/views/AccountView.vue
  - frontend/src/router/index.ts
  - frontend/src/App.vue
---
# admin-ui

## raw source
The web application gives reviewer accounts a separate administration workbench for the report queue, a video board, a user board, issued invoices, recharge orders, spendable balances, and read-only operations.

## expanded spec
The administration surface is reached from the signed-in account hub, not from the primary consumer navigation. Visitors without reviewer access do not see the entry and cannot stay on the route. The workbench uses its own chrome: an overview, a report queue, a video board, a user board, an invoice board, a payment board, a balance board, and operations. It does not reuse the consumer shell. Operations behavior is owned by `ops-ui`.

The invoice board lists issued site receipts and opens one receipt. The payment board lists recharge orders in every payment state with paid and pending totals. The balance board lists spendable coins and remaining lots. None of those boards offers an issue, reject, credit, or balance-edit control.

The report queue shows outstanding notices grouped by video, with reason totals and sample explanations, then lets the reviewer dismiss the notices or remove the video. Removal from the queue or from the video board asks for a written reason and a confirmation. The video board lists works in every review state and shows each work's stored tags in a dedicated tag column, then still opens one item for playback or takedown. The user board lists every account and shows the stored interest tags written by likes, tips, and comments; opening one author still shows that author's works with those works' stored tags in a dedicated tag column. An identifier, username, or email still opens that author's works in every review state. Share-code paste recognition is disabled on this surface so it cannot hijack an operator typing an identifier.
