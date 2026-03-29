# go2rtc stream cleaner

Proto.Actor 기반으로 재구성한 `go2rtc` 스트림 정리 서비스입니다.
`go2rtc` 설정 파일의 `streams` 목록을 읽고, 각 스트림의 producer 상태를 API로 확인한 뒤 producer가 없는 스트림은 `go2rtc` API를 통해 삭제합니다.
정리 사이클에서 실제 삭제가 발생하면 최종적으로 살아 있는 stream 개수를 Redis의 `steam@count@<router_ip>` 키에 기록할 수 있습니다.

## 폴더 구조

```text
.
├─ main.go                      # 서비스 엔트리포인트, 설정 로드 후 ActorSystem 시작
├─ go.mod                       # Go 모듈 정의 및 직접 의존성
├─ go.sum                       # 의존성 체크섬 잠금 파일
├─ config.yaml                  # 실행 설정값 예시 및 기본 런타임 설정
├─ Docker
│  └─ Dockerfile                # 컨테이너 이미지 빌드 정의
├─ config
│  └─ config.go                 # YAML/환경변수 기반 설정 로드 및 기본값 처리
├─ common
│  └─ message.go                # 액터 간 송수신에 쓰는 공용 메시지 타입
├─ logging
│  └─ logger.go                 # slog 기반 로거 생성 및 로그 레벨 설정
└─ actor
   ├─ MasterActor.go            # 하위 액터 생성과 전체 스케줄링/라우팅 담당
   ├─ StreamCleanerActor.go     # 스트림 점검 흐름과 정리 판단 로직 담당
   ├─ Go2RTCActor.go            # go2rtc API 호출과 스트림 상태 확인/삭제 담당
   ├─ RedisActor.go             # cleanup 완료 후 alive stream 수를 Redis에 기록
   └─ ActionActor.go            # 정리 후속 액션 및 부가 처리 담당
```

## Redis 연동

Redis 저장이 필요하면 `config.yaml`의 `redis` 섹션을 설정합니다.

```yaml
redis:
  addr: 127.0.0.1:6379
  password: ""
  db: 0
  router_ip: 192.168.0.1
```

정리 사이클에서 실제 삭제가 발생하면 `steam@count@192.168.0.1` 같은 키에 현재 살아 있는 stream 개수가 저장됩니다.
