---
title: analytics-ui
status: active
code:
  - frontend/src/analytics/track.ts
related:
  - frontend/src/analytics/watch.ts
  - frontend/src/main.ts
  - frontend/src/components/AppShell.vue
  - frontend/src/components/VideoPlayer.vue
  - frontend/src/components/TipSheet.vue
  - frontend/src/components/ReportSheet.vue
  - frontend/src/components/DanmakuLayer.vue
  - frontend/src/components/MessagePanel.vue
  - frontend/src/views/HomeView.vue
  - frontend/src/views/VideoDetailView.vue
  - frontend/src/views/VideoView.vue
  - frontend/src/views/AccountView.vue
  - frontend/src/views/RegisterView.vue
  - frontend/src/views/SettingsView.vue
  - frontend/src/views/WalletView.vue
  - frontend/src/views/CheckinView.vue
  - frontend/src/views/LotteryView.vue
  - frontend/src/router/index.ts
---
# analytics-ui

## raw source
The web application records product behaviors that explain how people move through feed, playback, publishing, search, account, wallet, reporting, danmaku, and private-message flows.

## expanded spec
A single client helper owns queueing, visitor identity, and delivery. Views do not call the report endpoint themselves. Failed delivery never changes the user-visible result of the action that produced the event.

Route changes emit `page_view`. Search, login, register, logout, publish, like, unlike, follow, unfollow, comment submit, paid recharge, tip, check-in, lottery, report submit, danmaku send, and private-message send emit only after the corresponding user action succeeds. Creating a recharge checkout is not a recharge event; `wallet_recharge` waits until the server marks the order paid. Lottery is recorded when the draw API succeeds, not when the wheel animation ends. Property bags carry amounts, identifiers, and reason codes, never passwords, message text, danmaku text, or report explanations. Feed and detail playback emit `video_play` when playback actually starts and `video_watch` when the user leaves that video, including watch duration when the player can provide it.

The helper batches events and flushes on a short delay, when the batch is full, or when the page is hidden, so playback does not create one request per tick. The visitor identifier is created locally and reused across sessions on the same browser; it is not a login credential.
