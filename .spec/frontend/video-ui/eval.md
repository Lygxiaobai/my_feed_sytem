---
scenarios:
  - name: video-detail-navigation
    description: A user can open a video from a feed card and return without losing the route context.
    expected: The detail view shows the selected video or an explicit missing/error state. Back navigation returns to the surface that opened the video, such as the account hub or the recommendation feed; it does not always say 返回推荐.
    tags:
      - frontend-e2e
      - desktop
  - name: video-detail-back-from-account
    description: A signed-in author opens one of their works from the account hub.
    expected: The detail back control says 返回账号, not 返回推荐. Following it returns to the account hub, including any works filter that was open.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-detail-resumes-unfinished
    description: A signed-in user reopens a detail page for a video they left unfinished.
    expected: The player seeks to the stored position after metadata is ready, and a completed or near-end video starts from the beginning.
    tags:
      - frontend-e2e
      - desktop
  - name: video-playback-lifecycle
    description: The active feed or detail video can autoplay muted, pause when inactive, and expose loading, buffering, failure, and retry states.
    expected: Only the active feed video plays, immediate neighbors may preload, failed playback is visible and recoverable, and leaving the view releases playback resources.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-playback-time-and-progress
    description: A user watches an active feed or detail video and uses the progress bar.
    expected: The player shows current time and total duration as mm:ss / mm:ss. The bar follows playback, and tapping or dragging it seeks to that offset so overlays such as danmaku stay aligned.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-upload-processing
    description: A signed-in user selects or drops one video. Upload starts immediately while they edit the title and description, then they publish after the server transcodes it.
    expected: Upload starts on accept without waiting for the publish click, does not copy the file name into the title, reports real byte-level progress across the whole file that stays below 100% until the server accepts the complete file, keeps the bar moving while several parts upload in parallel, sends those parts to a dedicated upload origin when the session names one, shows a confirming state only after the whole file has left the browser, times out instead of staying on a frozen percentage, keeps title and description editable during upload and processing, shows processing and publishing as distinct in-progress states in user terms, polls only the account-owned task, publishes only after ready URLs exist, and shows a processing failure as recoverable without requiring a new file.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-upload-progress-waits-for-ack
    description: A signed-in user uploads a video and watches the progress bar before the server responds.
    expected: The bar follows sent bytes but does not show 100% before the upload request completes. After the browser finishes sending, the UI shows a confirming state in user terms, then switches to processing only after the server accepts the file.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-upload-rejects-invalid-file
    description: A user selects or drops a non-video file or a video above the server size limit.
    expected: The file is rejected at selection or drop time with the reason shown, no upload request is issued, and the publish action stays unavailable until an acceptable file is chosen.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-upload-cancel
    description: A user cancels while the file is uploading or while the server is still processing it.
    expected: The in-flight request and any polling stop immediately, the form stays editable with the file still selected, and cancellation is not reported as a failure.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-publish-waits-for-ready
    description: A user clicks publish before upload or processing has finished.
    expected: The click does not start a second upload. The workflow waits for ready playable URLs, then publishes once using the title, description, and tags current at send time.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-publish-offers-inferred-tags
    description: A signed-in user fills the title and description, including a hashtag in one of them.
    expected: Suggested `#` chips appear under the tag field from those fields. Clicking a chip adds it to the selected tags. The author can still type a tag. Publishing without a selection stores the inferred tags.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-publish-accepts-tags
    description: A signed-in user adds up to seven tags in the publish composer and publishes.
    expected: The composer shows a single tag field, refuses an eighth tag, offers inferred chips from the title and description, sends the selected tags with publish, and the completion state shows those tags. If the user selects none, the published video uses the inferred tags.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-surfaces-show-stored-tags
    description: A viewer opens a published video that has stored tags.
    expected: The detail page and feed card show those tags. A video with an empty tag list shows no tag chips.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-draft-saves-ready-media
    description: A signed-in user finishes processing a video, edits the title, leaves without publishing, then opens drafts under 作品.
    expected: The video appears under the 草稿 pill with the typed title, not in public works, private works, or any public surface. Opening it returns to the composer with the same media and fields. Publishing from there does not upload the file again.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-author-unpublish-and-delete
    description: The author opens more on their own detail page, unpublishes the public video, then deletes it after confirming. Share, the caption overlay, and work cards have no unpublish or delete buttons.
    expected: Unpublish immediately stops the public player and feed card for others. On the author's studio the work leaves 作品 and appears under 私密作品. Delete asks for confirmation, then the work disappears from every studio list and reopening the detail shows the missing state.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: video-publish-has-no-cover-control
    description: A user completes the publish workflow end to end.
    expected: No cover is selected, uploaded, previewed, or confirmed at any point, and the published video shows the server-generated first-frame poster.
    tags:
      - frontend-e2e
      - desktop
      - mobile
