---
title: frontend
status: active
code:
  - frontend/src/main.ts
related:
  - frontend/src/App.vue
  - frontend/src/components/ShareInbox.vue
  - frontend/src/components/AppIcon.vue
  - frontend/src/components/AppShell.vue
  - frontend/src/router/index.ts
  - frontend/src/api/client.ts
  - frontend/src/analytics/track.ts
  - frontend/src/stores/auth.ts
  - frontend/src/stores/social.ts
  - frontend/src/stores/toast.ts
  - frontend/src/style.css
  - frontend/src/theme.ts
  - frontend/src/stores/theme.ts
  - frontend/src/views/NotificationsView.vue
  - frontend/src/views/MessagesView.vue
desc: Vue web application bootstrap and cross-view architecture boundary.
---
# frontend

## raw source
The frontend is a Vue application with a single bootstrap entry, client-side routing, Pinia stores, API modules, and behavior-facing views for feed, video, account, wallet, invoices, check-in, lottery, interactions, notifications, private messages, watch history, and a reviewer administration workbench.

## expanded spec
`frontend/src/main.ts` owns application bootstrap: it creates the Vue application, installs Pinia and the router, starts the appearance store owned by `theme-ui`, loads global styling, and mounts the root component. `frontend/src/App.vue` remains the root composition boundary and delegates page selection to the router. Share recognition is mounted here so a paste or clipboard offer survives navigation and does not depend on a particular view wrapping `AppShell`.

`frontend/src/router/index.ts` owns the URL-to-view contract. Views own user workflows, API modules own HTTP calls and response types, and Pinia stores own state shared across views. Authentication state has one client-side owner in `stores/auth.ts`; account and interaction flows must not maintain competing token state.

`api/client.ts` owns the wire contract. It unwraps the backend's response envelope in one place, so API modules and their types describe only the payload and never the envelope. A non-success business code becomes a thrown error carrying the backend's user-facing message, its business code, and the request identifier; views display that message rather than composing their own wording for server-side failures. Because unwrapping is centralized, adding or changing an endpoint does not require each caller to re-implement success detection.

`components/AppShell.vue` owns the navigation surface. The desktop chrome follows a Douyin-like frame: a full-width top bar, a left sidebar of icon-and-label destinations, and a compact bottom navigation on small screens. There is no overflow menu duplicating those destinations. The top bar hosts the brand, a single capsule search field, utility entries (充积分, notifications, messages), a 投稿 control, and the account avatar. Appearance is owned by `theme-ui` and sits in the sidebar footer on desktop, or as a bottom-left control above the tab bar on small screens — not in the top bar. Entries that already have an API open that page or panel. The notifications bell opens the inbox owned by `notification-ui`. The messages control opens the private-chat dropdown owned by `message-ui`. Signed-out visitors who hit an authenticated utility are sent to the account page. The compact "mine" destination still resolves to the account hub. The chrome shows no routing or diagnostic identifiers.

The child nodes own the detailed feed, video, account, wallet, invoice, interaction, notification, message, analytics, history, appearance, and administration contracts. This parent spec owns application startup, routing, cross-view state boundaries, and the rule that capability behavior remains in its owning child node.

## change rules
Adding or changing a route requires checking the relevant UI spec and its `eval.md`. Changing bootstrap, router installation, global state ownership, or authentication state requires checking this parent spec and every affected child UI spec. A visual refactor that preserves user-visible behavior does not require a new scenario.
