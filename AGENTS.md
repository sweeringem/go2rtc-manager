# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the entrypoint. It loads `config.yaml`, initializes the shared `slog` logger, starts the Proto.Actor system, spawns `MasterActor`, and launches the HTTP server. Keep runtime wiring there only.

Project layout:

- `actor/` — actor implementations for orchestration and I/O boundaries (`MasterActor`, `StreamCleanerActor`, `StreamCountActor`, `Go2RTCActor`, `SnapshotActor`, `RedisActor`, `ActionActor`)
- `common/` — actor message contracts in `common/message.go`
- `config/` — Viper-based config loading, defaults, validation, and config tests
- `httpserver/` — HTTP handlers and server wiring for `POST /snapshots`
- `logging/` — shared logger construction
- `Docker/` — container build assets

Keep documentation in sync with the codebase. When behavior, configuration, file layout, commands, or contributor guidance changes, update `README.md`, `AGENTS.md`, `CLAUDE.md`, and `CLAUDE_kr.md` in the same change.

## Build, Test, and Development Commands
Use the standard Go toolchain from the repo root:

- `go run .` — run the service with `config.yaml`
- `go build -o bin/go2rtc-manager .` — build a local binary
- `go test ./...` — run all tests
- `go test ./actor -run TestGo2RTCActorHasProducer` — run a targeted actor test
- `gofmt -w .` — format all Go files
- `docker build -f Docker/Dockerfile -t go2rtc-manager .` — build the production image

Use Go 1.24.x for this repository. The pinned local version is Go 1.24.12.

## Runtime & Configuration Notes
Configuration is loaded through Viper. Environment overrides use the `GO2RTC_MANAGER_` prefix, with nested keys mapped using underscores. Example:

- `GO2RTC_MANAGER_APP_BOX_IP=192.168.0.10`
- `GO2RTC_MANAGER_GO2RTC_BASE_URL=http://127.0.0.1:1984`
- `GO2RTC_MANAGER_REDIS_PUBLISH_INTERVAL=5m`

Validation in `config/config.go` is load-bearing. Keep these constraints aligned with the implementation:

- `app.box_ip` is required
- `http.addr` is required
- `http.read_timeout`, `http.write_timeout`, and `http.idle_timeout` must be positive
- `go2rtc.base_url` and `go2rtc.config_path` are required
- `schedule.crons` must contain one or more valid 5-field cron expressions
- `schedule.confirmation_delay` must be positive
- `snapshot.storage_dir` is required
- `redis.publish_interval` must be positive whenever `redis.addr` is set

Cleanup scheduling is cron-based, not interval-based. `schedule.crons` can contain multiple daily run times, and each cron is interpreted in the server local timezone.

## Actor Responsibilities
Keep actor responsibilities narrow and consistent with the current message flow:

- `MasterActor` orchestrates cleanup cycles, registers cron-based cleanup schedules, runs periodic stream counting for Redis publishing, routes cleanup lifecycle messages, and forwards snapshot requests.
- `StreamCleanerActor` owns a single cleanup cycle at a time and implements the double-check deletion semantics.
- `StreamCountActor` owns count-only alive stream calculations for periodic Redis publishing and must never delete streams.
- `Go2RTCActor` reads stream names from the go2rtc YAML config, checks producer state through `GET /api/streams?src=<name>`, optionally creates backups, and removes streams through `DELETE /api/streams?src=<name>`.
- `SnapshotActor` captures images through `GET /api/frame.jpeg?src=<cam_id>` and writes JPEGs under `snapshot.storage_dir`.
- `RedisActor` publishes alive-stream counts to `stream_count@<app.box_ip>` using Redis `SET`, and supports both delete-triggered and periodic writes.
- `ActionActor` is the post-removal hook point and currently logs follow-up actions only.

When changing cleanup behavior, keep `common/message.go`, `MasterActor`, `StreamCleanerActor`, and `Go2RTCActor` aligned.
When changing periodic Redis counting, keep `common/message.go`, `MasterActor`, `StreamCountActor`, `Go2RTCActor`, and `RedisActor` aligned.
When changing snapshot behavior, keep `common/message.go`, `MasterActor`, `SnapshotActor`, and `httpserver/server.go` aligned.

## HTTP API Expectations
The service exposes `POST /snapshots`.

- Request body: JSON with `cam_id`
- Success response: `201 Created` with `cam_id` and `saved_path`
- Invalid JSON or missing `cam_id`: `400 Bad Request`
- Wrong method: `405 Method Not Allowed`
- go2rtc not found path: `404 Not Found`
- Future timeout or actor response failure: `504 Gateway Timeout`

Preserve the current request/response shape unless the user explicitly asks for an API change.

## Coding Style & Naming Conventions
Follow idiomatic Go:

- tabs for indentation
- lowercase package names
- exported identifiers only when cross-package access is required
- `gofmt -w` on every changed Go file

Prefer direct, small implementations over speculative abstractions. Keep orchestration in `MasterActor`, external I/O in dedicated actors, and shared payloads in `common/message.go`. Preserve the existing `slog` logging style with descriptive structured fields.

## Testing Guidelines
Tests already exist and should be extended when behavior changes:

- `actor/Go2RTCActor_test.go`
- `actor/RedisActor_test.go`
- `actor/SnapshotActor_test.go`
- `actor/StreamCountActor_test.go`
- `config/config_test.go`
- `httpserver/server_test.go`

Prefer table-driven tests for HTTP response handling, config validation, and cleanup/count decision logic. When changing actor coordination, verify the message flow at the package level rather than only testing helper methods.

## Commit & Pull Request Guidelines
Use concise imperative commit subjects, preferably Conventional Commit style, for example:

- `feat: add app box ip for redis key generation`
- `fix: publish redis counts with configured box ip`
- `docs: sync box ip redis guidance`

PRs should summarize behavior changes, config impact, API impact, and exact validation performed, such as `go test ./...` or `docker build -f Docker/Dockerfile -t go2rtc-manager .`.
