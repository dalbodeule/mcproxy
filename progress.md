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
  - HTTP API 라우터: [internal/api/router.go](internal/api/router.go)
  - 환경설정 로더: [internal/config/config.go](internal/config/config.go)
  - 저장소/정책 평가 엔진: [internal/store/store.go](internal/store/store.go)
  - GeoLite2 연동: [internal/geo/geo.go](internal/geo/geo.go)
  - 로깅: [internal/observability/logging.go](internal/observability/logging.go)

## 마일스톤 진행 현황

### M1 완료
- ent 스키마 정의: [internal/ent/schema/server.go](internal/ent/schema/server.go), [internal/ent/schema/policy.go](internal/ent/schema/policy.go), [internal/ent/schema/rule.go](internal/ent/schema/rule.go), [internal/ent/schema/counter.go](internal/ent/schema/counter.go), [internal/ent/schema/audit.go](internal/ent/schema/audit.go)
- ent 코드 생성 및 마이그레이션 경로 확보: [internal/ent/client.go](internal/ent/client.go), [internal/ent/migrate/migrate.go](internal/ent/migrate/migrate.go)
- 저장소에 ent 기반 스키마 생성/CRUD 반영: [internal/store/store.go](internal/store/store.go)
- 관리 API 초안 구현: [internal/api/router.go](internal/api/router.go)

### M2 완료
- 룰 관리 API 구현
  - 룰 목록/생성/삭제: ["/api/v1/rules"](internal/api/router.go:153), ["/api/v1/rules/{ruleID}"](internal/api/router.go:178)
- 접속 평가 API 구현
  - 정책/룰/카운터 기반 평가: ["/api/v1/evaluate"](internal/api/router.go:190)
- 카운터 및 통계 확장
  - 카운터 경로: [`bumpCounter()`](internal/store/store.go:795)
  - 룰 매칭 및 임계값 평가: [`matchRules()`](internal/store/store.go:689), [`evaluateThresholds()`](internal/store/store.go:725)
  - 통계 확장: [`Stats()`](internal/store/store.go:235)

### M3 완료(초안)
- Geo 정책을 접속 평가에 통합: [`evaluateGeoPolicy()`](internal/store/store.go:741)
- Minekube Gate 런타임 추가: [internal/gate/runtime.go](internal/gate/runtime.go)
- PreLogin 기반 접속 거부 로직 연결: [`event.Subscribe(...PreLoginEvent...)`](internal/gate/runtime.go:69)
- Ping 기반 접속확인 로깅 연결: [`event.Subscribe(...PingEvent...)`](internal/gate/runtime.go:87)
- 초기 서버 선택 및 서버 동기화 로직 추가: [`pickInitialServer()`](internal/gate/runtime.go:203), [`syncServers()`](internal/gate/runtime.go:135)
- 메인 프로세스에 Gate 기동 통합: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)

### M4 완료
- 관리 API 보안 강화
  - 관리자 API 스로틀: [`middleware.ThrottleBacklog()`](internal/api/router.go:48)
  - 요청 바디 제한: [`limitBody()`](internal/api/router.go:248)
  - 보안 헤더: [`securityHeaders()`](internal/api/router.go:238)
- 구조화 로깅 추가
  - JSON/TEXT + Loki fanout 로거: [internal/observability/logging.go](internal/observability/logging.go)
  - 요청 로그 미들웨어: [`RequestLogger()`](internal/observability/logging.go:77)
  - 애플리케이션 시작/오류 로깅: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)
- 코드리뷰 수행 및 결과 반영

### M5 완료(초안)
- 정책/룰 snapshot 캐시: [`snapshotCache`](internal/store/cache.go:13)
- 캐시 리프레시: [`refreshSnapshots()`](internal/store/cache.go:47)
- 캐시 기반 읽기 경로: [`cachedGlobalPolicy()`](internal/store/cache.go:92), [`cachedServerPolicy()`](internal/store/cache.go:107), [`cachedRules()`](internal/store/cache.go:126)

### M6 완료(초안)
- 메모리 버킷 카운터: [`bucketCounter`](internal/store/counter_engine.go:9)
- 데이터 플레인 카운트 read path DB 제거: [`bumpCounter()`](internal/store/store.go:795)

### M7 완료(초안)
- Redis 분산 코디네이터: [`distributedCoordinator`](internal/store/distributed.go:11)
- Redis 카운터/캐시 무효화 pubsub: [`IncrWindow()`](internal/store/distributed.go:53), [`PublishInvalidate()`](internal/store/distributed.go:46)
- Redis 설정 추가: [`Config.RedisAddr`](internal/config/config.go:16), [`Config.RedisChannel`](internal/config/config.go:17)

### M8 완료(초안)
- 비동기 감사 이벤트 파이프라인: [`auditWriter`](internal/store/events.go:14)
- 운영 예시 환경을 PostgreSQL/Redis/Gate 기준으로 확장: [inc.env](inc.env)
- SQLite foreign key 및 DSN 경로 처리 보강: [`sqliteDSN()`](internal/config/config.go:103), [`store.Open()`](internal/store/store.go:128)

## 현재 남은 핵심 작업

### 1) Gate 데이터 플레인 고도화
- 현재 Gate 연동은 [`PreLoginEvent`](internal/gate/runtime.go:69), [`PingEvent`](internal/gate/runtime.go:87), 초기 서버 선택 중심이다.
- 다음 작업:
  - Login 이후 단계 차단/허용 로직 추가
  - 서버 전환 이후(PostConnect 계열) 검증 로직 추가
  - 접속 성공/실패에 대한 감사 로그 세분화
  - 서버 동기화를 polling 기반에서 이벤트 기반 또는 더 정교한 방식으로 개선

#### 현재 테스트 상태
- [x] Gate 최소 통합 테스트 추가: [internal/gate/runtime_test.go](internal/gate/runtime_test.go)
  - 빈 서버 목록일 때 초기화 실패 검증
  - backend 서버 등록 후 [`syncServers()`](internal/gate/runtime.go:135), [`pickInitialServer()`](internal/gate/runtime.go:203) 검증

### 2) 서버별 정책 API 구현
- 현재 전역 정책 API는 있으나 서버별 Threshold API는 없다.
- 다음 작업:
  - [x] ["/api/v1/policy/servers/{id}"](internal/api/router.go) 조회/업서트 추가
  - [ ] 서버별 정책 삭제/초기화 API 추가
  - [ ] Gate 서버 선택 시 서버별 정책이 실제 적용되도록 연결 고도화

### 3) Geo 정책 관리 API 구현
- 평가 로직은 [`evaluateGeoPolicy()`](internal/store/store.go:741)에 있으나 관리 API는 부족하다.
- 다음 작업:
  - [x] Geo allow/deny 목록 전용 관리 엔드포인트 추가: ["/api/v1/policy/geo"](internal/api/router.go:180)
  - [x] 서버별 Geo override 관리 추가: `server_id` query 기반 ["/api/v1/policy/geo"](internal/api/router.go:180)
  - [ ] Geo 정책 삭제/초기화 API 추가

### 4) 카운터 정밀도 고도화
- 현재 [`bucketCounter`](internal/store/counter_engine.go:9)는 초안 수준 구현이다.
- 다음 작업:
  - 더 세밀한 슬라이딩 윈도우 구현
  - 키 만료/정리 정책
  - Redis 분산 카운터와 로컬 카운터의 일관성 전략 문서화

### 5) 테스트 확충
- 현재 빌드 검증 중심이며 테스트는 거의 없다.
- 우선순위 높은 테스트:
  - [x] [`matchRule()`](internal/store/store.go:708), [`matchRules()`](internal/store/store.go:689), [`evaluateThresholds()`](internal/store/store.go:725), [`evaluateGeoPolicy()`](internal/store/store.go:741), [`normalizeCountryCodes()`](internal/store/store.go:639) 단위 테스트 추가: [internal/store/store_test.go](internal/store/store_test.go)
  - [ ] [`EvaluateAttempt()`](internal/store/store.go:420) 저장소/캐시/카운터 통합 단위 테스트
  - [x] HTTP API 통합 테스트 추가: [internal/api/router_test.go](internal/api/router_test.go)
  - [x] Gate 최소 통합 테스트 추가: [internal/gate/runtime_test.go](internal/gate/runtime_test.go)
  - [ ] Gate 연동 E2E 테스트
  - [ ] 테스트 헬퍼/fixture 공통화

### 6) 운영형 배포 정리
- [Dockerfile](Dockerfile)은 추가됐지만 실운영 배포 세트는 아직 없다.
- 다음 작업:
  - Docker Compose 또는 Kubernetes 매니페스트 초안
  - PostgreSQL/Redis/Loki 포함 운영 스택 예시
  - runbook 및 장애 대응 절차 문서화

### 7) 관측성 확장
- [`internal/observability/logging.go`](internal/observability/logging.go)은 정리됐지만 메트릭은 아직 없다.
- 다음 작업:
  - Prometheus 메트릭 익스포터
  - 차단 사유별/서버별 지표 수집
  - 알림 정책 및 보호 모드 자동 전환 설계

### 8) 코드 구조 리팩토링
- [`internal/store/store.go`](internal/store/store.go)에 책임이 집중되어 있다.
- 분리 대상:
  - 정책 저장소
  - 평가 엔진
  - 카운터 엔진
  - 감사 이벤트 writer
  - 분산 코디네이터

## 대규모 동시 접속 대응 강화 계획
- 데이터 플레인 성능 최적화
  - 정책/룰/Geo 설정을 DB 직접 조회 대신 메모리 캐시로 유지하고, 관리 API 변경 시 무효화 또는 핫 리로드
  - [`EvaluateAttempt()`](internal/store/store.go:420)의 read path를 lock-free 또는 low-lock 구조로 재설계
  - 카운터를 슬라이딩 윈도우 버킷/ring buffer 구조로 지속 고도화
- 분산 확장성
  - Redis 또는 샤딩된 인메모리 카운터로 다중 프록시 인스턴스 간 임계값 공유
  - Gate 프록시 수평 확장 및 L4 로드밸런서/Anycast 앞단 배치 검토
  - 서버별 정책/룰 캐시를 pub/sub 기반으로 동기화
- 저장소 및 관리 Plane 분리
  - SQLite는 개발/소규모용, 대규모 운영은 PostgreSQL 기본
  - 관리 API와 데이터 플레인 읽기 경로 분리
  - 감사 로그/차단 이벤트 비동기 큐 처리
- 네트워크/보안 계층 방어
  - SYN flood, connection flood, per-IP conntrack 제한 적용
  - 관리자 API는 별도 네트워크/VPN 뒤로 분리
  - ASN/Hosting Provider 차단, reputation feed 연동 검토
- 관측성 및 운영 자동화
  - 활성 연결 수, 차단 사유별 카운트, 서버별 QPS, 정책 캐시 hit ratio, DB latency 메트릭 추가
  - 경보 기준 정의 및 Adaptive threshold 검토
- 안정성/복원력
  - fail-open/fail-close 정책 선택
  - degraded mode 설계
  - drain/shutdown 전략 정교화

## 권장 다음 작업 순서
1. 서버별 정책 API + Gate 데이터 플레인 적용
2. Geo 관리 API + 핵심 테스트 작성
3. Prometheus 메트릭 + 운영 배포 문서
4. Store 책임 분리 리팩토링

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
- 운영 권장 기본값 예시는 [inc.env](inc.env) 기준으로 조정.
- GeoLite2-City.mmdb 경로 설정: MCPROXY_GEOIP_PATH
- 관리 API 인증 토큰: MCPROXY_ADMIN_TOKEN (미설정 시 /api/v1 비활성화)
- Redis 분산 동기화(선택): MCPROXY_REDIS_ADDR, MCPROXY_REDIS_CHANNEL
- Loki 로깅(선택): MCPROXY_LOKI_HOST, MCPROXY_LOG_IDENTIFY
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
