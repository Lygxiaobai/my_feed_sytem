---
scenarios:
  - name: publish-stores-tags
    description: An authenticated user publishes a video with author tags and a hashtag in the title, then publishes another with no tags and only a hashtag.
    expected: The first video stores only the author-selected tags. The second stores inferred tags from the title or description, including `#话题` tokens and short leftover phrases. A long sentence is not stored as a tag.
    tags:
      - backend-api
  - name: publish-video
    description: An authenticated user uploads a video, waits for media processing, and publishes one video.
    expected: The upload task reaches ready with a playable MP4 and generated poster before publish; a source that is already browser-playable H.264/AAC is remuxed rather than re-encoded; the file is accepted after an upload session plus independently numbered parts that may arrive in parallel and only creates one media task after all parts are stored; the publish response identifies the video and the operation is not duplicated by a repeated idempotency key.
    tags:
      - backend-api
  - name: upload-parts-use-dedicated-origin
    description: An authenticated user starts an upload while a dedicated upload origin is configured.
    expected: The session names that origin and the larger direct part size so subsequent part bytes go there instead of the site CDN proxy. When the origin is unset, parts stay on the page origin with the Cloudflare-safe part size.
    tags:
      - backend-api
  - name: upload-retry-replaces-abandoned-session
    description: An authenticated user starts an upload, the transfer is abandoned, and they start another upload.
    expected: The new session is created instead of being rejected as an unfinished upload, and the abandoned parts are discarded.
    tags:
      - backend-api
  - name: video-writes-are-rate-limited
    description: One account uploads or publishes faster than the write ceiling.
    expected: Later write attempts are rejected with the rate-limited outcome and do not create another media task or video.
    tags:
      - backend-api
  - name: media-task-failure
    description: An uploaded file that ffmpeg cannot decode is processed by the Worker.
    expected: The task reaches failed with a bounded error message, no raw source path is returned, and the file cannot be published as a playable video.
    tags:
      - backend-api
      - worker
  - name: video-detail-cache-fallback
    description: A video detail remains readable when the detail cache is unavailable.
    expected: The API returns the persisted video detail through the database fallback.
    tags:
      - backend-api
  - name: published-content-is-not-public-until-approved
    description: A user publishes a video and immediately checks every public listing.
    expected: The publish call succeeds, the video appears in the author's own view marked as awaiting review, and it appears in no public listing until a review approves it.
    tags:
      - backend-api
  - name: disallowed-text-is-rejected-including-evaded-spellings
    description: A user publishes titles containing a disallowed term, once plainly and once with inserted spacing, punctuation, and full-width characters.
    expected: Both are rejected, neither reaches any public listing, and the author is told only that the content did not pass, with no indication of what matched.
    tags:
      - backend-api
      - worker
  - name: undecidable-content-goes-to-a-human
    description: Content that the machine cannot decide is published, and separately the moderation path fails outright.
    expected: Both end up awaiting human review rather than approved, so a failure in the review path never becomes a publication channel.
    tags:
      - backend-api
      - worker
  - name: approval-publishes-through-one-path
    description: One video is approved by machine and another by a human reviewer.
    expected: Both become visible in the public listings and both count toward popularity, so neither decision route can expose content the other would not.
    tags:
      - backend-api
      - worker
  - name: unapproved-content-is-indistinguishable-from-missing
    description: A non-author requests an unapproved video directly and through every listing that can surface identifiers from a derived index.
    expected: Every route answers as though the video does not exist, and no derived index causes it to surface.
    tags:
      - backend-api
  - name: review-queue-strands-nothing
    description: A video's review event never reaches the worker.
    expected: The video still appears in the human review queue rather than remaining invisible to both the public and the reviewers.
    tags:
      - backend-api
  - name: review-actions-are-restricted-and-recorded
    description: A non-reviewer attempts a review action, then a reviewer decides the same item twice.
    expected: The non-reviewer is refused, the second decision is rejected as already handled, and every state change is recorded with its cause and operator.
    tags:
      - backend-api
  - name: share-code-round-trips
    description: A share code is issued for a video and then resolved, both alone and embedded in the surrounding text a user would paste.
    expected: Every form resolves to the same video the code was issued for.
    tags:
      - backend-api
  - name: damaged-share-code-is-refused-not-misresolved
    description: A share code is truncated, extended, and altered by one character, then each variant is resolved.
    expected: Every variant is refused as invalid; none resolves to a different video.
    tags:
      - backend-api
  - name: ordinary-text-is-not-mistaken-for-a-code
    description: Plain search text containing eight-character words is submitted for resolution.
    expected: No video is resolved, so pasting ordinary text cannot redirect a viewer to an unrelated video.
    tags:
      - backend-api
  - name: share-code-grants-no-access
    description: A code is issued for a video that is later removed, and a non-author resolves it; separately a non-author requests a code for an unapproved video.
    expected: Both answer as though the content does not exist, and the failure is indistinguishable from an unrecognized code.
    tags:
      - backend-api
  - name: unfinished-compose-becomes-a-draft
    description: An authenticated user uploads playable media and leaves without publishing, then opens the draft box.
    expected: One draft exists for that media, it is missing from public listings and from other viewers' author pages, and publishing it later reuses the same video id.
    tags:
      - backend-api
  - name: author-unpublish-and-delete-are-not-moderation
    description: The author unpublishes an approved video, a stranger requests it, the author relists it, then deletes it.
    expected: Unpublish hides it from every public surface without writing a rejected review state. Relist makes the approved video public again. Delete answers as missing to the author and to strangers, and the row remains for audit.
    tags:
      - backend-api
  - name: pre-existing-content-survives-the-rollout
    description: The review feature is introduced into a system that already contains published videos.
    expected: Existing content stays publicly visible instead of disappearing behind a default awaiting-review state.
    tags:
      - backend-api
