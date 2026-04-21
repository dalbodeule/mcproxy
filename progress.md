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
- 최소 구동 스캐폴딩
  - 실행 엔트리포인트/서버 기동: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)
  - HTTP API 라우터 스켈레톤(/health, /api/v1/stats): [internal/api/router.go](internal/api/router.go)
  - 환경설정 로더(ENV 기반): [internal/config/config.go](internal/config/config.go)
  - 저장소 스토어 스켈레톤(DB 연결/기본 통계): [internal/store/store.go](internal/store/store.go)
  - GeoLite2 연동 스켈레톤(파일 오픈/국가코드 조회 유틸): [internal/geo/geo.go](internal/geo/geo.go)
- M1 완료
  - ent 스키마 정의: [internal/ent/schema/server.go](internal/ent/schema/server.go), [internal/ent/schema/policy.go](internal/ent/schema/policy.go), [internal/ent/schema/rule.go](internal/ent/schema/rule.go), [internal/ent/schema/counter.go](internal/ent/schema/counter.go), [internal/ent/schema/audit.go](internal/ent/schema/audit.go)
  - ent 코드 생성 및 마이그레이션 경로 확보: [internal/ent/client.go](internal/ent/client.go), [internal/ent/migrate/migrate.go](internal/ent/migrate/migrate.go)
  - 저장소에 ent 기반 스키마 생성/CRUD 반영: [internal/store/store.go](internal/store/store.go)
  - 관리 API 초안 구현: [internal/api/router.go](internal/api/router.go)
    - 서버 API: GET/POST ["/api/v1/servers"](internal/api/router.go:53), GET/PUT/DELETE ["/api/v1/servers/{serverID}"](internal/api/router.go:74)
    - 전역 정책 API: GET/PUT ["/api/v1/policy/global"](internal/api/router.go:118)
- M2 진행 중
  - 룰 관리 API 초안 구현
    - 룰 목록/생성/삭제: ["/api/v1/rules"](internal/api/router.go:147), ["/api/v1/rules/{ruleID}"](internal/api/router.go:170)
  - 접속 평가 API 초안 구현
    - 정책/룰/카운터 기반 평가: ["/api/v1/evaluate"](internal/api/router.go:181)
  - 카운터 및 통계 확장
    - 카운터 증가/윈도우 초기화: [`bumpCounter()`](internal/store/store.go:520)
    - 룰 매칭 및 임계값 평가: [`matchRules()`](internal/store/store.go:438), [`evaluateThresholds()`](internal/store/store.go:481)
    - 통계 확장: [`Stats()`](internal/store/store.go:204)
- M4 진행 중
  - 관리 API 보안 강화
    - 관리자 API 스로틀: [`middleware.ThrottleBacklog()`](internal/api/router.go:47)
    - 요청 바디 제한: [`limitBody()`](internal/api/router.go:245)
    - 보안 헤더: [`securityHeaders`](internal/api/router.go:234)
  - 구조화 로깅 추가
    - JSON 로거: [`observability.LogJSON()`](internal/observability/logging.go:12)
    - 요청 로그 미들웨어: [`observability.RequestLogger()`](internal/observability/logging.go:29)

## 다음 작업(진행 계획)
1) 데이터 모델 및 영속화(entgo)
- ent 스키마 정의(초안)
  - Servers(이름, 업스트림, 상태)
  - Policies(전역/서버별 임계값, 평가 순서, 메시지)
  - Rules(IP Allow/Deny, Name Allow/Deny, Geo 정책, 범위/만료/사유)
  - Counters(IP/닉네임별 슬라이딩 윈도 카운터, 전역/서버 스코프)
  - Audits(정책 변경/차단 이벤트 로그)
- ent generate 및 마이그레이션 경로 확립(자동/수동 선택지)

2) HTTP API 확장(관리 Plane)
- /api/v1/servers: 등록/조회/수정/삭제
- /api/v1/policy/global, /api/v1/policy/servers/{id}: 조회/수정
- /api/v1/rules/*: IP/Name Allow/Deny 추가/삭제, Geo 정책 조회/수정
- 요청/응답 스키마 정의 및 유효성 검사, 에러 규격화
- 인증(관리 토큰/리버스 프록시 연동) 및 감사 로그 기록

3) 데이터 Plane 로직(Gate 연동 전 준비)
- 평가 순서에 따른 정책 적용(Allow/Deny → 임계값 → Geo)
- 전역/서버별 임계값 조합 로직 및 초과시 조치(하드 차단/소프트 스로틀)
- 카운터 메커니즘(메모리 우선 + 주기적 영속화) 설계/구현

4) Minekube Gate 연동
- Gate 기반 프록시 부트스트랩(수용 → 백엔드 라우팅)
- 접속 이벤트 훅에 정책 평가 연결
- 킥/거부 메시지 및 텔레메트리 연동

5) 운영/보안/가시성
- API 인증/인가 강화(토큰 롤링, 최소 권한)
- 레이트 리밋(관리 API)
- 로깅(구조화), 메트릭(Prometheus) 익스포트
- 구성값 재적재(핫 리로드 여부 검토)

6) 테스트/품질
- 단위 테스트(정책 평가, Geo, 카운터)
- 통합 테스트(HTTP API, DB, Geo 파일)
- E2E(기본 흐름: 접속 허용/차단)
- CI 설정(GitHub Actions 등): 빌드/테스트/린트/라이선스 체크

7) 대규모 동시 접속 대응 강화 계획
- 데이터 플레인 성능 최적화
  - 정책/룰/Geo 설정을 DB 직접 조회 대신 메모리 캐시로 유지하고, 관리 API 변경 시 무효화 또는 핫 리로드
  - [`EvaluateAttempt()`](internal/store/store.go:329) 의 DB 의존도를 줄이고 read path를 lock-free 또는 low-lock 구조로 재설계
  - 카운터는 현재 [`bumpCounter()`](internal/store/store.go:520) 의 단순 last-seen 방식에서 슬라이딩 윈도우 버킷/ring buffer 방식으로 전환
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

## 단기 마일스톤(제안)
- M1: ent 스키마(Servers/Policies/Rules/Counters/Audits)와 마이그레이션, /servers 및 /policy API 초안 완료 ✅
- M2: IP/닉네임 임계값 평가 로직 + 카운터 구현, /rules API 완성, /stats 실데이터 반영 🔄
- M3: Geo 정책 적용, Gate 연동으로 최소 접속 흐름 차단/허용 데모
- M4: 보안/가시성 보강(인증/메트릭/로그), 안정화 및 문서 갱신
- M5: 정책/룰/Geo 메모리 캐시 계층 도입 및 관리 API 변경 시 캐시 무효화
- M6: 카운터를 슬라이딩 윈도우 버킷 구조로 전환하고 데이터 플레인 read path의 DB 의존 제거
- M7: 다중 인스턴스 확장을 위한 Redis 기반 분산 카운터/이벤트 동기화(pub/sub) 도입
- M8: PostgreSQL 운영 기본화, 감사 로그/차단 이벤트 비동기 파이프라인 분리
- M9: Prometheus 메트릭, 경보 규칙, Adaptive threshold 및 보호 모드 자동 전환 구현
- M10: L4/L7 앞단 연동 전략, ASN/Hosting Provider 차단, 운영용 배포/드레인/복구(runbook 포함) 정리

## 실행/환경 메모
- 기본 DB: SQLite(파일 경로 data/mcproxy.db). PostgreSQL 사용 시 MCPROXY_DB_DRIVER=postgres 및 MCPROXY_POSTGRES_DSN 설정.
- GeoLite2-City.mmdb 경로 설정: MCPROXY_GEOIP_PATH
- 관리 API 인증 토큰: MCPROXY_ADMIN_TOKEN (미설정 시 /api/v1 비활성화)
- HTTP API 바인드: MCPROXY_HTTP_ADDR(기본 127.0.0.1:8080)
- 빌드/실행: `make build`, `make run` (사전 `make tools`로 ent 코드젠 설치 권장)

## 참고 파일
- [README.ko.md](README.ko.md)
- [README.md](README.md)
- [Makefile](Makefile)
- [inc.env](inc.env)
- [go.mod](go.mod)
- [cmd/mcproxy/main.go](cmd/mcproxy/main.go)
- [internal/api/router.go](internal/api/router.go)
- [internal/config/config.go](internal/config/config.go)
- [internal/store/store.go](internal/store/store.go)
- [internal/geo/geo.go](internal/geo/geo.go)
- [internal/ent/schema/server.go](internal/ent/schema/server.go)
- [internal/ent/schema/policy.go](internal/ent/schema/policy.go)
- [internal/ent/schema/rule.go](internal/ent/schema/rule.go)
- [internal/ent/schema/counter.go](internal/ent/schema/counter.go)
- [internal/ent/schema/audit.go](internal/ent/schema/audit.go)
