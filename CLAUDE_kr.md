# CLAUDE_kr.md

이 파일은 이 저장소에서 작업하는 Claude Code (claude.ai/code)를 위한 가이드를 제공합니다.

## 개발 명령어

- `go run .` — 저장소 루트의 `config.yaml`을 사용해 서비스를 실행합니다.
- `go build -o bin/go2rtc-manager .` — 로컬 실행 파일을 빌드합니다.
- `go test ./...` — 전체 테스트 스위트를 실행합니다.
- `go test ./actor -run TestGo2RTCActorHasProducer` — 단일 테스트를 실행합니다.
- `gofmt -w .` — 모든 Go 파일을 포맷합니다.
- `docker build -f Docker/Dockerfile -t go2rtc-manager .` — 컨테이너 이미지를 빌드합니다.

이 저장소는 Go 1.24.x를 사용합니다. 기존 저장소 가이드에서는 로컬 개발 버전으로 Go 1.24.12를 기준으로 두고 있습니다.

체크인된 `Docker/Dockerfile`은 컨테이너 실행 시 `GO2RTC_MANAGER_HTTP_ADDR=:7181`를 설정하고 `7181` 포트를 expose합니다. 저장소에는 `docker-compose.yml`도 포함되어 있으며, 로컬 컨테이너 실행 시 저장소 루트의 `config.yaml`, `storage`, `go2rtc.yaml`를 마운트하면서 `7181` 포트를 publish합니다.

## 런타임 및 설정 모델

- `main.go`는 `config.yaml`을 로드하고, 공용 `slog` 로거를 생성한 뒤 Proto.Actor 시스템을 시작하고 `MasterActor`를 생성한 다음 HTTP 서버도 함께 시작합니다.
- 설정은 `config.Load`에서 Viper로 읽습니다. 파일 설정값은 `GO2RTC_MANAGER_` 접두사를 가진 환경 변수로 덮어쓸 수 있으며, 중첩 키는 언더스코어로 매핑됩니다. 예: `GO2RTC_MANAGER_GO2RTC_BASE_URL`, `GO2RTC_MANAGER_APP_BOX_IP`
- `config/config.go`의 검증 로직은 실제 동작에 중요합니다. `app.box_ip`, `http.addr`, 0보다 큰 `http.read_timeout`, 0보다 큰 `http.write_timeout`, 0보다 큰 `http.idle_timeout`, `go2rtc.base_url`, `go2rtc.config_path`, 하나 이상의 유효한 `schedule.crons`, 0보다 큰 `schedule.confirmation_delay`, `snapshot.storage_dir`, 0보다 큰 `record.max_duration`, 0보다 큰 `record.job_retention`, 0보다 큰 `record.max_concurrent_jobs`, 유효한 IP/CIDR 형식의 `record.allowed_ips`, 필수 MinIO 설정, 필수 MongoDB 설정, 그리고 `redis.addr`가 설정된 경우 0보다 큰 `redis.publish_interval`이 필요합니다.
- cleanup 스케줄은 `schedule.crons`의 표준 5-field cron 문자열 목록으로 구성됩니다. 각 cron은 서버 local 시간대를 기준으로 해석되며 하루 여러 시각 실행도 가능합니다.
- `config.yaml`은 대표적인 런타임 설정 예시이며, cleanup 스케줄러, 주기적 Redis 기록, snapshot HTTP API, MinIO MP4 녹화를 함께 표현할 수 있는 기준 설정입니다.

## 액터 아키텍처

서비스는 Proto.Actor 기반으로 구성되어 있으며, 액터 간 메시지 계약은 `common/message.go`에 모여 있습니다.

- `MasterActor`는 전체 오케스트레이션 허브입니다. 시작 시 하위 액터를 생성하고, cron 기반 cleanup 스케줄러를 시작하며, 필요하면 Redis publish ticker도 시작하고, cleanup 관련 라이프사이클 메시지를 라우팅하고, snapshot 요청을 `SnapshotActor`로 전달합니다.
- `StreamCleanerActor`는 한 번에 하나의 cleanup 사이클을 담당합니다. 스트림 목록을 요청하고, 진행 중인 검사 수를 추적하며, producer가 없는 스트림에 대해 2단계 확인 흐름을 수행하고, 두 번째 검사에서도 producer가 없을 때만 삭제하며, 사이클 종료 시 최종 alive/removed 개수를 전달합니다.
- `StreamCountActor`는 주기적 Redis 기록용 count-only alive stream 계산을 담당합니다. go2rtc 상태 확인 흐름과 동일한 2단계 producer 확인을 재사용하지만, 스트림 삭제는 수행하지 않습니다.
- `Go2RTCActor`는 외부 go2rtc 접근을 캡슐화합니다. 설정된 go2rtc YAML 파일에서 스트림 이름을 읽고, `GET /api/streams?src=<name>` 호출로 producer 존재 여부를 확인하며, 변경 전 go2rtc 설정 백업을 선택적으로 만들고, `DELETE /api/streams?src=<name>`로 죽은 스트림을 삭제합니다.
- `SnapshotActor`는 온디맨드 프레임 캡처를 담당합니다. go2rtc의 `GET /api/frame.jpeg?src=<cam_id>`를 호출하고, 응답을 검증한 뒤 파일명을 안전하게 정리해서 `snapshot.storage_dir` 아래에 JPEG 파일로 저장합니다.
- `RecordActor`는 비동기 녹화 job을 담당합니다. MongoDB에서 `mac`으로 `process`, `site`, `group`을 조회하고, go2rtc의 `GET /api/stream.mp4?src=<cam_id>&duration=<seconds>&filename=<filename>`로 MP4를 녹화하며, 정규화된 `process` bucket이 MinIO에 없으면 생성하고 `<site>/<group>/<cam_id>_<TYPE>_<timestamp>.mp4`에 업로드합니다.
- `RedisActor`는 선택적 구성 요소이며 `redis.addr`가 설정된 경우에만 활성화됩니다. alive stream count를 `stream_count@<app.box_ip>` 키로 기록하며, Redis `SET`을 사용하고 TTL은 두지 않습니다. 삭제 직후 기록과 주기 기록을 모두 담당합니다.
- `ActionActor`는 삭제 이후 후속 처리 지점입니다. 현재는 후속 액션 요청을 로그로 남기고 `action.dry_run` 설정을 반영하지만, 추가 외부 부작용을 수행하지는 않습니다.

## Cleanup 흐름

1. `MasterActor`가 `run_on_start` 또는 설정된 `schedule.crons` 항목에 따라 cleanup을 트리거합니다.
2. `StreamCleanerActor`가 `Go2RTCActor`에 go2rtc YAML의 `streams` 섹션에서 스트림 이름 목록을 요청합니다.
3. 각 스트림은 go2rtc HTTP API를 통해 검사됩니다.
4. producer가 있는 스트림은 즉시 alive로 집계됩니다.
5. producer가 없는 스트림은 `schedule.confirmation_delay` 이후 한 번 더 검사합니다.
6. 두 번째 검사에서도 producer가 없을 때만 스트림을 삭제합니다.
7. 삭제가 실제로 발생하면 `MasterActor`가 `RedisActor`에 최종 alive 수를 즉시 기록하도록 요청합니다.
8. 스트림 삭제가 성공하면 `ActionActor`로 후속 액션 요청도 전달됩니다.

## 주기적 Redis 기록 흐름

1. Redis가 활성화되어 있으면 `MasterActor`는 서비스 시작 직후 1회, 이후 `redis.publish_interval`마다 count-only 실행을 트리거합니다.
2. `StreamCountActor`가 `Go2RTCActor`에 스트림 목록을 요청합니다.
3. 각 스트림은 cleanup과 동일한 go2rtc API 및 2단계 producer 확인 흐름으로 검사됩니다.
4. `StreamCountActor`는 삭제 없이 `AliveStreamCountCalculated` 결과를 반환합니다.
5. `MasterActor`는 이 결과를 `UpdateStreamCount`로 `RedisActor`에 전달합니다.
6. `RedisActor`는 `stream_count@<app.box_ip>` key에 alive count를 기록합니다.

## Snapshot HTTP 흐름

1. HTTP 서버는 JSON body에 `cam_id`를 포함한 `POST /snapshots` 요청을 받습니다.
2. `httpserver`는 설정된 HTTP write timeout을 사용해 `RequestFuture`로 `CaptureSnapshotRequest`를 `MasterActor`에 보냅니다.
3. `MasterActor`는 요청을 `SnapshotActor`로 전달합니다.
4. `SnapshotActor`는 go2rtc의 `GET /api/frame.jpeg?src=<cam_id>`를 호출하고, 이미지를 `<snapshot.storage_dir>/<정리된-cam-id>.jpg` 경로에 저장합니다.
5. 성공하면 HTTP 핸들러는 `cam_id`와 `saved_path`를 포함한 `201 Created`를 반환합니다.
6. 잘못된 JSON 또는 누락된 `cam_id`는 `400`, 지원하지 않는 메서드는 `405`, go2rtc의 `404`는 `404`, 액터 응답 timeout은 `504`로 매핑됩니다.

## Record HTTP 흐름

1. HTTP 서버는 `TYPE`, `mac`, `cam_id`, `duration`을 포함한 `POST /record` 요청을 받습니다.
2. `TYPE`은 `UI` 또는 `BODYCAM`이어야 하며, `duration`은 0보다 큰 Go duration 문자열이고 `record.max_duration`을 초과할 수 없습니다.
3. `httpserver`는 직접 연결한 클라이언트의 `RemoteAddr`가 `record.allowed_ips`에 매칭될 때만 `POST /record`와 `GET /record/{job_id}`를 허용합니다. 빈 `allowed_ips`는 record API 전체를 `403 Forbidden`으로 차단합니다.
4. `httpserver`는 `StartRecordRequest`를 `MasterActor`에 보내고, `MasterActor`는 `RecordActor`로 전달합니다.
5. `RecordActor`는 메모리 job을 만들고 `202 Accepted`와 `job_id`, `accepted` 상태를 반환합니다. `accepted`/`running` 상태의 active job 수가 `record.max_concurrent_jobs`에 도달한 경우 `429 Too Many Requests`를 반환합니다.
6. background job은 MongoDB `BODYCAM_INFO`를 `mac`으로 조회하고, `process`를 MinIO bucket 이름으로 정규화한 뒤, go2rtc에서 MP4를 녹화하고 bucket이 없으면 생성한 다음 object를 업로드합니다.
7. 호출자는 `GET /record/{job_id}`로 `accepted`, `running`, `completed`, `failed` 상태를 조회합니다. 완료 상태에는 `bucket`, `object_key`, `content_type`이 포함됩니다.

## 테스트 포인트

현재 테스트는 `actor/Go2RTCActor_test.go`, `actor/RedisActor_test.go`, `actor/RecordActor_test.go`, `actor/SnapshotActor_test.go`, `actor/StreamCountActor_test.go`, `config/config_test.go`, `httpserver/server_test.go`에 있습니다.

- `Go2RTCActor` 테스트는 YAML 기반 스트림 탐색, HTTP API 응답 기반 producer 판별, 삭제 시 백업 생성 동작을 검증합니다.
- `RedisActor` 테스트는 Redis 키/값 기록 동작을 검증합니다.
- `RecordActor` 테스트는 record job 성공/실패 상태 전이, retention 정리, active job 제한을 검증합니다.
- `SnapshotActor` 테스트는 정상적인 프레임 캡처 및 저장 동작과 go2rtc not-found 경로를 검증합니다.
- `StreamCountActor` 테스트는 count-only alive stream 계산을 검증합니다.
- `config` 테스트는 `app.box_ip`, cleanup cron 검증, record/MinIO/MongoDB 검증, Redis publish interval 검증을 다룹니다.
- `httpserver` 테스트는 snapshot 및 record HTTP 핸들러 응답 형태를 검증합니다.

cleanup 로직을 변경할 때는 `common/message.go`, `MasterActor`, `StreamCleanerActor`, `Go2RTCActor` 사이의 메시지 흐름과 2단계 확인 후 삭제 semantics가 일치하도록 유지해야 합니다.
주기적 Redis 기록을 변경할 때는 `common/message.go`, `MasterActor`, `StreamCountActor`, `Go2RTCActor`, `RedisActor`가 함께 맞물리도록 유지해야 합니다.
snapshot 동작을 변경할 때는 `common/message.go`, `MasterActor`, `SnapshotActor`, `httpserver/server.go`가 함께 맞물리도록 유지해야 합니다.
record 동작을 변경할 때는 `common/message.go`, `MasterActor`, `RecordActor`, `httpserver/server.go`가 함께 맞물리도록 유지해야 합니다.

## 저장소 특이사항

- 서비스는 `go2rtc.config_path`에 지정된 go2rtc 설정 파일 경로를 사용하며, 기본값은 `/config/go2rtc.yaml`입니다.
- snapshot 파일은 `snapshot.storage_dir` 아래에 저장되며, 기본값은 `storage`입니다.
- record job은 메모리에 저장되며 완료/실패 job은 `record.job_retention` 이후 정리됩니다. 프로세스가 재시작되면 job 상태는 유실됩니다. `record.max_concurrent_jobs`는 기본값 `3`이며 `accepted`/`running` active job만 제한합니다. record API 접근 제어는 직접 연결한 `RemoteAddr`와 `record.allowed_ips`를 비교하며 proxy header는 사용하지 않습니다.
- `app.box_ip`는 manager 박스를 식별하며 Redis key suffix로 사용됩니다.
- `schedule.crons`는 cleanup 실행 시각을 표준 5-field cron 문자열로 정의하며 서버 local 시간대 기준으로 해석됩니다.
- `redis.publish_interval`은 Redis가 활성화되었을 때 alive stream count를 다시 계산하고 기록하는 주기를 의미합니다.
- 동작, 설정, 기여 가이드가 바뀌면 `README.md`, `AGENTS.md`, `CLAUDE.md`도 함께 맞춰야 합니다.
