mcproxy — Minekube Gate 기반 Minecraft 보안 프록시

요약

mcproxy는 Minekube Gate(Go)를 기반으로 한 Minecraft(Java Edition) 보안 프록시입니다. 관리 Plane을 위한 HTTP API를 제공하고, 데이터 Plane에서 IP/닉네임 기반 과도한 접속 시도(DoS 의심) 차단과 GeoLite2-City.mmdb를 활용한 국가별 접근 제어를 수행합니다. 전역 및 서버별 임계값(Threshold)을 설정할 수 있으며, 저장소는 entgo ORM을 사용해 SQLite(기본) 또는 PostgreSQL을 지원합니다.

핵심 기능

- Minekube Gate(Go) 위에서 동작하는 프록시 계층
- HTTP API 기반 관리 Plane 제공(정책 관리, 임계값 변경, 목록 확인, 감사 기록 열람 등)
- IP 및 닉네임 기준의 접속 폭주(DoS 의심) 요청 차단: 전역 및 서버별 임계값 지원
- GeoLite2-City.mmdb 기반 국가별 Allow/Deny 정책 적용
- 서버별 개별 정책(임계값, Geo 정책, 차단 목록) 관리
- entgo ORM 기반 영속화: 기본 SQLite, 선택적으로 PostgreSQL
- 메모리 기반 정책/룰 snapshot 캐시
- 슬라이딩 윈도우 성격의 메모리 카운터 및 선택적 Redis 분산 동기화
- 비동기 감사 이벤트 파이프라인
- distroless 멀티스테이지 컨테이너 이미지 제공: [Dockerfile](Dockerfile)

현재 구현 상태

- 관리 Plane 초안 구현 완료
  - 서버 관리: [internal/api/router.go](internal/api/router.go)
  - 전역 정책 관리: [internal/api/router.go](internal/api/router.go)
  - 룰 관리 및 접속 평가 API: [internal/api/router.go](internal/api/router.go)
- 데이터 저장/정책 엔진 기반 구현 완료
  - entgo 스키마/마이그레이션: [internal/ent](internal/ent)
  - 정책 평가 및 차단 로직: [internal/store/store.go](internal/store/store.go)
  - 캐시/카운터/분산 동기화: [internal/store/cache.go](internal/store/cache.go), [internal/store/counter_engine.go](internal/store/counter_engine.go), [internal/store/distributed.go](internal/store/distributed.go)
- Minekube Gate 실행 및 접속확인 초안 구현 완료
  - Gate 런타임: [internal/gate/runtime.go](internal/gate/runtime.go)
  - PreLogin 기반 차단 경로 및 Ping 로깅 포함
  - 메인 엔트리포인트 통합: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)

아키텍처 개요

- 데이터 Plane: Minecraft 클라이언트의 접속을 Gate로 수용하고, 백엔드 Minecraft 서버로 라우팅합니다. 연결 수, 닉네임, GeoIP를 체크하여 정책을 평가한 뒤 허용 또는 차단합니다.
- 관리 Plane: 운영자가 HTTP API를 통해 정책 생성/수정/삭제, 임계값 조정, 목록 관리(Allow/Deny), 감사 로그 조회 등을 수행합니다.
- 저장소: entgo ORM 스키마로 정책, 서버, 룰, 카운터(슬라이딩 윈도), 감사 로그 등을 저장합니다.

요구 사항

- 운영체제: Linux, macOS
- Go 버전: 프로젝트의 [go.mod](go.mod) 기준 버전 이상
- GeoIP DB: MaxMind GeoLite2-City.mmdb (MaxMind 계정 및 EULA 준수 필요)
- DB: 기본 SQLite(내장 파일), 또는 PostgreSQL(선택)

설치 및 빌드

- 소스 코드를 가져온 뒤 표준 Go 빌드 도구로 빌드합니다.
- SQLite 기본 모드에서는 별도 DB 서버가 필요 없습니다. 실행 시 데이터 디렉터리가 생성됩니다.
- PostgreSQL 사용 시 연결 문자열을 환경 변수 또는 설정 파일로 제공합니다.

실행 전 준비

- GeoLite2-City.mmdb 파일 경로를 준비합니다. MaxMind EULA에 따라 파일을 내려받고 최신 상태를 유지하십시오.
- 초기 관리자 토큰 또는 API 인증 설정을 준비합니다(아래 보안 섹션 참고).
- 기본 전역 임계값과 서버별 임계값의 초기값을 결정합니다.

설정 방법(개요)

- 실행 옵션과 환경 변수 예시
  - 데이터베이스 종류: sqlite 또는 postgres
  - SQLite 파일 경로: 예) data/mcproxy.db
  - PostgreSQL 연결: 호스트, 포트, 사용자명, 비밀번호, DB명, SSL 모드 등
  - Redis 연결(선택): 예) 127.0.0.1:6379
  - GeoIP DB 경로: 예) data/GeoLite2-City.mmdb
  - HTTP API 바인드 주소: 예) 0.0.0.0:8080
  - Gate 바인드 주소: 예) 0.0.0.0:25565
  - Gate 온라인 모드 여부
  - 관리자 인증 토큰 또는 헤더 키

- 실제 예시 파일: [inc.env](inc.env)

HTTP API 설계(요약)

기본 원칙

- 베이스 경로 예시: /api/v1
- 인증: 고정 토큰 기반 헤더 또는 프록시 앞단의 인증 게이트웨이 사용(권장)
- 응답 포맷: JSON

헬스체크 및 상태

- GET /health: 프로세스 헬스
- GET /api/v1/stats: 전역 통계(요청 수, 차단 수, 활성 카운터 등)

서버 리소스(백엔드 Minecraft 서버 단위)

- POST /api/v1/servers: 서버 등록
  - 주요 필드: 이름, 업스트림 주소, 기본 임계값, Geo 정책 기본값
- GET /api/v1/servers 및 GET /api/v1/servers/{id}: 서버 목록/상세 조회
- PUT /api/v1/servers/{id}: 서버 속성 및 정책 갱신
- DELETE /api/v1/servers/{id}: 서버 삭제(참조 무결성 및 연쇄 정책 처리)

정책 및 임계값(Threshold)

- 전역 정책: GET/PUT /api/v1/policy/global
  - 필드 예시: IP 기준 최대 연결 시도 수(윈도 10초, 60초), 닉네임 기준 최대 연결 시도 수(윈도 10초, 60초), 초과 시 조치(즉시 차단, 소프트 스로틀), 킥 메시지, 평가 순서 옵션
- 서버별 정책: GET/PUT /api/v1/policy/servers/{serverId}
  - 전역 정책을 상속하되 서버별 오버라이드 허용

룰 엔드포인트(Allow/Deny 및 Geo)

- IP Allow/Deny 목록
  - POST/DELETE /api/v1/rules/ip-allow
  - POST/DELETE /api/v1/rules/ip-deny
  - 항목 필드: CIDR 또는 단일 IP, 만료 시각(옵션), 사유, 범위(전역 또는 특정 서버)
- 닉네임 Allow/Deny 목록
  - POST/DELETE /api/v1/rules/name-allow
  - POST/DELETE /api/v1/rules/name-deny
  - 항목 필드: 닉네임, 정규화 옵션(대소문자 무시 등), 만료, 사유, 범위
- Geo 정책
  - GET/PUT /api/v1/rules/geo
  - 모드: 비활성, Allowlist, Denylist
  - 목록: ISO 국가 코드 배열
  - 폴백 동작: 목록에 해당 없을 때 허용 또는 거부
  - 범위: 전역 또는 서버별 오버라이드

평가 순서(권장 기본값)

1) 닉네임 Allow 목록
2) IP Allow 목록
3) 닉네임 Deny 목록
4) IP Deny 목록
5) 임계값 평가(IP, 닉네임 — 전역 → 서버별 순서로 평가, 더 엄격한 결과를 채택)
6) Geo 정책 평가(Allowlist 또는 Denylist)

운영자는 전역 옵션으로 평가 순서를 일부 재정의할 수 있습니다(예: 보안을 위해 Deny 우선 적용). 기본값은 운영 편의와 보안의 균형을 위해 Allow 우선이지만, 높은 보안 환경에서는 Deny 우선을 권장합니다.

차단 동작

- 하드 차단: 즉시 연결 거부 및 킥 메시지 반환
- 소프트 스로틀: 대기 또는 저속 처리 후 거부(옵션)
- 임시 차단: 일정 기간 동안 자동 만료되는 Deny 항목으로 전환

감사 및 가시성

- 모든 정책 변경 및 차단 이벤트에 대한 감사 로그를 저장합니다(시각, 주체, 대상, 이유, 규칙 ID 등).
- 통계 및 메트릭(선택): Prometheus 익스포트 옵션 제공 예정.
- 구조화 요청 로그 및 관리자 인증 실패 로그를 기록합니다: [internal/observability/logging.go](internal/observability/logging.go)

데이터 모델(개념)

- Servers: 이름, 업스트림, 기본 정책, 상태
- Policies: 전역 및 서버별 임계값, 평가 순서, 차단 메시지
- Rules: IP/닉네임 Allow, IP/닉네임 Deny, Geo 정책 항목
- Counters: IP 및 닉네임별 윈도 카운터(10초, 60초 등) — 전역/서버별 스코프 분리
- Audits: 정책 변경 이력과 차단 이벤트

GeoLite2 사용 안내

- MaxMind EULA를 준수해야 하며, 파일 배포 요건을 확인하십시오.
- DB 파일 경로는 실행 시 설정으로 지정합니다. 최신 상태 유지를 위해 주기적 갱신을 권장합니다.

보안 고려 사항

- HTTP API는 신뢰할 수 있는 네트워크 내에서만 노출하십시오.
- API 앞단에 리버스 프록시를 두고 인증/인가, 속도 제한, 감사 로깅을 적용하십시오.
- 관리자 토큰은 주기적으로 교체하고, 최소 권한 원칙을 준수하십시오.

성능 및 운영 팁

- 카운터 저장은 메모리 우선이며, 현재는 버킷 기반 카운터를 사용합니다: [internal/store/counter_engine.go](internal/store/counter_engine.go)
- 정책/룰 평가는 snapshot 캐시를 우선 사용합니다: [internal/store/cache.go](internal/store/cache.go)
- 다중 인스턴스 환경에서는 Redis를 통한 분산 카운터/캐시 무효화를 사용할 수 있습니다: [internal/store/distributed.go](internal/store/distributed.go)
- 애그레시브한 임계값은 정상 사용자에게 영향이 갈 수 있으므로 점진적으로 조정하십시오.
- 백엔드 서버별로 트래픽 특성이 다를 수 있으니 서버별 임계값 튜닝을 활용하십시오.

컨테이너 실행

- 이미지 빌드: `docker build -t mcproxy .`
- 런타임 이미지는 [`gcr.io/distroless/base-debian13:nonroot`](Dockerfile:12) 기반입니다.

마이그레이션 및 스키마(개요)

- entgo 스키마로 테이블이 생성됩니다. 초기 실행 시 자동 마이그레이션 또는 독립 마이그레이션 명령을 제공합니다.
- 환경 전환(개발/스테이징/운영) 시 동일한 스키마 버전을 사용하십시오.

로드맵

- 초기 MVP: 전역/서버별 임계값, IP/닉네임 Allow/Deny, Geo 정책, 기본 감사 로그, SQLite 지원
- 현재 반영: Gate 런타임 초안, PostgreSQL/Redis 운영 예시, 비동기 감사 파이프라인, 메모리 캐시/카운터
- 다음 확장: Prometheus 메트릭, 관리 UI, Gate 이벤트 확장(Login/PostConnect), 분산 카운터 고도화, 서명 기반 세션 전달

라이선스

- AGPL-3.0. 자세한 내용은 [LICENSE.md](LICENSE.md) 참고.

관련 문서

- 영어 요약: [README.md](README.md)
- 진행 현황: [progress.md](progress.md)
- Minekube Gate: https://gate.minekube.com/
