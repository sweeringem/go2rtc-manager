basic rule for project
1. Think Before Coding
Don't assume. Don't hide confusion. Surface tradeoffs.

Before implementing:

State your assumptions explicitly. If uncertain, ask.
If multiple interpretations exist, present them - don't pick silently.
If a simpler approach exists, say so. Push back when warranted.
If something is unclear, stop. Name what's confusing. Ask.
2. Simplicity First
Minimum code that solves the problem. Nothing speculative.

No features beyond what was asked.
No abstractions for single-use code.
No "flexibility" or "configurability" that wasn't requested.
No error handling for impossible scenarios.
If you write 200 lines and it could be 50, rewrite it.
Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

3. Surgical Changes
Touch only what you must. Clean up only your own mess.

When editing existing code:

Don't "improve" adjacent code, comments, or formatting.
Don't refactor things that aren't broken.
Match existing style, even if you'd do it differently.
If you notice unrelated dead code, mention it - don't delete it.
When your changes create orphans:

Remove imports/variables/functions that YOUR changes made unused.
Don't remove pre-existing dead code unless asked.
The test: Every changed line should trace directly to the user's request.

4. Goal-Driven Execution
Define success criteria. Loop until verified.

Transform tasks into verifiable goals:

"Add validation" → "Write tests for invalid inputs, then make them pass"
"Fix the bug" → "Write a test that reproduces it, then make it pass"
"Refactor X" → "Ensure tests pass before and after"
For multi-step tasks, state a brief plan:

1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the entrypoint. It loads `config.yaml`, initializes the shared `slog` logger, starts the Proto.Actor system, spawns `MasterActor`, and launches the HTTP server. Keep runtime wiring there only.

Project layout:

- `actor/` — actor implementations for orchestration and I/O boundaries (`MasterActor`, `StreamCleanerActor`, `StreamCountActor`, `Go2RTCActor`, `SnapshotActor`, `RecordActor`, `RedisActor`, `ActionActor`)
- `common/` — actor message contracts in `common/message.go`
- `config/` — Viper-based config loading, defaults, validation, and config tests
- `httpserver/` — HTTP handlers and server wiring for `POST /snapshots`, `POST /record`, and `GET /record/{job_id}`
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

The checked-in `Docker/Dockerfile` sets `GO2RTC_MANAGER_HTTP_ADDR=:7181` and exposes port `7181` for container runs. The repo also includes `docker-compose.yml` for a local container run that starts `go2rtc-manager` plus MinIO, publishes `7181`, `9000`, and `9001`, and mounts `config.yaml`, `storage`, and `go2rtc.yaml` from the repository root. MongoDB remains external and must be configured through `config.yaml`.

## Runtime & Configuration Notes
Configuration is loaded through Viper. Environment overrides use the `GO2RTC_MANAGER_` prefix, with nested keys mapped using underscores. Example:

- `GO2RTC_MANAGER_APP_BOX_IP=192.168.0.10`
- `GO2RTC_MANAGER_GO2RTC_BASE_URL=http://127.0.0.1:1984`
- `GO2RTC_MANAGER_MINIO_ENDPOINT=localhost:9000`
- `GO2RTC_MANAGER_MONGODB_URI=mongodb://localhost:27017`
- `GO2RTC_MANAGER_REDIS_PUBLISH_INTERVAL=5m`

Validation in `config/config.go` is load-bearing. Keep these constraints aligned with the implementation:

- `app.box_ip` is required
- `http.addr` is required
- `http.read_timeout`, `http.write_timeout`, and `http.idle_timeout` must be positive
- `go2rtc.base_url` and `go2rtc.config_path` are required
- `schedule.crons` must contain one or more valid 5-field cron expressions
- `schedule.confirmation_delay` must be positive
- `snapshot.storage_dir` is required
- `record.max_duration`, `record.job_retention`, and `record.max_concurrent_jobs` must be positive
- `record.allowed_ips` entries must be valid IPs or CIDRs
- `minio.endpoint`, `minio.access_key`, and `minio.secret_key` are required
- `mongodb.uri`, `mongodb.database`, and `mongodb.collection` are required
- `redis.publish_interval` must be positive whenever `redis.addr` is set

Cleanup scheduling is cron-based, not interval-based. `schedule.crons` can contain multiple daily run times, and each cron is interpreted in the server local timezone.
Recording uses go2rtc `GET /api/stream.mp4?src=<cam_id>&duration=<seconds>&filename=<filename>` and stores MP4 objects in MinIO. `record.max_duration` defaults to `1h`, `record.max_concurrent_jobs` defaults to `3`, and completed/failed in-memory jobs are pruned after `record.job_retention`. `POST /record` and `GET /record/{job_id}` are allowed only when the direct client `RemoteAddr` matches `record.allowed_ips`; empty `allowed_ips` blocks all record API access.

## Actor Responsibilities
Keep actor responsibilities narrow and consistent with the current message flow:

- `MasterActor` orchestrates cleanup cycles, registers cron-based cleanup schedules, runs periodic stream counting for Redis publishing, routes cleanup lifecycle messages, and forwards snapshot requests.
- `StreamCleanerActor` owns a single cleanup cycle at a time and implements the double-check deletion semantics.
- `StreamCountActor` owns count-only alive stream calculations for periodic Redis publishing and must never delete streams.
- `Go2RTCActor` reads stream names from the go2rtc YAML config, checks producer state through `GET /api/streams?src=<name>`, optionally creates backups, and removes streams through `DELETE /api/streams?src=<name>`.
- `SnapshotActor` captures images through `GET /api/frame.jpeg?src=<cam_id>` and writes JPEGs under `snapshot.storage_dir`.
- `RecordActor` owns asynchronous record jobs, looks up `process`, `site`, and `group` from MongoDB by `mac`, records MP4 through go2rtc, creates a MinIO bucket from normalized `process` when absent, and uploads to `<site>/<group>/<cam_id>_<TYPE>_<timestamp>.mp4`.
- `RedisActor` publishes alive-stream counts to `stream_count@<app.box_ip>` using Redis `SET`, and supports both delete-triggered and periodic writes.
- `ActionActor` is the post-removal hook point and currently logs follow-up actions only.

When changing cleanup behavior, keep `common/message.go`, `MasterActor`, `StreamCleanerActor`, and `Go2RTCActor` aligned.
When changing periodic Redis counting, keep `common/message.go`, `MasterActor`, `StreamCountActor`, `Go2RTCActor`, and `RedisActor` aligned.
When changing snapshot behavior, keep `common/message.go`, `MasterActor`, `SnapshotActor`, and `httpserver/server.go` aligned.
When changing record behavior, keep `common/message.go`, `MasterActor`, `RecordActor`, and `httpserver/server.go` aligned.

## HTTP API Expectations
The service exposes `POST /snapshots`, `POST /record`, and `GET /record/{job_id}`.

- Request body: JSON with `cam_id`
- Success response: `201 Created` with `cam_id` and `saved_path`
- Invalid JSON or missing `cam_id`: `400 Bad Request`
- Wrong method: `405 Method Not Allowed`
- go2rtc not found path: `404 Not Found`
- Future timeout or actor response failure: `504 Gateway Timeout`

Preserve the current request/response shape unless the user explicitly asks for an API change.

Record request body: JSON with `TYPE`, `mac`, `cam_id`, and `duration`. `TYPE` must be `UI` or `BODYCAM`. `POST /record` returns `202 Accepted` with `job_id` and `accepted` status; callers poll `GET /record/{job_id}` for `running`, `completed`, or `failed`. Completed record jobs return `bucket`, `object_key`, and `content_type`. If `RemoteAddr` is not allowed, record APIs return `403 Forbidden`. If active `accepted`/`running` jobs reach `record.max_concurrent_jobs`, `POST /record` returns `429 Too Many Requests`.

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
- `actor/RecordActor_test.go`
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
