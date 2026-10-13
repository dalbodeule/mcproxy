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
- 서버별 정책 API 구현
  - 조회/업서트: ["/api/v1/policy/servers/{serverID}"](internal/api/router.go:151)
- Geo 정책 관리 API 구현
  - 조회/업서트: ["/api/v1/policy/geo"](internal/api/router.go:180)
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

### 운영 보강 반영
- [x] 설정 로그에서 DSN/비밀번호 노출 방지
- [x] `/ready` DB readiness와 기본 Prometheus `/metrics` 추가
- [x] 엄격한 JSON 단일 값 디코딩, 서버 upstream/public address 및 정책 입력 검증
- [x] 별도 관리패널 연결용 exact-origin CORS 설정
- [x] nonroot 컨테이너의 SQLite 쓰기 경로(`/data`)와 PostgreSQL/Redis Compose 스택
- [x] 별도 SvelteKit 3+·TailwindCSS 패널의 서버사이드 프록시 운영 지침
- [x] 독립 `admin-panel/` 앱: 대시보드, 서버 등록/활성화/비활성화/삭제, API reverse proxy
- [x] 감사로그 목록 API와 관리패널 최근 감사 이벤트 화면
- [x] 관리패널 전역 임계값·Geo 정책·Allow/Deny 룰 CRUD 화면
- [x] 관리패널 서버별 Geo 정책·Allow/Deny 룰 CRUD 화면
- [x] 관리패널 자체 로그인, 서명 세션 쿠키, 로그아웃 및 API proxy 보호
- [x] 서버별 정책 편집, 감사로그 이벤트 필터·페이지네이션, 차단 사유별 Prometheus 지표
- [x] 관리자 CRUD와 Gate 연결/해제/백엔드 거부 이벤트의 감사 기록
- [x] 서버 삭제 시 정책·룰·카운터를 트랜잭션으로 정리하고 감사 이력은 보존
- [x] distroless용 바이너리 healthcheck와 Go·관리패널·Compose GitHub Actions CI
- [x] 별도 SvelteKit 관리패널 컨테이너 bind 설정과 로그인 페이지 healthcheck
- [x] 외부 PostgreSQL/Redis와 Secret 주입을 전제로 한 Kubernetes 배포 템플릿
- [x] Kubernetes 관리 API·메트릭 내부 접근 및 패널 ingress 제한 NetworkPolicy 예시
- [x] 확인 토큰을 요구하는 PostgreSQL custom-format 복구 스크립트 및 runbook 연결
- [x] Go/npm/Docker/GitHub Actions 의존성 자동 업데이트 설정(Dependabot)
- [x] 관리패널 기본 보안 응답 헤더(X-Frame-Options, nosniff, Referrer-Policy, Permissions-Policy)
- [x] CI에서 전체 Go race test 수행
- [x] SIGTERM 시 HTTP와 Gate 종료를 함께 기다리는 graceful shutdown 경로
- [x] 백엔드 0개 상태에서도 Gate를 유지해 관리패널의 재활성화를 자동 반영
- [x] Gate upstream 동기화 전 사전 해석으로 잘못된 신규 설정이 기존 라우팅을 깨뜨리지 않도록 보호
- [x] 독립 Docker 실행 시 HTTP 외부 바인드 및 `/data/mcproxy.db` 기본 경로 보정
- [x] `/ready`가 DB뿐 아니라 Gate 데이터 플레인 기동 상태도 반영
- [x] Gate readiness Prometheus 지표 및 비정상 알림 규칙 추가
- [x] 실제 Minecraft Status Handshake/Ping 컨테이너 E2E smoke test 및 CI 타깃 추가
- [x] 로컬 카운터의 만료 버킷 정리 및 메모리 누수 회귀 테스트
- [x] 평가 요청·룰 target 입력 길이 제한으로 메모리/Redis 키 남용 방지
- [x] Redis 분산 카운터의 INCR/EXPIRE 원자적 처리
- [x] Compose의 로컬 `.env` 의존성을 선택적으로 변경해 CI/신규 호스트 검증 개선
- [x] PostgreSQL/Redis 포함 Compose 기동 및 readiness smoke test 타깃/CI 추가
- [x] PostgreSQL custom-format 백업→별도 DB 복구→fixture 검증 smoke 및 CI 타깃 추가
- [x] Compose smoke를 전용 override/고유 프로젝트로 격리해 운영 포트·볼륨 충돌 방지
- [x] 별도 관리패널 이미지 런타임·로그인 endpoint·보안 헤더 smoke test 타깃/CI 추가
- [x] 서버별 BungeeGuard allowed-token 자동 발급·회전 API 및 관리패널 추가
- [x] 공식 BungeeGuard 설정 키(`allowed-tokens`)와 reload 절차를 문서·관리패널에 반영
- [x] 서버별 `forwarding_mode`로 BungeeGuard/legacy/none 전달 방식을 독립 설정하고 Ent migration·API·관리패널에 연결
- [x] Redis/local control-plane invalidation을 Gate backend 즉시 동기화에 연결하고 5초 polling을 recovery path로 유지
- [x] `mcproxy migrate` 명령과 운영 백업→마이그레이션→readiness runbook 추가
- [x] 배포 이미지 내부 `mcproxy migrate` migration smoke 검증을 CI에 추가
- [x] Kubernetes `/data`를 `emptyDir`에서 PersistentVolumeClaim으로 변경
- [x] Go 1.27 `encoding/json`와 Minecraft component reason 충돌로 인한 감사 로그 panic 차단
- [x] Minecraft 26.3(protocol 777) Gate 로그인 hello 회귀 대응 및 재배포 문서화
- [x] PostgreSQL Compose smoke에서 발견된 pgx/database-sql 드라이버명 불일치 수정

### 1) Gate 데이터 플레인 고도화
- 현재 Gate 연동은 [`PreLoginEvent`](internal/gate/runtime.go:69), [`PingEvent`](internal/gate/runtime.go:87), 초기 서버 선택과 연결 lifecycle 감사 이벤트를 지원한다.
- `public address/hostname -> backend upstream` 라우팅, 서버별 정책 평가와 우선순위 기반 초기 서버 선택이 구현되어 있다.
- 다음 작업:
  - [x] Login 이후 단계 차단/허용 로직 추가
  - [x] 서버 전환 이전(ServerPreConnect) 서버별 정책·Geo 검증 추가
  - [x] 접속 성공/실패 및 백엔드 거부 감사 로그 세분화
  - [x] 서버 동기화를 control-plane invalidation 이벤트 기반으로 개선하고 5초 polling을 recovery path로 유지
  - [x] Redis를 사용하는 다중 Gate 인스턴스에서 서버 목록/라우팅 변경을 즉시 동기화

#### 현재 테스트 상태
- [x] Gate 최소 통합 테스트 추가: [internal/gate/runtime_test.go](internal/gate/runtime_test.go)
  - 빈 서버 목록에서 idle runtime으로 기동하고 이후 백엔드 추가를 동기화하는 검증
  - backend 서버 등록 후 [`syncServers()`](internal/gate/runtime.go:135), [`pickInitialServer()`](internal/gate/runtime.go:203) 검증

### 2) 정책 관리 API 고도화
- 현재 서버별 정책 API와 Geo 정책 API의 조회/업서트 및 Gate 평가 연결이 완료되었다.
- 남은 작업:
  - [x] Gate 서버 선택/전환 lifecycle 전체에 대한 E2E 검증: 실제 TCP backend 2개와 Minecraft handshake/login으로 hostname별 초기 서버 선택 및 오선택 방지를 검증

### 3) Public address / hostname 라우팅 계층 추가
- 목표 구조:
  - `client -(public address)-> gate -(internal/private/unknown upstream)-> minecraft server`
- 필요한 작업:
  - [x] 서버 엔티티에 `public_host` / `public_addr` / `priority` / `enabled` 필드 도입
  - [x] Gate에서 [`VirtualHost`](internal/gate/runtime.go:87) 기준으로 backend upstream을 선택하는 라우터 추가
  - [x] 관리 API에서 public address → upstream 매핑 CRUD 제공
- [x] 서버별 `ping_motd`를 저장하고 hostname 기준 Ping 응답에 반영

### 4) 카운터 정밀도 고도화
- 현재 [`bucketCounter`](internal/store/counter_engine.go:9)는 버킷 기반 카운터이며 만료 키 정리까지 구현되어 있다.
- 다음 작업:
  - 더 세밀한 슬라이딩 윈도우 구현
  - [x] 키 만료/정리 정책
  - Redis 분산 카운터와 로컬 카운터의 일관성 전략 문서화

### 5) 테스트 확충
- Go 단위/통합 테스트와 HTTP/Gate 기본 통합 테스트가 구성되어 있다.
- 우선순위 높은 테스트:
  - [x] [`matchRule()`](internal/store/store.go:708), [`matchRules()`](internal/store/store.go:689), [`evaluateThresholds()`](internal/store/store.go:725), [`evaluateGeoPolicy()`](internal/store/store.go:741), [`normalizeCountryCodes()`](internal/store/store.go:639) 단위 테스트 추가: [internal/store/store_test.go](internal/store/store_test.go)
  - [x] [`EvaluateAttempt()`](internal/store/store.go:420) 저장소/캐시/카운터 통합 단위 테스트
  - [x] HTTP API 통합 테스트 추가: [internal/api/router_test.go](internal/api/router_test.go)
  - [x] Gate 최소 통합 테스트 추가: [internal/gate/runtime_test.go](internal/gate/runtime_test.go)
  - [x] Gate Status handshake/Ping 컨테이너 E2E 테스트
  - [x] 테스트 헬퍼/fixture 공통화: [internal/testutil/testenv.go](internal/testutil/testenv.go)

### 6) 운영형 배포 정리
- [Dockerfile](Dockerfile), Compose 운영 스택, 백업 스크립트와 runbook이 구성되어 있다.
- 다음 작업:
  - provider별 External SecretStore와 NetworkPolicy egress 규칙을 운영 클러스터에 맞게 연결
  - [x] PostgreSQL 복구 훈련 자동화: `make postgres-restore-smoke`

### 7) 관측성 확장
- 기본 Prometheus `/metrics`와 차단 사유별 지표가 구현되어 있다.
- 다음 작업:
  - [x] Prometheus scrape 설정과 기본 장애/폭주 경보 규칙 추가
  - [x] Compose mcproxy 서비스에 read-only rootfs, no-new-privileges, capability 최소화 적용
- [x] 관리패널 로그아웃 POST-only 및 COOP/CORP/HSTS 조건부 보안 헤더 적용
- [x] 민감 설정의 Docker/Kubernetes Secret 파일 주입(`*_FILE`) 지원 추가
- [x] SvelteKit 관리패널도 API 토큰·로그인·세션 secret의 `*_FILE` 주입 지원
- [x] Gate 현재 접속자 수와 등록 backend 수를 Prometheus metrics에 추가
- [x] Gate ready 상태에서 backend 0개를 감지하는 Prometheus critical alert 추가
- [x] 운영 Docker/Compose 기반 이미지를 검증된 digest로 고정해 재현 가능한 배포 구성
- [x] Kubernetes workload의 불필요한 ServiceAccount 토큰 자동 마운트 차단 및 관리패널 PDB 추가
- [x] 공급자 중립적인 External Secrets Operator 예시와 SecretStore 교체 절차 문서화
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
  - 활성 연결 수, 서버별 QPS, 정책 캐시 hit ratio, DB latency 메트릭 추가
  - 경보 기준 정의 및 Adaptive threshold 검토
- 안정성/복원력
  - fail-open/fail-close 정책 선택
  - degraded mode 설계
  - drain/shutdown 전략 정교화

## 권장 다음 작업 순서
1. Prometheus 경보 규칙과 보호 모드 자동 전환 설계
2. 외부 Secret Manager 및 네트워크 정책 운영 연계
3. Store 책임 분리 리팩토링

## 지금 바로 먼저 시작할 작업
- 1순위: Prometheus 경보와 보호 모드 자동 전환 설계
- 이유:
  - 관리 plane과 데이터 plane의 실제 연동을 배포 전 확인해야 한다.

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
- 관리 API 인증 실패 제한: 소스 IP별 분당 20회 초과 시 429/Retry-After 응답
- Redis 분산 동기화(선택): MCPROXY_REDIS_ADDR, MCPROXY_REDIS_CHANNEL
- Redis 인증/TLS: MCPROXY_REDIS_PASSWORD, MCPROXY_REDIS_TLS 및 Compose 인증 healthcheck
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
