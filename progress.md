# mcproxy Progress

## 진행 현황(완료)
- 프로젝트 초기화 및 문서 작성
  - 개요 및 스펙: [README.ko.md](README.ko.md), [README.md](README.md)
  - 라이선스: [LICENSE.md](LICENSE.md)
- 실행 환경 파일
  - 기본 ENV 예시: [inc.env](inc.env)
- 빌드 및 개발 편의
  - Make 타깃 추가: [Makefile](Makefile) (tools, ent, build, run, test, tidy, clean)
  - 모듈/의존성: [go.mod](go.mod)
- 컨테이너 실행 환경
  - distroless 기반 멀티스테이지 이미지: [Dockerfile](Dockerfile)
- 최소 구동 스캐폴딩
  - 실행 엔트리포인트/서버 기동: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)
  - HTTP API 라우터 스켈레톤(/health, /api/v1/stats): [internal/api/router.go](internal/api/router.go)
  - 환경설정 로더(ENV 기반): [internal/config/config.go](internal/config/config.go)
  - 저장소 스토어 스켈레톤(DB 연결/기본 통계): [internal/store/store.go](internal/store/store.go)
  - GeoLite2 연동 스켈레톤(파일 오픈/국가코드 조회 유틸): [internal/geo/geo.go](internal/geo/geo.go)

## 마일스톤 진행 현황

### M1 완료
- ent 스키마 정의: [internal/ent/schema/server.go](internal/ent/schema/server.go), [internal/ent/schema/policy.go](internal/ent/schema/policy.go), [internal/ent/schema/rule.go](internal/ent/schema/rule.go), [internal/ent/schema/counter.go](internal/ent/schema/counter.go), [internal/ent/schema/audit.go](internal/ent/schema/audit.go)
- ent 코드 생성 및 마이그레이션 경로 확보: [internal/ent/client.go](internal/ent/client.go), [internal/ent/migrate/migrate.go](internal/ent/migrate/migrate.go)
- 저장소에 ent 기반 스키마 생성/CRUD 반영: [internal/store/store.go](internal/store/store.go)
- 관리 API 초안 구현: [internal/api/router.go](internal/api/router.go)
  - 서버 API: ["/api/v1/servers"](internal/api/router.go:58), ["/api/v1/servers/{serverID}"](internal/api/router.go:84)
  - 전역 정책 API: ["/api/v1/policy/global"](internal/api/router.go:127)

### M2 완료
- 룰 관리 API 구현
  - 룰 목록/생성/삭제: ["/api/v1/rules"](internal/api/router.go:148), ["/api/v1/rules/{ruleID}"](internal/api/router.go:173)
- 접속 평가 API 구현
  - 정책/룰/카운터 기반 평가: ["/api/v1/evaluate"](internal/api/router.go:185)
- 카운터 및 통계 확장
  - 카운터 경로: [`bumpCounter()`](internal/store/store.go:771)
  - 룰 매칭 및 임계값 평가: [`matchRules()`](internal/store/store.go:665), [`evaluateThresholds()`](internal/store/store.go:701)
  - 통계 확장: [`Stats()`](internal/store/store.go:223)

### M3 완료(초안)
- Geo 정책을 접속 평가에 통합: [`evaluateGeoPolicy()`](internal/store/store.go:717)
- Minekube Gate 런타임 추가: [internal/gate/runtime.go](internal/gate/runtime.go)
- PreLogin 기반 접속 거부 로직 연결: [`event.Subscribe(...PreLoginEvent...)`](internal/gate/runtime.go:69)
- Ping 기반 접속확인 로깅 연결: [`event.Subscribe(...PingEvent...)`](internal/gate/runtime.go:87)
- 초기 서버 선택 및 서버 동기화 로직 추가: [`pickInitialServer()`](internal/gate/runtime.go:203), [`syncServers()`](internal/gate/runtime.go:135)
- 메인 프로세스에 Gate 기동 통합: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)

### M4 완료
- 관리 API 보안 강화
  - 관리자 API 스로틀: [`middleware.ThrottleBacklog()`](internal/api/router.go:47)
  - 요청 바디 제한: [`limitBody()`](internal/api/router.go:257)
  - 보안 헤더: [`securityHeaders()`](internal/api/router.go:245)
- 구조화 로깅 추가
  - JSON 로거: [`observability.LogJSON()`](internal/observability/logging.go:12)
  - 요청 로그 미들웨어: [`observability.RequestLogger()`](internal/observability/logging.go:29)
  - 시작 로그 추가: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)
- 코드리뷰 수행 및 결과 반영

### M5 완료(초안)
- 정책/룰 snapshot 캐시: [`snapshotCache`](internal/store/cache.go:13)
- 캐시 리프레시: [`refreshSnapshots()`](internal/store/cache.go:47)
- 캐시 기반 읽기 경로: [`cachedGlobalPolicy()`](internal/store/cache.go:92), [`cachedServerPolicy()`](internal/store/cache.go:107), [`cachedRules()`](internal/store/cache.go:126)

### M6 완료(초안)
- 메모리 버킷 카운터: [`bucketCounter`](internal/store/counter_engine.go:9)
- 데이터 플레인 카운트 read path DB 제거: [`bumpCounter()`](internal/store/store.go:771)

### M7 완료(초안)
- Redis 분산 코디네이터: [`distributedCoordinator`](internal/store/distributed.go:11)
- Redis 카운터/캐시 무효화 pubsub: [`IncrWindow()`](internal/store/distributed.go:53), [`PublishInvalidate()`](internal/store/distributed.go:46)
- Redis 설정 추가: [`Config.RedisAddr`](internal/config/config.go:16), [`Config.RedisChannel`](internal/config/config.go:17)

### M8 완료(초안)
- 비동기 감사 이벤트 파이프라인: [`auditWriter`](internal/store/events.go:14)
- 운영 예시 환경을 PostgreSQL/Redis/Gate 기준으로 확장: [inc.env](inc.env)
- Gate/Redis/PostgreSQL 운영 옵션을 설정 로더에 반영: [`LoadFromEnv()`](internal/config/config.go:33)

## 대규모 동시 접속 대응 강화 계획
- 데이터 플레인 성능 최적화
  - 정책/룰/Geo 설정을 DB 직접 조회 대신 메모리 캐시로 유지하고, 관리 API 변경 시 무효화 또는 핫 리로드
  - [`EvaluateAttempt()`](internal/store/store.go:396) 의 DB 의존도를 줄이고 read path를 lock-free 또는 low-lock 구조로 재설계
  - 카운터를 슬라이딩 윈도우 버킷/ring buffer 구조로 지속 고도화
- 분산 확장성
  - 단일 인스턴스 카운터를 Redis 또는 샤딩된 인메모리 카운터로 확장해 다중 프록시 인스턴스 간 임계값 공유
  - Gate 프록시를 여러 대로 수평 확장하고 L4 로드밸런서/Anycast 앞단 배치 검토
  - 서버별 정책/룰 캐시를 pub/sub 기반으로 동기화
- 저장소 및 관리 Plane 분리
  - SQLite는 개발/소규모용으로 두고, 대규모 운영은 PostgreSQL 기본 전환
  - 관리 API와 데이터 플레인 읽기 경로를 분리해 운영 중 정책 변경이 접속 처리에 미치는 영향을 최소화
  - 감사 로그/차단 이벤트는 비동기 큐로 넘겨 쓰기 지연을 줄임
- 네트워크/보안 계층 방어
  - L4/L7 앞단에서 SYN flood, connection flood, per-IP conntrack 제한 적용
  - 관리자 API는 별도 네트워크 또는 VPN 뒤로 분리하고 WAF/리버스 프록시 레이트 리밋 추가
  - GeoIP 외에도 ASN/Hosting Provider 차단, reputation feed 연동 검토
- 관측성 및 운영 자동화
  - Prometheus 메트릭 추가: 활성 연결 수, 차단 사유별 카운트, 서버별 QPS, 정책 캐시 hit ratio, DB latency
  - 경보 기준 정의: 특정 국가/ASN 급증, 서버별 임계값 초과율 상승, 인증 실패 급증
  - 운영 중 임계값 자동 조정(Adaptive threshold) 또는 보호 모드 전환 기능 검토
- 안정성/복원력
  - 장애 시 fail-open/fail-close 정책을 항목별로 선택 가능하게 설계
  - GeoIP DB 갱신 실패, DB 지연, 캐시 미스 폭증에 대한 degraded mode 설계
  - 롤링 배포 중 세션 영향 최소화를 위한 drain/shutdown 전략 정교화

## 다음 작업(진행 계획)
1) Gate 데이터 플레인 고도화
- Gate 이벤트별 정책 적용 확장(PreLogin 외 Login/InitialServer/PostConnect)
- 서버 등록/해제 변경을 더 짧은 주기로 반영하거나 이벤트 기반으로 전환
- 접속 성공/실패에 대한 richer telemetry 및 감사 로그 세분화

2) 운영형 배포 정리
- Docker Compose 또는 Kubernetes 매니페스트 초안(PostgreSQL/Redis/Gate/API)
- distroless 이미지 기준 healthcheck/volume/env 정리
- 운영용 runbook 및 장애 대응 문서화

3) 관측성 확장
- Prometheus 메트릭 익스포터 추가
- 차단 사유별/서버별 지표 수집
- 알림 정책 및 보호 모드 자동 전환 설계

4) 테스트/품질
- 단위 테스트(정책 평가, Geo, 카운터)
- 통합 테스트(HTTP API, DB, Redis, Geo 파일)
- E2E(기본 흐름: Gate 접속 허용/차단)
- CI 설정(GitHub Actions 등): 빌드/테스트/린트/컨테이너 빌드 체크

## 단기 마일스톤(제안)
- M1: ent 스키마(Servers/Policies/Rules/Counters/Audits)와 마이그레이션, /servers 및 /policy API 초안 완료 ✅
- M2: IP/닉네임 임계값 평가 로직 + 카운터 구현, /rules API 완성, /stats 실데이터 반영 ✅
- M3: Geo 정책 적용, Gate 연동으로 최소 접속 흐름 차단/허용 데모 ✅(초안)
- M4: 보안/가시성 보강(인증/메트릭/로그), 안정화 및 문서 갱신 ✅
- M5: 정책/룰/Geo 메모리 캐시 계층 도입 및 관리 API 변경 시 캐시 무효화 ✅(초안)
- M6: 카운터를 슬라이딩 윈도우 버킷 구조로 전환하고 데이터 플레인 read path의 DB 의존 제거 ✅(초안)
- M7: 다중 인스턴스 확장을 위한 Redis 기반 분산 카운터/이벤트 동기화(pub/sub) 도입 ✅(초안)
- M8: PostgreSQL 운영 기본화, 감사 로그/차단 이벤트 비동기 파이프라인 분리 ✅(초안)
- M9: Prometheus 메트릭, 경보 규칙, Adaptive threshold 및 보호 모드 자동 전환 구현
- M10: L4/L7 앞단 연동 전략, ASN/Hosting Provider 차단, 운영용 배포/드레인/복구(runbook 포함) 정리

## 실행/환경 메모
- 기본 DB: SQLite(파일 경로 data/mcproxy.db). PostgreSQL 사용 시 MCPROXY_DB_DRIVER=postgres 및 MCPROXY_POSTGRES_DSN 설정.
- 운영 권장 기본값 예시는 [inc.env](inc.env) 에서 PostgreSQL/Redis/Gate 기준으로 확장.
- GeoLite2-City.mmdb 경로 설정: MCPROXY_GEOIP_PATH
- 관리 API 인증 토큰: MCPROXY_ADMIN_TOKEN (미설정 시 /api/v1 비활성화)
- Redis 분산 동기화(선택): MCPROXY_REDIS_ADDR, MCPROXY_REDIS_CHANNEL
- Minekube Gate 바인드 및 온라인 모드: MCPROXY_GATE_BIND, MCPROXY_GATE_ONLINE_MODE
- HTTP API 바인드: MCPROXY_HTTP_ADDR(기본 127.0.0.1:8080)
- 빌드/실행: `make build`, `make run` (사전 `make tools`로 ent 코드젠 설치 권장)
- 컨테이너 빌드: `docker build -t mcproxy .`

## 참고 파일
- [README.ko.md](README.ko.md)
- [README.md](README.md)
- [Makefile](Makefile)
- [Dockerfile](Dockerfile)
- [inc.env](inc.env)
- [go.mod](go.mod)
- [cmd/mcproxy/main.go](cmd/mcproxy/main.go)
- [internal/api/router.go](internal/api/router.go)
- [internal/config/config.go](internal/config/config.go)
- [internal/gate/runtime.go](internal/gate/runtime.go)
- [internal/store/store.go](internal/store/store.go)
- [internal/store/cache.go](internal/store/cache.go)
- [internal/store/counter_engine.go](internal/store/counter_engine.go)
- [internal/store/distributed.go](internal/store/distributed.go)
- [internal/store/events.go](internal/store/events.go)
- [internal/geo/geo.go](internal/geo/geo.go)
- [internal/observability/logging.go](internal/observability/logging.go)
- [internal/ent/schema/server.go](internal/ent/schema/server.go)
- [internal/ent/schema/policy.go](internal/ent/schema/policy.go)
- [internal/ent/schema/rule.go](internal/ent/schema/rule.go)
- [internal/ent/schema/counter.go](internal/ent/schema/counter.go)
- [internal/ent/schema/audit.go](internal/ent/schema/audit.go)
