# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the entrypoint and always loads `config.yaml` from the repository root. Keep runtime wiring there only. Put actor implementations in `actor/` (`MasterActor`, `Go2RTCActor`, `StreamCleanerActor`, `ActionActor`), shared message types in `common/`, configuration loading and defaults in `config/`, and logging setup in `logging/`. Container build assets live in `Docker/`. Do not commit Windows `:Zone.Identifier` sidecar files.

Keep documentation in sync with the codebase. When files are added, removed, renamed, when responsibilities change, or when file contents change in a way that affects behavior, usage, configuration, or structure, update `README.md` in the same change. Update `AGENTS.md` as well when contributor guidance or repository conventions change.

## Build, Test, and Development Commands
Use the standard Go toolchain from the repo root:

- `go run .` runs the service with `config.yaml`.
- `go build -o bin/go2rtc-stream-cleaner .` builds a local binary.
- `go test ./...` runs all package tests.
- `docker build -f Docker/Dockerfile -t go2rtc-stream-cleaner .` builds the production image.

Use Go 1.24.x for all work in this repository. In this environment, the pinned install is `~/.local/go/1.24.12`; confirm `go version` reports `go1.24.12` before building, testing, or updating dependencies.

Configuration is Viper-based, so environment overrides use the `GO2RTC_CLEANER_` prefix. Example: `GO2RTC_CLEANER_GO2RTC_BASE_URL=http://127.0.0.1:1984 go run .`.

The cleanup flow is configuration-driven. By default it runs once on startup and then every `3h`, reads all stream names from the configured `go2rtc.yml` `streams` section, checks each stream via `GET /api/streams?src=<name>`, waits `3m` before rechecking streams that have no producer, and deletes only streams that still have no producer on the second check via `DELETE /api/streams?src=<name>`.

After a cleanup cycle actually deletes one or more streams, publish the final alive stream count through `RedisActor` when Redis is configured. The Redis key format is `steam@count@<router_ip>`, and `redis.router_ip` must be set whenever `redis.addr` is configured.

## Coding Style & Naming Conventions
Follow idiomatic Go: tabs for indentation, lowercase package names, exported identifiers only when cross-package access is required. Run `gofmt -w` on every changed `.go` file before submitting. Keep actor responsibilities narrow: orchestration in `MasterActor`, external I/O in dedicated actors, and shared payloads in `common/message.go`. Use descriptive log fields and preserve the existing `slog` style.

## Testing Guidelines
There are no committed tests yet, so new behavior should include package-level `_test.go` files next to the code under test. Prefer table-driven tests for config parsing, HTTP response handling, and cleanup decision logic. When a change affects actor coordination, cover the message flow at the package level and include manual verification steps in the PR.

## Commit & Pull Request Guidelines
This repository has no established Git history yet, so start with short imperative commit subjects, ideally Conventional Commit style such as `feat: add backup guard for stream removal` or `fix: handle empty stream list`. PRs should include a brief summary, linked issue if one exists, config or Docker impact, and the exact validation performed (`go test ./...`, `go run .`, or container build output).
