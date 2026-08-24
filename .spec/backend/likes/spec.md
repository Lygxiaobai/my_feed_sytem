---
title: likes
status: active
code:
  - backend/internal/like/service.go
related:
  - backend/internal/like/handler.go
  - backend/internal/like/repo.go
  - backend/internal/like/entity.go
---
# likes

## raw source
A user can like, unlike, and query the like state of a video without creating duplicate relationships.

## expanded spec
Like and unlike write the relationship and `videos.likes_count` in the same MySQL transaction. The unique relationship key makes concurrent duplicate requests idempotent, so a successful response means the relationship query is already consistent.

After the transaction commits, the liker's stored interest tags are updated from that video's stored tags, an outbox event asynchronously updates popularity projections and cache invalidation. A broker or Redis failure must not make the committed like relationship disappear. The same transaction writes the author's like notice unless the liker is the author; aggregation and retraction of that notice are owned by `notification/spec.md`.
