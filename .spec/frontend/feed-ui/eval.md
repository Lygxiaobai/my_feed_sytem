---
scenarios:
  - name: feed-cards-hide-stored-tags
    description: A viewer watches a recommendation or hot-ranking card for a video that has stored tags.
    expected: The playback overlay and list card do not show those tags. A video with no stored tags also shows no tag chips and does not invent labels from the title.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: feed-mode-navigation
    description: A user can switch between recommendation, likes-count, and following feed modes and load the next page.
    expected: The selected mode, loading state, exclude-id or cursor, and returned cards stay consistent across navigation.
    tags:
      - frontend-e2e
      - desktop
  - name: hot-feed-navigation
    description: A user can open the hot-ranking view and load the next page of popularity-ranked videos.
    expected: The ranking snapshot and pagination state remain consistent while the view loads more results.
    tags:
      - frontend-e2e
      - desktop
  - name: feed-api-error
    description: A failed feed request is visible to the user and does not render as an empty successful feed.
    expected: The page shows an error state with a retry path and preserves the selected feed mode.
    tags:
      - frontend-e2e
      - desktop
  - name: feed-has-no-detail-chip
    description: A user views the recommendation feed chrome.
    expected: The playback surface has no feed-mode tabs and no mute or danmaku chip above the video; those two controls sit with the danmaku composer, tipping stays on the video action rail, and on desktop the stage is a dark rounded frame rather than an edge-to-edge card.
    tags:
      - frontend-e2e
      - desktop
  - name: hot-card-has-no-play-url-or-detail-chip
    description: A user views a video row on the hot-ranking list.
    expected: The row does not expose a playback-URL control or a separate details/comments chip; the cover and title still open the video.
    tags:
      - frontend-e2e
      - desktop
  - name: feed-playback-navigation
    description: A user can move between full-screen feed items using scroll or keyboard navigation.
    expected: The active item follows visibility, old items pause, only nearby items keep media sources enabled, and a single tap does not fire together with a double-tap like.
    tags:
      - frontend-e2e
      - desktop
      - mobile
