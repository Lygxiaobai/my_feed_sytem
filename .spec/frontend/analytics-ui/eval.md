---
scenarios:
  - name: page-view-is-recorded-on-navigation
    description: A user opens the site and then moves from the recommendation feed to the hot ranking.
    expected: Two page_view events are reported, one for each destination, and the UI navigation is unchanged.
    tags:
      - frontend-e2e
      - desktop
  - name: successful-like-is-recorded
    description: A logged-in user likes a video from the feed.
    expected: A video_like event is reported after the like request succeeds; a failed like does not record the event and still shows the previous state.
    tags:
      - frontend-e2e
      - desktop
  - name: watch-duration-is-recorded-on-leave
    description: A user plays a feed video and then scrolls to the next item.
    expected: The first video emits video_play when playback starts and video_watch when it is left, carrying the elapsed watch time when available.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: tracking-failure-is-invisible
    description: The report endpoint is unavailable while the user publishes a comment.
    expected: The comment still appears in the open drawer and the user is not shown a tracking error.
    tags:
      - frontend-e2e
      - desktop
  - name: successful-wallet-events-are-recorded
    description: A signed-in user completes a tip, a paid recharge, a check-in, and a lottery draw.
    expected: wallet_tip, wallet_recharge, wallet_checkin, and wallet_lottery are reported only after those APIs succeed. Creating a recharge checkout does not emit wallet_recharge. Failed tip or claim requests do not record the event.
    tags:
      - frontend-e2e
      - desktop
  - name: successful-report-danmaku-and-dm-are-recorded
    description: A signed-in user submits a report, sends a danmaku, and sends a private message.
    expected: report_submit, danmaku_send, and dm_send are reported after those APIs succeed, without the report explanation or message text. A failed send does not record the event.
    tags:
      - frontend-e2e
      - desktop
      - mobile
