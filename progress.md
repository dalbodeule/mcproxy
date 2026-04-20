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

## 단기 마일스톤(제안)
- M1: ent 스키마(Servers/Policies/Rules/Counters/Audits)와 마이그레이션, /servers 및 /policy API 초안 완료 ✅
- M2: IP/닉네임 임계값 평가 로직 + 카운터 구현, /rules API 완성, /stats 실데이터 반영
- M3: Geo 정책 적용, Gate 연동으로 최소 접속 흐름 차단/허용 데모
- M4: 보안/가시성 보강(인증/메트릭/로그), 안정화 및 문서 갱신

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
