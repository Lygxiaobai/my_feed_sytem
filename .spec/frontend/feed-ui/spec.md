---
title: feed-ui
status: active
code:
  - frontend/src/views/HomeView.vue
related:
  - frontend/src/api/feed.ts
  - frontend/src/api/types.ts
  - frontend/src/components/AppIcon.vue
  - frontend/src/components/FeedVideoCard.vue
  - frontend/src/components/VideoTags.vue
  - frontend/src/components/VideoPlayer.vue
  - frontend/src/views/HotView.vue
  - frontend/src/router/index.ts
  - frontend/src/stores/auth.ts
---
# feed-ui

## raw source
The web application lets a user browse recommendation, likes-count, following, and hot-ranking feeds with loading, pagination, authentication, and error states.

## expanded spec
The UI preserves the active feed mode and cursor while loading adjacent pages, renders stable video cards, shows a video's stored tags when present, and reports API failures without silently showing stale or empty content as success. The recommendation tab reads `listRecommend` and pages with the server-returned exclude-id set; the standalone hot-ranking view continues to read the popularity ranking. Authenticated-only feed modes redirect or explain the missing session state. Feed-mode switching is not drawn on the playback surface; recommendation, following, and likes-count remain reachable from the application chrome. On desktop the playback stage sits in the chrome content area as a dark, rounded frame with a short gutter; on compact screens it still goes edge to edge. The picture itself still fills that frame by cropping rather than letterboxing. The playback surface does not offer a separate detail button; comments, follow, share, tipping, a signed-in tip-list control, and the danmaku composer stay on the video. Mute and danmaku on/off sit with that composer. List cards on the hot-ranking view also omit a playback-URL control and a separate details/comments chip, because the cover and title already open the video. Any signed-in user can open that video's tip list: authors see every tip, viewers see only their own.
