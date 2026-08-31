---
title: feed
status: active
code:
  - backend/internal/feed/service.go
related:
  - backend/internal/feed/handler.go
  - backend/internal/feed/repo.go
  - backend/internal/feed/entity.go
  - backend/internal/feed/cache.go
  - backend/internal/feed/local_page_cache.go
  - backend/internal/feed/timeline_cache.go
  - backend/internal/feed/fanout_store.go
  - backend/internal/feed/invalidation_consumer.go
  - backend/internal/recommend/service.go
  - backend/internal/recommend/mixer.go
  - backend/internal/recommend/interest.go
  - backend/internal/recommend/embedder.go
  - backend/internal/recommend/repo.go
  - backend/internal/recommend/review.go
---
# feed

## raw source
The backend serves latest, following, likes-count, popularity, and personalized recommendation feeds with stable pagination and authenticated access where required. Each card includes the video's stored tags when present. Feed reads share an IP ceiling; recommendation is held to a tighter ceiling because mixing is more expensive than a cache-backed latest page.

## expanded spec
Feed reads prefer the appropriate Redis timeline, ranking, or page cache and can fall back to MySQL when a cache is unavailable or a ranking has no usable entries. A popularity ranking is usable only when the current Redis snapshot has at least 10 members. A first page (`as_of` and `offset` are both zero) that still cannot fill the requested limit after playable filtering is also treated as unusable. Playable filtering checks media URL shape only and does not consult object storage, so it never adds a storage round trip to a feed read. The feed does not merge window scores with persisted MySQL popularity on the same page. When the ranking is unusable, or the client continues a previous MySQL fallback page (`as_of` is zero and `offset` is greater than zero), the popularity feed returns persisted MySQL popularity and sets as_of to zero. Cursor or snapshot pagination must not duplicate or skip items across adjacent pages. Following feed results are scoped to the authenticated account. Cache invalidation and asynchronous timeline updates must not make newly published videos permanently invisible.

首页推荐不再复用热度窗。`listRecommend` 一页默认 10 条。已登录且账号 `interests` 有 tag 时，先取作品已存 tags 匹配这些 tag 的已过审作品；不够 10 条时再用默认混排补齐。没有 tag 或匿名时走默认推送：普通人作者与热度/最新的冷启动混排。候选始终从已过审的 MySQL 库存读取。一页默认 10 条时普通人槽位占 20%；该队列空时回填默认队列，不回填大 V 热度。热度只在热度队列内部排序；Redis 窗口快照不足 10 条时热度队列改用 `videos.popularity`。多样性（MMR 与作者窗口）作用于默认补齐。分页用已出视频 id 排除集。未过审内容不得出现。粉丝数阈值只用于推荐侧的普通人判定。账号兴趣 tag 由点赞、打赏、评论从作品已存 tags 写入，最多 7 个，多出的覆盖最前面的；作品没有 tag 就不改兴趣。用户兴趣向量仍可由点赞/打赏重算写入 `user_embeddings`，但本页推送读的是账号 tag 字段，不读向量。视频向量在过审后异步计算，主副本在 `video_embeddings`。管理面展示的是账号上已持久化的 tag 和作品上已存的 tags。

The following feed combines write fanout and read fanout. Videos from authors below the configured follower threshold are delivered into a per-user inbox when they are published, videos from authors at or above the pull threshold stay in a per-author outbox and are merged at read time, and an author between the two thresholds is delivered only to followers whose inbox is currently maintained. A read must produce the same items regardless of which side delivered them: results are restricted to the reader's current following set, so an unfollowed author's residue in an inbox is never returned, and a reader whose inbox is not currently maintained has it rebuilt from MySQL before the page is assembled. A page may only report that no further pages exist when the inbox and every merged outbox are known to be complete; otherwise the page is produced from MySQL. When Redis, the inbox, or the outbox is unavailable, the following feed degrades to reading MySQL directly and remains correct.
