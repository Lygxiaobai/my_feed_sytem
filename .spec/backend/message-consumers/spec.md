---
title: message-consumers
status: active
code:
  - backend/internal/mq/consumer.go
related:
  - backend/internal/mq/connection.go
  - backend/internal/mq/topology.go
  - backend/internal/mq/processed_message.go
  - backend/internal/mq/dead_letter_consumer.go
  - backend/internal/mq/dead_letter_message.go
  - backend/internal/worker/common.go
  - backend/internal/worker/comment_worker.go
  - backend/internal/worker/like_worker.go
  - backend/internal/worker/popularity_worker.go
  - backend/internal/worker/social_worker.go
  - backend/internal/worker/timeline_worker.go
  - backend/internal/worker/fanout_worker.go
  - backend/internal/worker/embed_worker.go
  - backend/internal/feed/invalidation_consumer.go
  - backend/internal/video/invalidation_consumer.go
---
# message-consumers

## raw source
The Worker consumes RabbitMQ events and applies asynchronous media, comment, like, social, popularity, timeline, embedding, and cache-invalidation work safely.

## expanded spec
Consumers acknowledge messages only after the required effect is durable. The embedding consumer writes one title-and-description vector per approved video. If the embedding HTTP endpoint is not configured, the consumer acknowledges and skips rather than dead-lettering every publish. If the endpoint is configured but the call fails, the message follows the dead-letter policy so publication itself is unaffected. The comment and social consumers also persist the matching inbox notices in that same durable write, so a later inbox read cannot see a comment or follow that the worker has not yet applied. After a comment is durable the worker also writes the commenter's stored interest tags from that video's stored tags. Processed-message tracking makes redelivery safe, media tasks expose explicit processing/ready/failed states, and failed messages follow the dead-letter policy instead of disappearing. Worker behavior must remain independently runnable from the API process.

Following-feed fanout is a separate consumer from the global timeline consumer, so a slow or failing fanout never delays the latest feed. Because a single message has a bounded processing budget, fanout for one publish is split into follower batches that advance by cursor, and each batch schedules the next only after its own delivery succeeds. Fanout effects are naturally idempotent, so redelivery is safe without processed-message tracking. When the inbox and outbox storage is unavailable the fanout consumer does not run at all, rather than draining publishes into the dead-letter queue.

The short handle budget belongs to write-projection consumers. Media transcode is the exception: its handle budget is long enough to encode a file at the upload size limit. A short budget cancels ffmpeg before a valid video finishes and fails the task.
