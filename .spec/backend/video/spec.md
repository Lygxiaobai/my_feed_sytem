---
title: video
status: active
code:
  - backend/internal/video/service.go
  - backend/internal/video/lifecycle.go
  - backend/internal/video/visibility.go
related:
  - backend/internal/video/handler.go
  - backend/internal/video/repo.go
  - backend/internal/video/entity.go
  - backend/internal/video/lifecycle.go
  - backend/internal/video/visibility.go
  - backend/internal/video/detail_cache.go
  - backend/internal/video/detail_cache_payload.go
  - backend/internal/video/local_detail_cache.go
  - backend/internal/video/media_validator.go
  - backend/internal/video/invalidation_consumer.go
  - backend/internal/video/sharecode.go
  - backend/internal/video/review.go
  - backend/internal/tag/tag.go
  - backend/internal/media/entity.go
  - backend/internal/media/repo.go
  - backend/internal/media/service.go
  - backend/internal/media/parts.go
  - backend/internal/media/processor.go
  - backend/internal/media/probe.go
  - backend/internal/worker/media_worker.go
  - backend/internal/audit/moderator.go
  - backend/internal/audit/entity.go
  - backend/internal/audit/service.go
  - backend/internal/video/audit_store.go
  - backend/internal/worker/audit_worker.go
---
# video

## raw source
The video subsystem validates uploads, publishes videos, serves details, exposes author and liked-video views, and issues shareable codes that resolve back to a video. Content review is optional and off by default; when enabled, published content is reviewed before it becomes publicly discoverable.

## expanded spec
Video publishing is authenticated and idempotent. A publish request may carry up to seven tags. If the author selected any tags, only those are stored. If the author selected none, inferred tags from the title and description are stored as a fallback: `#话题` tokens first, then short leftover phrases. A long sentence is not treated as a tag. The stored list is a JSON string array in a text column, the same shape as account interests. Existing videos keep an empty list. Upload and publish are rate-limited by client IP and by account so a single session cannot flood the media worker; public detail and share reads share a separate IP ceiling. A video upload first creates a multipart upload in object storage and returns one presigned URL per part; the client sends part bytes directly to object storage, so the API never carries video content. Parts may be sent in parallel and each is at least 5 MiB except the last, which the S3 multipart contract requires. A media task is created only after the client returns every part's ETag and the parts are assembled. The object key is scoped to the uploading account and a completion request carrying another account's key is rejected. Upload state lives in object storage rather than API process memory, so an upload survives an API restart, and an abandoned upload is reclaimed by storage's stale-upload expiry rather than by the API. The write ceiling counts the upload, not each part. A video upload creates an account-owned `processing` media task and returns no playable URL until the Worker has produced a standard MP4 and JPEG poster. Tasks finish as `ready` with `/static/videos/*.mp4` and `/static/covers/*.jpg` URLs or as `failed` with a bounded, user-facing error message that does not include ffmpeg, process, or path diagnostics. Those URLs address objects in the media bucket and are served by the edge directly from object storage, not by the API.

The Worker uses ffmpeg to normalize video codec/container and MIME-by-extension, enables MP4 fast start, and generates the poster. A source that is already browser-playable H.264/AAC 4:2:0 is remuxed with fast start instead of being re-encoded; any other readable source is transcoded. When a dedicated upload origin is configured, part bytes are sent there so they do not traverse the site's CDN proxy. That origin may use a non-standard HTTPS port when 80/443 on the origin address are intercepted. Raw source files remain private to the shared upload volume and are not exposed by the static resource surface. Media paths returned by the API remain usable through the static resource surface. Detail reads may use cache but must preserve the persisted video's visible fields. Publishing accepts only ready, playable media.

Review is gated by `audit.enabled` and defaults to off. When review is disabled, publishing writes an approved status and immediately enqueues the same public-release side effects (global timeline and popularity) that approval would have produced. The machine-review consumer and human-review HTTP surface stay unregistered.

When review is enabled, publishing does not make content public. A newly published video is awaiting review and is visible only to its author until a review decision approves it. Review is decided by machine first and escalates to a human whenever the machine cannot decide or the moderation path itself fails; a moderation failure never resolves to approval, because an outage in the review path must not become a publication channel. A review outcome and its audit trail are recorded together, so no state change exists without an explanation of who or what caused it, and the trail outlives the log retention window.

Approval — or publish itself when review is disabled — is the single point where content becomes public: the side effects that expose content — entering the global timeline, counting toward popularity, and scheduling a title-and-description embedding for recommendation — happen there and only there, for both machine and human decisions. A failed embedding must not block publication; the video remains public and can still appear in non-interest recommendation queues. Consequently no read path may treat presence in a derived index as evidence of approval; every public query filters on review state, including the one that resolves cached or index-supplied identifiers back into videos. A viewer who is not the author cannot distinguish an unapproved video from one that does not exist. Content awaiting review remains reachable by a human reviewer even if its machine review never arrived, so nothing can be silently stranded outside both the public surface and the review queue.

Media validation happens on the write path only. The Worker checks transcoded output before it is stored, and publishing confirms that the referenced objects exist. Read paths validate URL shape alone and never call object storage, because playability filtering runs on every feed item and a per-item storage round trip would cost tens of calls per page. The consequence is deliberate: if a stored object is removed or corrupted outside the application, listings no longer hide that video automatically, and the gap has to be found by operations rather than by the read path.

Author intent is a separate field from review state. A video is a draft, published, or unpublished by its author; review still decides whether a published video may appear in public listings. Public surfaces require all three: the author has published it, review has approved it, and it has not been soft-deleted. Drafts never enter review, the global timeline, or popularity. An unfinished compose is persisted as a draft only after playable media exists, reused for the same author and playable URL, and capped per author so abandoned uploads cannot grow without bound. Leaving the composer or explicitly saving writes that draft. Publishing a draft promotes the same row instead of creating a second video, and the publish time becomes the listing time.

The author may unpublish a published video without changing its review conclusion, and may relist it later. Relisting an already-approved video re-enters the public-release path; relisting a pending or rejected video only restores author intent. The author may soft-delete a draft, unpublished, or published video. Soft-deleted rows remain in the database for audit and recovery, disappear from every author and public list, and answer as missing on every read path, including share-code resolution. Non-authors who attempt these writes receive the same missing outcome as a request for an unknown id. A moderation takedown still writes rejected and is not used to express author unpublish.

A rejection tells the author only that the content did not pass. The matched rule, category, or any other decision detail stays internal, because disclosing it lets an author probe the boundary by resubmitting variations. A reviewer may also reject already-public content from the administration surface without a prior report; that path is owned by `admin/spec.md` and still writes the same trail. A reviewer may list videos in every review state through administration. That list must not use the public detail path and must not write an unapproved record into the public detail cache.

The review capability is an interface with one local implementation; substituting or chaining a different provider must not require changes to the state machine, the audit trail, or the read-path filtering.

A video can be addressed by a share code as well as by its identifier. The code is derived from the video itself and stores nothing, so a code never expires, never drifts from the content it names, and needs no cleanup. Codes are fixed width and carry a check character, so a code that was truncated or mistyped is reported as invalid rather than silently resolving to a different video — resolving to the wrong video is worse than refusing, because the viewer has no way to notice.

The code is an addressing scheme, not a credential. It grants nothing: resolving a code applies exactly the same visibility rules as requesting the video directly, so a code for content that is unapproved or removed resolves as though the content does not exist, and issuing a code for a video the requester cannot see is likewise impossible. Consequently the encoding may obscure the identifier's sequence but must never be relied on to conceal it.

Both directions of the mapping live on the server, and resolution accepts the surrounding text a user pasted rather than requiring a pre-extracted code. Recognition is deliberately conservative: text is only treated as carrying a code when it is unambiguously one, because a scheme that guesses will occasionally send a viewer to an unrelated video. Text submitted for resolution is bounded, and every failure to recognize, validate, or resolve a code is reported to the caller as the same outcome, so a caller holding arbitrary codes learns nothing about which ones name real content.
