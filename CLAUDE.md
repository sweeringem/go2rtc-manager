# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development commands

- `go run .` — run the service with the repository-root `config.yaml`
- `go build -o bin/go2rtc-manager .` — build the local binary
- `go test ./...` — run the full test suite
- `go test ./actor -run TestGo2RTCActorHasProducer` — run a single test
- `gofmt -w .` — format all Go files
- `docker build -f Docker/Dockerfile -t go2rtc-manager .` — build the container image

Use Go 1.24.x in this repository. The existing repo guidance pins local development to Go 1.24.12.

## Runtime and configuration model

- `main.go` loads `config.yaml`, creates the shared `slog` logger, starts a Proto.Actor system, spawns `MasterActor`, and starts the HTTP server.
- Configuration is loaded by `config.Load` with Viper. File values can be overridden with environment variables prefixed by `GO2RTC_MANAGER_`, with nested keys mapped by underscores, e.g. `GO2RTC_MANAGER_GO2RTC_BASE_URL` and `GO2RTC_MANAGER_APP_BOX_IP`.
- Validation in `config/config.go` is load-bearing: `app.box_ip`, `http.addr`, positive `http.read_timeout`, positive `http.write_timeout`, positive `http.idle_timeout`, `go2rtc.base_url`, `go2rtc.config_path`, one or more valid `schedule.crons` entries, positive `schedule.confirmation_delay`, `snapshot.storage_dir`, and positive `redis.publish_interval` whenever `redis.addr` is set.
- Cleanup scheduling is driven by `schedule.crons` using standard 5-field cron strings. Cron expressions are interpreted in the server local timezone and can represent multiple runs per day.
- `config.yaml` is the canonical example runtime config and can enable the cleanup scheduler, periodic Redis publishing, and the snapshot HTTP API.

## Actor architecture

The service is organized around Proto.Actor actors with message contracts centralized in `common/message.go`.

- `MasterActor` is the orchestration hub. On startup it spawns the child actors, starts the cron-based cleanup scheduler, optionally starts the Redis publish ticker, routes cleanup lifecycle messages, and forwards snapshot requests to `SnapshotActor`.
- `StreamCleanerActor` owns one cleanup cycle at a time. It requests the stream list, tracks outstanding checks, performs the two-pass confirmation flow for streams without producers, deletes only after the second failed check, and emits the final alive/removed counts when the cycle completes.
- `StreamCountActor` handles count-only alive stream calculations for periodic Redis publishing. It reuses the go2rtc health-check flow, performs the same two-pass producer confirmation, and never deletes streams.
- `Go2RTCActor` encapsulates external go2rtc access. It reads the configured go2rtc YAML file to discover stream names, calls `GET /api/streams?src=<name>` to detect whether a producer exists, optionally creates a backup of the go2rtc config before changes, and removes dead streams through `DELETE /api/streams?src=<name>`.
- `SnapshotActor` handles on-demand frame capture. It calls `GET /api/frame.jpeg?src=<cam_id>` on go2rtc, validates the response, sanitizes the filename, and stores the JPEG under `snapshot.storage_dir`.
- `RedisActor` is optional and activates only when `redis.addr` is configured. It writes alive-stream counts to `stream_count@<app.box_ip>` using Redis `SET`, and supports both delete-triggered and periodic writes.
- `ActionActor` is the post-removal hook point. Right now it logs follow-up actions and reflects the `action.dry_run` setting, but it does not perform additional external side effects.

## Cleanup flow

1. `MasterActor` triggers a cleanup because of `run_on_start` or one of the configured `schedule.crons` entries.
2. `StreamCleanerActor` asks `Go2RTCActor` for the stream names from the go2rtc YAML `streams` section.
3. Each stream is checked through the go2rtc HTTP API.
4. Streams with producers are counted as alive immediately.
5. Streams without producers are checked again after `schedule.confirmation_delay`.
6. A stream is deleted only if it is still missing a producer on the second check.
7. When deletions happen, `MasterActor` asks `RedisActor` to publish the final alive count immediately.
8. Each successful removal also emits a follow-up action request to `ActionActor`.

## Periodic Redis publish flow

1. If Redis is enabled, `MasterActor` triggers a count-only run immediately on startup and then every `redis.publish_interval`.
2. `StreamCountActor` requests the stream list from `Go2RTCActor`.
3. Each stream is checked for a producer using the same go2rtc API and two-pass confirmation timing as cleanup.
4. `StreamCountActor` returns `AliveStreamCountCalculated` without deleting any streams.
5. `MasterActor` forwards that result to `RedisActor` as `UpdateStreamCount`.
6. `RedisActor` writes the count to `stream_count@<app.box_ip>` with Redis `SET` and no TTL.

## Snapshot HTTP flow

1. The HTTP server accepts `POST /snapshots` requests with a JSON body containing `cam_id`.
2. `httpserver` sends `CaptureSnapshotRequest` to `MasterActor` with `RequestFuture` using the configured HTTP write timeout.
3. `MasterActor` forwards the request to `SnapshotActor`.
4. `SnapshotActor` calls `GET /api/frame.jpeg?src=<cam_id>` on go2rtc and writes the image to `<snapshot.storage_dir>/<sanitized-cam-id>.jpg`.
5. The HTTP handler returns `201 Created` with `cam_id` and `saved_path` on success.
6. Invalid JSON or a missing `cam_id` returns `400`, unsupported methods return `405`, go2rtc `404` maps to `404`, and actor timeout failures map to `504`.

## Testing focus

Current tests live in `actor/Go2RTCActor_test.go`, `actor/RedisActor_test.go`, `actor/SnapshotActor_test.go`, `actor/StreamCountActor_test.go`, `config/config_test.go`, and `httpserver/server_test.go`.

- `Go2RTCActor` tests cover stream discovery from YAML, producer detection from the HTTP API, and backup creation during delete.
- `RedisActor` tests verify the Redis key/value write behavior.
- `SnapshotActor` tests cover successful frame capture and save behavior plus the not-found path from go2rtc.
- `StreamCountActor` tests cover count-only alive stream calculation.
- `config` tests cover `app.box_ip`, cleanup cron validation, and Redis publish interval validation.
- `httpserver` tests cover the snapshot handler response shape.

When changing cleanup logic, keep the actor message flow and the double-check deletion semantics aligned with `common/message.go`, `MasterActor`, `StreamCleanerActor`, and `Go2RTCActor`.
When changing periodic Redis publishing, keep `common/message.go`, `MasterActor`, `StreamCountActor`, `Go2RTCActor`, and `RedisActor` aligned.
When changing snapshot behavior, keep `common/message.go`, `MasterActor`, `SnapshotActor`, and `httpserver/server.go` aligned.

## Repository-specific notes

- The service expects the go2rtc config file path from `go2rtc.config_path`; by default it points to `/config/go2rtc.yaml`.
- Snapshot files are stored under `snapshot.storage_dir`; the default is `storage`.
- `app.box_ip` identifies the manager box and is used as the Redis key suffix.
- `schedule.crons` configures cleanup run times with standard 5-field cron strings interpreted in the server local timezone.
- `redis.publish_interval` controls how often the service recalculates and publishes the alive stream count when Redis is enabled.
- Keep `README.md`, `AGENTS.md`, and `CLAUDE_kr.md` in sync when behavior, configuration, or contributor guidance changes.
