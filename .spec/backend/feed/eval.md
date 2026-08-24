---
scenarios:
  - name: feed-cards-include-stored-tags
    description: A client reads latest, popularity, following, or recommendation pages that contain a video with stored tags.
    expected: Each matching card includes those tags. A video with an empty tag list omits tag chips rather than inventing labels from the title.
    tags:
      - backend-api
  - name: latest-feed-cursor
    description: A client can read the latest feed across two cursor pages without duplicate or missing video IDs.
    expected: Adjacent pages form a stable ordered sequence and the next cursor advances or ends the sequence.
    tags:
      - backend-api
  - name: following-feed-auth
    description: An authenticated user can read the following feed and cannot use another user's following scope.
    expected: Results contain only followed authors for the authenticated account and unauthenticated access is rejected.
    tags:
      - backend-api
  - name: following-feed-push-pull-merge
    description: A reader follows both a low-follower author whose video was delivered to the reader's inbox and a high-follower author whose video was never delivered.
    expected: Both videos appear in one page ordered by publish time, showing the read merged the inbox with the high-follower author's outbox.
    tags:
      - backend-api
  - name: following-feed-unfollow-residue
    description: A reader's inbox still holds a video from an author the reader no longer follows.
    expected: The video is absent from the response and no longer occupies the inbox.
    tags:
      - backend-api
  - name: following-feed-inbox-rebuild
    description: A reader whose inbox is not currently maintained opens the following feed.
    expected: The page is rebuilt from MySQL, contains every followed author's videos, and the inbox becomes maintained afterwards.
    tags:
      - backend-api
  - name: following-feed-incomplete-page-fallback
    description: A page cannot be filled and the inbox has reached its retention limit.
    expected: The response is produced from MySQL instead of reporting that no further pages exist.
    tags:
      - backend-api
  - name: popularity-snapshot
    description: A popularity feed remains stable while a client requests its next page.
    expected: The second page uses the same ranking snapshot boundary as the first page.
    tags:
      - backend-api
  - name: popularity-mysql-fallback
    description: When Redis has fewer than 10 popularity entries, cannot be read, or a first page cannot fill after playable filtering, the popularity feed returns playable videos ordered by persisted MySQL popularity.
    expected: The response is non-empty when MySQL has playable videos, returned popularity values match MySQL, and as_of is zero.
  - name: popularity-mysql-fallback-sticky-page
    description: The first popularity page fell back to MySQL and the client requests the next page with as_of zero.
    expected: The second page stays on persisted MySQL popularity even if Redis later grows past 10 entries, so adjacent pages do not switch ranking sources.
    tags:
      - backend-api
  - name: recommend-small-creator-quota
    description: A signed-in viewer with no stored interest tags requests a 10-item recommendation page while both default-matching videos and ordinary-author videos exist.
    expected: The page contains at least two videos from authors below the recommend small-creator follower threshold, unless that pool is empty.
    tags:
      - backend-api
  - name: recommend-author-window
    description: One author has many otherwise eligible videos in the candidate pool.
    expected: Adjacent items in a recommendation page do not repeat that author within a window of four, while other authors remain available.
    tags:
      - backend-api
  - name: recommend-anonymous-cold-start
    description: An unauthenticated client reads the recommendation feed.
    expected: The response is a mixed page of approved videos rather than an empty success, and no unapproved video appears.
  - name: recommend-thin-hot-window
    description: Redis has fewer than 10 recent popularity entries while MySQL still has approved playable videos.
    expected: Recommendation still returns a mixed page from MySQL candidates, and hot-queue scores stay on persisted popularity instead of the thin window.
    tags:
      - backend-api
  - name: recommend-excludes-unapproved
    description: An approved video and a pending video both exist when recommendation is requested.
    expected: Only the approved video can appear.
    tags:
      - backend-api
  - name: recommend-is-rate-limited
    description: One client IP requests recommendation pages faster than the recommendation ceiling.
    expected: Later pages are rejected with the rate-limited outcome; a latest-feed request from the same IP may still succeed if it remains under the broader feed-read ceiling.
    tags:
      - backend-api
  - name: user-interest-persisted
    description: A signed-in user likes or tips a video that already has an embedding.
    expected: user_embeddings holds one row for that account and the same model, and the account interests field holds a tag taken from that video's stored tags.
    tags:
      - backend-api
  - name: recommend-uses-stored-tags
    description: A signed-in viewer has a stored interest tag and both matching and non-matching approved videos exist.
    expected: The first page prefers videos whose stored tags match the interest tag, then fills remaining slots from the default mix so the page reaches ten items when inventory allows.
    tags:
      - backend-api
  - name: recommend-default-without-tags
    description: A signed-in viewer has an empty or NULL interests field.
    expected: The page uses the default mix rather than returning empty or an internal error, and still excludes unapproved videos.
    tags:
      - backend-api
