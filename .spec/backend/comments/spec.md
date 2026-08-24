---
title: comments
status: active
code:
  - backend/internal/comment/service.go
related:
  - backend/internal/comment/handler.go
  - backend/internal/comment/repo.go
  - backend/internal/comment/entity.go
  - backend/internal/comment/id.go
---
# comments

## raw source
Users can publish, list, reply to, and delete video comments while preserving the comment tree and ownership rules.

## expanded spec
Root comments and replies retain stable identifiers and parent relationships. Deletion follows the defined ownership and subtree rules. Asynchronous writes must not make an accepted comment impossible to retrieve after the worker settles. Once the comment is durable, the commenter's stored interest tags are updated from that video's stored tags, and the same write produces comment, reply, and `@username` notices as defined by `notification/spec.md`; deleting the comment hides those notices.
