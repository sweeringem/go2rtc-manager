# go2rtc-manager

`go2rtc-manager`는 Proto.Actor 기반으로 동작하는 `go2rtc` 보조 관리 서비스입니다.
`go2rtc` 설정 파일의 `streams` 목록을 읽어 producer가 없는 스트림을 2차 확인 후 정리하고, 원격 Redis에 alive stream count를 기록할 수 있습니다. 또한 HTTP API로 특정 카메라의 스냅샷 이미지를 저장할 수 있습니다.

## 주요 기능

- **cron 기반 cleanup**
  - 시작 시 1회 실행하거나(`schedule.run_on_start`)
  - `schedule.crons`에 지정한 여러 cron 시각에 cleanup 수행
  - cron 해석은 서버 local 시간대 기준
- **2단계 확인 후 삭제**
  - `GET /api/streams?src=<name>`로 producer 존재 여부 확인
  - producer가 없으면 `schedule.confirmation_delay` 후 한 번 더 확인
  - 두 번째 확인에서도 producer가 없을 때만 `DELETE /api/streams?src=<name>` 실행
- **Redis alive count 기록**
  - cleanup에서 실제 삭제가 발생하면 즉시 alive stream 수 기록
  - 별도 주기(`redis.publish_interval`)로 현재 alive stream 수를 다시 계산해 기록
  - 서비스 시작 직후에도 1회 바로 기록 시도
  - key 형식은 `stream_count@<app.box_ip>`
- **Snapshot HTTP API**
  - `POST /snapshots` 요청으로 go2rtc의 `GET /api/frame.jpeg?src=<cam_id>`를 호출해 JPEG 파일 저장

## 런타임 구성

서비스 시작 시 `main.go`가 다음을 수행합니다.

1. `config.yaml` 로드
2. `slog` 기반 로거 생성
3. Proto.Actor 시스템 시작
4. `MasterActor` 생성
5. HTTP 서버 시작

핵심 액터 구성은 다음과 같습니다.

- `MasterActor` — cleanup/snapshot/count 요청을 오케스트레이션하고 하위 액터를 라우팅
- `StreamCleanerActor` — cleanup 사이클과 2차 확인 후 삭제 흐름 담당
- `StreamCountActor` — 주기적 Redis 기록용 alive stream count 계산 담당
- `Go2RTCActor` — go2rtc 설정 파일 읽기, stream 상태 확인, 삭제 수행
- `SnapshotActor` — go2rtc snapshot API 호출 후 JPEG 저장
- `RedisActor` — 원격 Redis에 alive stream 수를 `app.box_ip` 기반 key로 기록
- `ActionActor` — 삭제 후 후속 액션 요청을 로그로 처리

액터 간 메시지 계약은 `common/message.go`에 모여 있습니다.

## 폴더 구조

```text
.
├─ main.go
├─ config.yaml
├─ go.mod
├─ go.sum
├─ Docker/
│  └─ Dockerfile
├─ actor/
│  ├─ ActionActor.go
│  ├─ Go2RTCActor.go
│  ├─ Go2RTCActor_test.go
│  ├─ MasterActor.go
│  ├─ RedisActor.go
│  ├─ RedisActor_test.go
│  ├─ SnapshotActor.go
│  ├─ SnapshotActor_test.go
│  ├─ StreamCleanerActor.go
│  ├─ StreamCountActor.go
│  └─ StreamCountActor_test.go
├─ common/
│  └─ message.go
├─ config/
│  ├─ config.go
│  └─ config_test.go
├─ httpserver/
│  ├─ server.go
│  └─ server_test.go
└─ logging/
   └─ logger.go
```

## 요구 사항

- Go 1.24.x
- 이 저장소의 로컬 기준 버전: **Go 1.24.12**
- 접근 가능한 `go2rtc` 서버
- `go2rtc.config_path`가 가리키는 go2rtc YAML 파일
- 선택 사항: 원격 Redis 서버

## 실행 방법

기본 실행:

```bash
go run .
```

바이너리 빌드:

```bash
go build -o bin/go2rtc-manager .
```

도커 이미지 빌드:

```bash
docker build -f Docker/Dockerfile -t go2rtc-manager .
```

## 설정

설정은 `config.yaml`에서 읽고, 환경 변수로 override할 수 있습니다.
환경 변수 prefix는 `GO2RTC_MANAGER_`이며 중첩 키는 `_`로 매핑됩니다.

예:

```bash
GO2RTC_MANAGER_APP_BOX_IP=192.168.0.10 GO2RTC_MANAGER_GO2RTC_BASE_URL=http://127.0.0.1:1984 go run .
```

대표 설정 항목:

```yaml
app:
  name: go2rtc-manager
  env: production
  box_ip: 192.168.0.10

http:
  addr: ":8080"
  read_timeout: 5s
  write_timeout: 15s
  idle_timeout: 60s

go2rtc:
  base_url: http://127.0.0.1:1984
  config_path: /config/go2rtc.yaml
  request_timeout: 10s
  backup_before_change: true

schedule:
  run_on_start: true
  crons:
    - "0 2 * * *"
    - "0 14 * * *"
    - "0 23 * * *"
  confirmation_delay: 3m

snapshot:
  storage_dir: storage

redis:
  addr: 10.0.0.20:6379
  password: ""
  db: 0
  publish_interval: 5m
```

중요 검증 규칙:

- `app.box_ip` 필수
- `http.addr` 필수
- `http.read_timeout`, `http.write_timeout`, `http.idle_timeout`은 0보다 커야 함
- `go2rtc.base_url`, `go2rtc.config_path` 필수
- `schedule.crons`는 하나 이상의 유효한 5-field cron 문자열을 포함해야 함
- `schedule.confirmation_delay`는 0보다 커야 함
- `snapshot.storage_dir` 필수
- `redis.addr`를 설정하면 `redis.publish_interval`도 0보다 커야 함

## Cleanup 동작 방식

1. `MasterActor`가 `schedule.run_on_start` 또는 `schedule.crons`에 따라 cleanup을 트리거합니다.
2. `StreamCleanerActor`가 go2rtc YAML의 `streams` 목록을 요청합니다.
3. 각 stream에 대해 producer 존재 여부를 확인합니다.
4. producer가 있으면 alive count에 즉시 반영합니다.
5. producer가 없으면 `schedule.confirmation_delay` 후 다시 확인합니다.
6. 두 번째 확인에서도 producer가 없을 때만 삭제합니다.
7. 삭제가 발생하면 `ActionActor`에 후속 액션 요청을 보내고 Redis에 alive count를 즉시 기록합니다.

## Cleanup cron 스케줄

cleanup 실행 시각은 `schedule.crons`에 5-field cron 문자열 목록으로 넣습니다.

예:

```yaml
schedule:
  run_on_start: true
  crons:
    - "0 2 * * *"
    - "0 14 * * *"
    - "0 23 * * *"
  confirmation_delay: 3m
```

의미:
- 매일 02:00 실행
- 매일 14:00 실행
- 매일 23:00 실행

시간대 기준:
- cron 해석은 **서버 local 시간대** 기준입니다.
- 서버 local 시간이 한국 시간이면 한국 시간 기준으로 동작합니다.

## Redis 연동

Redis를 사용하려면 `config.yaml`의 `redis` 섹션을 채웁니다.

```yaml
app:
  box_ip: 192.168.0.10

redis:
  addr: 10.0.0.20:6379
  password: ""
  db: 0
  publish_interval: 5m
```

동작 방식:

- 서비스 시작 직후 1회 현재 alive stream count를 계산해 Redis에 기록합니다.
- 이후 `redis.publish_interval`마다 alive stream count를 다시 계산해 기록합니다.
- cleanup에서 실제 삭제가 발생하면 별도의 즉시 기록도 수행합니다.
- key는 `stream_count@<app.box_ip>` 형식입니다.
- Redis 저장은 `SET`으로 수행하며 TTL은 두지 않습니다.

예시 key:

```text
stream_count@192.168.0.10
```

## Snapshot API

### 요청

```http
POST /snapshots
Content-Type: application/json
```

```json
{
  "cam_id": "TEST_P1000HDKFH"
}
```

예시:

```bash
curl -X POST http://127.0.0.1:8080/snapshots \
  -H 'Content-Type: application/json' \
  -d '{"cam_id":"TEST_P1000HDKFH"}'
```

### 성공 응답

```json
{
  "cam_id": "TEST_P1000HDKFH",
  "saved_path": "storage/TEST_P1000HDKFH.jpg"
}
```

### 응답 규칙

- 성공: `201 Created`
- 잘못된 JSON: `400 Bad Request`
- `cam_id` 누락: `400 Bad Request`
- 지원하지 않는 메서드: `405 Method Not Allowed`
- go2rtc에서 stream을 찾지 못함: `404 Not Found`
- actor 응답 timeout 등 내부 요청 실패: `504 Gateway Timeout`

파일명은 `cam_id`를 안전한 파일명으로 정리한 뒤 `<snapshot.storage_dir>/<cam_id>.jpg` 형태로 저장합니다. 같은 이름으로 다시 요청하면 같은 파일을 덮어쓸 수 있습니다.

## 테스트

전체 테스트 실행:

```bash
go test ./...
```

단일 테스트 예시:

```bash
go test ./actor -run TestGo2RTCActorHasProducer
```

현재 테스트 범위:

- `actor/Go2RTCActor_test.go` — stream 목록 읽기, producer 판별, backup 생성
- `actor/RedisActor_test.go` — Redis 키/값 기록
- `actor/SnapshotActor_test.go` — snapshot 저장 및 not-found 처리
- `actor/StreamCountActor_test.go` — count-only alive stream 계산
- `config/config_test.go` — `app.box_ip`, cleanup cron, Redis publish interval 검증
- `httpserver/server_test.go` — snapshot HTTP 핸들러 응답

## 문서 동기화

동작, 설정, 구조, contributor guidance가 바뀌면 아래 문서도 함께 갱신해야 합니다.

- `README.md`
- `AGENTS.md`
- `CLAUDE.md`
- `CLAUDE_kr.md`
