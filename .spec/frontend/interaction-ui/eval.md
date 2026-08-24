---
scenarios:
  - name: video-interactions
    description: A user can like, comment, follow, unfollow, tip, and open that video's tip list from the supported video surfaces.
    expected: Each successful action updates the visible state and each failure is shown without a false success state. Authors see every tip; signed-in viewers see only their own.
    tags:
      - frontend-e2e
      - desktop
  - name: comment-authors-show-identity-avatar
    description: A user opens comments on a video that already has a root comment and a reply.
    expected: Each row shows that author's identity avatar beside the name, replies use a smaller avatar, and opening the avatar or name lands on that author's public profile.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: interaction-refresh
    description: A successful interaction remains consistent after navigating away and returning to the video.
    expected: The returned view reflects the server state rather than stale local action state.
    tags:
      - frontend-e2e
      - desktop
  - name: interaction-race-protection
    description: A user rapidly changes video context while comments or a like mutation is still settling.
    expected: A stale comment response is discarded, the like action remains pending until the transactional API response or failure, and the final visible state is not replaced by an older request.
    tags:
      - frontend-e2e
      - desktop
  - name: share-passage-is-recognized-without-search
    description: A user shares a video, copies the passage, and pastes it on the page in a fresh session, including outside the search field.
    expected: The passage contains the code, author, title, and a link built from the current origin. Pasting it opens the shared video and does not run a text search or leave the passage in the search field.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: clipboard-share-is-offered-on-return
    description: A user copies a share passage elsewhere and then focuses the site in a session that can read the clipboard.
    expected: The site offers the shared video as a recognition card without opening search. Dismissing the card does not navigate. Opening it lands on the video. When the clipboard cannot be read, an explicit paste still opens the video.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: share-copy-degrades-without-a-clipboard
    description: A user shares a video from the entry point where the clipboard API is unavailable.
    expected: A manual-copy prompt containing the full passage appears instead of a failure, and the code stays readable on screen.
    tags:
      - frontend-e2e
      - desktop
  - name: unresolvable-paste-is-reported-but-a-search-term-is-not-swallowed
    description: A user pastes a share passage whose video was removed, then separately searches for an eight-character word.
    expected: The first reports that the code is invalid instead of searching for the passage; the second performs an ordinary search.
    tags:
      - frontend-e2e
      - desktop
  - name: share-link-opens-directly
    description: A user opens a share link in the address bar and then presses back.
    expected: The link lands on the video, and back returns to where the user came from rather than to the intermediate resolving step.
    tags:
      - frontend-e2e
      - desktop
  - name: share-sheet-has-no-author-manage
    description: The author of a video opens share on the detail page.
    expected: The share sheet only offers copy-code and copy-link. It has no unpublish, relist, or delete controls.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: reporting-sets-expectations-and-is-not-offered-on-own-content
    description: A viewer reports another user's video, submits the catch-all reason without an explanation, then reports the same video again; the author checks their own video.
    expected: The catch-all reason cannot be submitted without an explanation, a successful submission says a person will review it and that the content is not removed immediately, the repeat submission is refused as already reported, the video stays visible throughout, and the author is offered no report control on their own video.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: danmaku-flies-with-playback
    description: A signed-in user turns danmaku on, watches a video that already has timed items, joins after those offsets, and sends a short one at the current moment.
    expected: Items still crossing the screen appear without dumping earlier history, the new text flies immediately, a failed send removes the local item and shows the server message, and turning danmaku off hides the flying overlay but leaves the composer so it can be turned back on.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: playback-surface-has-no-instructional-overlay
    description: A user views a feed video and a video detail page, then toggles mute.
    expected: No gesture or keyboard-shortcut hints are drawn over the video, mute produces no transient message because the control's own label shows the state, and the underlying gestures and shortcuts still work.
    tags:
      - frontend-e2e
      - desktop
      - mobile
