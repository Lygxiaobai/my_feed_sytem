---
title: backend
status: active
code:
  - backend/cmd/main.go
  - backend/cmd/worker/main.go
related:
  - backend/internal/http/router.go
  - backend/internal/config/loadconfig.go
  - backend/internal/mq/topology.go
  - backend/internal/observability/metrics.go
  - backend/internal/media/service.go
  - backend/internal/worker/media_worker.go
desc: Go API and asynchronous Worker architecture boundary.
---
# backend

## raw source
The backend has two runnable processes: an HTTP API and an asynchronous Worker. They share configuration and infrastructure packages, while their process responsibilities remain separate.

## expanded spec
`backend/cmd/main.go` owns API bootstrap and lifecycle. It loads configuration, initializes database and optional cache or broker dependencies, assembles the HTTP router, starts API-side outbox and cache-invalidation work, and serves the public API.

`backend/cmd/worker/main.go` owns asynchronous processing. It connects to the broker, starts the like, comment, social, popularity, timeline, embedding, and dead-letter consumers, and shuts them down with the process context. It does not replace the HTTP API process.

`backend/internal/http/router.go` is the API composition boundary. Routes, authentication middleware, rate limits, the traffic guard, and handler wiring belong there; capability behavior belongs to the child backend specs. Configuration loading and shared database, broker, cache, and observability conventions must remain usable by both entrypoints.

Every HTTP endpoint answers with one response envelope: a machine-readable business code, a message written for the end user, the payload, and the identifier of the request occurrence. The success code is distinct from every failure code, so a client decides success from the envelope rather than from payload shape. HTTP status codes keep their transport meaning and are not collapsed into a single value; the business code carries the finer distinction the status cannot express.

Failure codes identify the origin of the failure — caller, this system, or a downstream dependency — so logs and alerts can be routed by origin without parsing messages. Stack traces, internal error text, SQL, and dependency diagnostics never appear in a response; they belong to the log record for the same request occurrence. The message returned to the caller is a user-facing sentence, never an error code and never a raw error string.

Both entrypoints emit structured logs through one logger with a configurable level. Log severity is a property of the log record, not of the business code: caller mistakes are warnings, failures of this system are errors. Each request carries an identifier that appears in both its response and every log record it produces, including records emitted by asynchronous work it triggers, so a reported failure can be traced back to its logs. Credentials, tokens, and query parameter values are redacted before they reach a log record. Endpoints that exist only for liveness or metrics scraping do not produce access-log records. Product-event report requests are omitted from access logs because each accepted event is written as its own product_event record.

The child nodes own the detailed account, feed, video, interaction, social, wallet, invoice, notification, dm, analytics, ops, admin, report, danmaku, history, outbox, message-consumer, and runtime contracts. This parent spec owns only the process boundary and the rules that connect those nodes.

Capabilities that exist to receive user notices about content are not gated on optional moderation features. `report/spec.md` owns that channel and stays registered whether or not automated review is configured, because a switch that turns off automated review must not also remove the only way a viewer can raise a problem.

## change rules
Changing an API route, middleware, or API startup dependency requires checking `runtime/spec.md` and the affected capability spec. Changing a queue, event, consumer, or worker lifecycle requires checking `message-consumers/spec.md`, `outbox/spec.md`, and the affected capability spec. Changing shared configuration or an entrypoint requires checking both API and Worker startup paths. Adding a failure code, changing the response envelope, or changing how severity maps to outcomes requires checking this parent spec and every affected capability spec, because clients and alerting both depend on those contracts.
