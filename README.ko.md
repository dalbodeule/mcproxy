mcproxy — Minekube Gate 기반 Minecraft 보안 프록시

요약

mcproxy는 Minekube Gate(Go)를 기반으로 한 Minecraft(Java Edition) 보안 프록시입니다. 관리 Plane을 위한 HTTP API를 제공하고, 데이터 Plane에서 IP/닉네임 기반 과도한 접속 시도(DoS 의심) 차단과 GeoLite2-City.mmdb를 활용한 국가별 접근 제어를 수행합니다. 전역 및 서버별 임계값(Threshold)을 설정할 수 있으며, 저장소는 entgo ORM을 사용해 SQLite(기본) 또는 PostgreSQL을 지원합니다.

신뢰 경계, 운영 필수 보안 설정, 알려진 한계와 취약점 신고 절차는 [SECURITY.md](SECURITY.md)를 참고하십시오.

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

- 핵심 구현과 배포 rehearsal은 완료되어 제한된 staging 배포를 진행할 수 있습니다. 다만 호스팅 서비스는 아니므로 TLS, SecretStore, 방화벽, GeoIP, egress CIDR, Prometheus threshold는 운영자가 환경에 맞게 설정해야 합니다.
- 관리 Plane 초안 구현 완료
  - 서버 관리: [internal/api/router.go](internal/api/router.go)
  - 전역 정책 관리: [internal/api/router.go](internal/api/router.go)
  - 룰 관리 및 접속 평가 API: [internal/api/router.go](internal/api/router.go)
- 데이터 저장/정책 엔진 기반 구현 완료
  - entgo 스키마/마이그레이션: [internal/ent](internal/ent)
  - 정책 평가 및 차단 로직: [internal/store/store.go](internal/store/store.go)
  - 캐시/카운터/분산 동기화: [internal/store/cache.go](internal/store/cache.go), [internal/store/counter_engine.go](internal/store/counter_engine.go), [internal/store/distributed.go](internal/store/distributed.go)
- Minekube Gate 실행 및 연결 lifecycle 연동 구현 완료
  - Gate 런타임: [internal/gate/runtime.go](internal/gate/runtime.go)
  - PreLogin/Login/ServerPreConnect 정책 차단, Ping 라우팅, 연결·해제·백엔드 거부 감사 이벤트 포함
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
  - 선택 ASN DB 경로: 예) data/GeoLite2-ASN.mmdb (`asn-allow`/`asn-deny` 룰용)
  - HTTP API 바인드 주소: 예) 0.0.0.0:8080
  - Gate 바인드 주소: 예) 0.0.0.0:25565
  - Gate 온라인 모드 여부
  - 관리자 인증 토큰 또는 헤더 키
  - `MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP` (선택적 IP별 동시 로그인 제한)
  - `MCPROXY_SQLITE_PATH` (SQLite 경로, 기본 `data/mcproxy.db`)
  - `MCPROXY_ADMIN_ACTOR_SECRET` 및 `MCPROXY_ADMIN_THROTTLE` (패널 감사 actor 서명·API backlog 제한)
  - `MCPROXY_PING_CACHE_TTL_SEC`, `MCPROXY_PING_FALLBACK_MOTD`, `MCPROXY_PING_OFFLINE_MOTD`, `MCPROXY_PING_FALLBACK_FAVICON` (서버 목록 ping fallback)
  - `MCPROXY_LOKI_HOST`, `MCPROXY_LOG_IDENTIFY` (선택적 Loki 로깅)

주요 환경 변수 이름은 `MCPROXY_HTTP_ADDR`, `MCPROXY_GATE_BIND`,
`MCPROXY_GATE_ONLINE_MODE`, `MCPROXY_GATE_PROXY_PROTOCOL`,
`MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES`, `MCPROXY_GATE_PROXY_PROTOCOL_BACKEND`,
`MCPROXY_GATE_FORWARDING_MODE`, `MCPROXY_GATE_VELOCITY_SECRET`,
`MCPROXY_PING_CACHE_TTL_SEC`, `MCPROXY_PING_FALLBACK_MOTD`,
`MCPROXY_PING_OFFLINE_MOTD`, `MCPROXY_PING_FALLBACK_FAVICON`,
`MCPROXY_ADMIN_TOKEN`, `MCPROXY_ADMIN_ALLOWED_ORIGINS`,
`MCPROXY_TRUST_PROXY_HEADERS`, `MCPROXY_TRUSTED_PROXY_CIDRS`,
`MCPROXY_DB_DRIVER`, `MCPROXY_POSTGRES_DSN`, `MCPROXY_REDIS_ADDR`,
`MCPROXY_REDIS_PASSWORD`, `MCPROXY_REDIS_TLS`, `MCPROXY_REDIS_CHANNEL`,
`MCPROXY_REDIS_FAIL_CLOSED`, `MCPROXY_GEOIP_PATH`, `MCPROXY_GEOIP_ASN_PATH`,
`MCPROXY_REQUIRE_POSTGRES_TLS`, `MCPROXY_REQUIRE_REDIS_TLS`입니다.

- 실제 예시 파일: [inc.env](inc.env)

Gate 백엔드 연결

- Go 소스와 컨테이너 빌드는 Go 1.27을 사용합니다.
- `MCPROXY_GATE_FORWARDING_MODE`는 `none`, `legacy`, `velocity`를 지원하며 기본값은 `legacy`입니다. BungeeGuard는 전역값으로 설정할 수 없고 반드시 각 서버의 `forwarding_mode`로 명시해야 합니다. 그러면 한 Gate에서 서버별로 legacy/none/BungeeGuard를 섞어 사용할 수 있습니다. 서버별 `velocity`는 Gate의 login-plugin 흐름 때문에 전역 모드도 `velocity`여야 합니다.
- `MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP`로 IP별 동시 로그인 준비 수를 제한할 수 있습니다(`0`은 비활성화). Redis를 사용하면 Gate replica 간 예약을 공유하고, Redis가 없으면 프로세스별 제한으로 동작합니다. TCP 수립 이후 애플리케이션 계층 방어이며 L4 방화벽/conntrack 제한을 대체하지 않습니다. 로그인 중 UUID가 제공되는 최신 클라이언트에 적용됩니다.
- 신뢰할 수 있는 L4 로드밸런서가 HAProxy PROXY protocol을 보내는 경우에만 `MCPROXY_GATE_PROXY_PROTOCOL=true`와 `MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES`에 LB의 IP/CIDR을 지정하십시오. 그래야 Gate의 per-IP quota가 실제 접속자 IP를 기준으로 동작하며, 신뢰 목록 없이 활성화하면 클라이언트가 IP를 위조할 수 있습니다. 백엔드가 PROXY protocol을 요구할 때만 `MCPROXY_GATE_PROXY_PROTOCOL_BACKEND=true`를 사용하십시오.
- Paper/Spigot에서 Legacy 전달을 사용할 경우 백엔드는 `online-mode=false`, `settings.bungeecord=true`로 설정해야 합니다. 백엔드 포트는 mcproxy만 접근할 수 있도록 방화벽으로 제한하십시오.
- Velocity 전달은 `MCPROXY_GATE_FORWARDING_MODE=velocity`와 `MCPROXY_GATE_VELOCITY_SECRET` 설정 후 백엔드에도 같은 secret을 지정해야 합니다.
- 서버별 BungeeGuard 모드에서는 mcproxy가 해당 백엔드의 `bungeeguard_allowed_token`을 handshake에 넣습니다. 공식 BungeeGuard 플러그인은 `/api/v1/servers/` 응답의 정확한 토큰을 `plugins/BungeeGuard/config.yml`의 `allowed-tokens:` 목록 항목으로 추가하고, placeholder/이전 토큰을 제거한 뒤 `bungeeguard reload`를 실행하세요. `allowed-token`(단수)은 공식 키가 아닙니다. 관리패널의 `Regenerate token` 또는 `POST /api/v1/servers/{id}/bungeeguard-token`으로 32자 토큰을 회전할 수 있으며, 회전 후에는 백엔드 설정을 갱신하고 reload해야 합니다. 전역 `MCPROXY_GATE_BUNGEGUARD_SECRET`는 서버별 토큰 원칙에 어긋나므로 배포 preflight가 거부합니다. BungeeGuard는 반드시 서버별 토큰을 사용합니다.
- 민감한 설정은 Docker/Kubernetes Secret 파일 마운트용 표준 `*_FILE` 형식도 지원합니다(예: `MCPROXY_ADMIN_TOKEN_FILE=/run/secrets/admin-token`). 같은 설정의 환경변수가 함께 있으면 환경변수가 우선하며, 관리자 토큰·선택적 패널 actor 서명 secret·PostgreSQL DSN·Redis 비밀번호·전역 Velocity forwarding secret에 적용됩니다. BungeeGuard는 전역 secret을 사용하지 않고 백엔드별로 생성·선택되는 토큰을 사용합니다.
- 서버별 `ping_motd`를 서버 API 또는 관리패널에서 설정할 수 있습니다. hostname으로 라우팅된 상태 ping에는 해당 MOTD가 표시되고, 매칭되지 않은 hostname에는 fallback MOTD가 표시됩니다.
- 같은 `public_host` 또는 `public_addr`를 가진 활성 백엔드는 하나의 분배 그룹으로 묶이며, 클라이언트 IP 해시를 기준으로 대략 균등하게 선택됩니다. 매칭되는 공개 라우트가 없을 때는 공개 라우트가 지정된 서버를 포함한 모든 활성 백엔드가 기본 분배 풀로 동작하고, 접속자의 ephemeral TCP 포트는 분배 키에서 제외되어 재접속 시 선택이 안정적입니다.

### Minecraft 26.3 접속 오류

`Failed to decode packet 'clientbound/minecraft:hello'`가 발생하면 26.3(protocol 777)을 지원하는 최신 Gate 버전으로 반드시 다시 빌드/배포해야 합니다. 이 프로젝트는 현재 Gate `v0.74.41`을 사용하며 26.3 지원이 포함되어 있습니다. 기존 바이너리나 Docker 이미지가 남아 있으면 `docker compose build --pull mcproxy && docker compose up -d mcproxy`로 교체하세요.

그 다음 `Unable to authenticate`가 계속되면 백엔드 종류에 맞는 전달 모드를 확인하세요. Paper 1.13.2 이상은 `velocity` 모드와 동일 secret을 사용하고, 구형 Paper/Spigot은 `legacy` 또는 BungeeGuard를 사용해야 합니다. 프록시와 백엔드의 온라인 모드/forwarding secret이 서로 다르면 인증이 실패합니다.
- Docker 안에서 실행할 때 `localhost`는 mcproxy 컨테이너 자신을 가리킵니다. 다른 컨테이너는 Compose 서비스 이름을, 호스트에서 실행 중인 백엔드는 컨테이너에서 도달 가능한 호스트 주소를 upstream으로 등록하십시오.
- 연결 실패 시 `gate_backend_rejected_player` 로그의 `reason` 및 `upstream` 필드에서 백엔드 거부 사유를 확인할 수 있습니다.

HTTP API 설계(요약)

기본 원칙

- 베이스 경로 예시: /api/v1
- 인증: 고정 토큰 기반 헤더 또는 프록시 앞단의 인증 게이트웨이 사용(권장)
- 응답 포맷: JSON

헬스체크 및 상태

- GET /health: 프로세스 헬스
- GET /ready: DB·필수 Redis coordination·Gate 데이터 플레인이 모두 트래픽을 받을 수 있는지 확인하며, 준비되지 않으면 503을 반환합니다.
- GET /metrics: Prometheus 메트릭입니다. 공개 인터넷에 노출하지 말고 Prometheus 네트워크에서만 접근하십시오.
- GET /api/v1/stats: 전역 통계(요청 수, 차단 수, 활성 카운터 등)

서버 리소스(백엔드 Minecraft 서버 단위)

- POST /api/v1/servers: 서버 등록
  - 주요 필드: 이름, 업스트림 주소, `forwarding_mode`(`none`/`legacy`/`velocity`/`bungeeguard`), 기본 임계값, Geo 정책 기본값
- GET /api/v1/servers 및 GET /api/v1/servers/{id}: 서버 목록/상세 조회
- PUT /api/v1/servers/{id}: 서버 속성 갱신(정책은 아래 정책 endpoint에서 별도 갱신)
- POST /api/v1/servers/{id}/bungeeguard-token: 해당 백엔드의 32자 BungeeGuard 토큰 생성·즉시 동기화
- DELETE /api/v1/servers/{id}: 서버 삭제(참조 무결성 및 연쇄 정책 처리)

정책 및 임계값(Threshold)

- 전역 정책: GET/PUT /api/v1/policy/global
  - 필드 예시: IP 기준 최대 연결 시도 수(윈도 10초, 60초), 닉네임 기준 최대 연결 시도 수(윈도 10초, 60초), 초과 시 조치(즉시 차단, 소프트 스로틀), 킥 메시지, 평가 순서 옵션
- 서버별 정책: GET/PUT /api/v1/policy/servers/{serverId}
  - 전역 정책을 상속하되 서버별 오버라이드 허용

룰 엔드포인트(Allow/Deny)

- 목록 조회: `GET /api/v1/rules` 또는 `GET /api/v1/rules?server_id={id}`
- 룰 생성: `POST /api/v1/rules`
  - `kind`: `ip-allow`, `ip-deny`, `name-allow`, `name-deny`, `asn-allow`, `asn-deny`
  - 항목 필드: `target`, `is_cidr`(IP 룰), `expires_at`(옵션), `reason`, `enabled`, `server_id`(옵션)
- 룰 삭제: `DELETE /api/v1/rules/{ruleId}`
- ASN 룰은 위 일반 룰 생성 endpoint에 `kind: asn-allow` 또는 `kind: asn-deny`를 지정합니다. MaxMind GeoLite2-ASN DB를 설정하면 `AS12345` 또는 `12345` 형식의 ASN을 평가합니다.
- Geo 정책
  - GET/PUT/DELETE `/api/v1/policy/geo`
  - 서버별 정책은 `?server_id={id}`를 붙이고, 생략하면 전역 정책을 대상으로 합니다.
  - 모드: 비활성, Allowlist, Denylist
  - 목록: ISO 국가 코드 배열
  - 범위: 전역 또는 서버별 오버라이드

접속 평가

- `POST /api/v1/evaluate`: `ip`, `nickname`, 선택적 `server_id`·`country`·`asn`을 평가하고 `allowed`, `reason`, `matched`, `counts`를 반환합니다.
- `record`를 생략하거나 `true`로 설정하면 평가 감사/카운터 기록 경로를 사용합니다.

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
- 통계 및 메트릭: Prometheus 호환 `/metrics`와 차단 사유별 지표를 제공합니다.
- 구조화 요청 로그 및 관리자 인증 실패 로그를 기록합니다: [internal/observability/logging.go](internal/observability/logging.go)

데이터 모델(개념)

- Servers: 이름, 업스트림, 기본 정책, 상태
- Policies: 전역 및 서버별 임계값, 평가 순서, 차단 메시지
- Rules: IP/닉네임/ASN Allow, IP/닉네임/ASN Deny, Geo 정책 항목
- Counters: IP 및 닉네임별 윈도 카운터(10초, 60초 등) — 전역/서버별 스코프 분리
- Audits: 정책 변경 이력과 차단 이벤트

GeoLite2 사용 안내

- MaxMind EULA를 준수해야 하며, 파일 배포 요건을 확인하십시오.
- DB 파일 경로는 실행 시 설정으로 지정합니다. 최신 상태 유지를 위해 주기적 갱신을 권장합니다.
- ASN 차단은 `MCPROXY_GEOIP_ASN_PATH`에 MaxMind GeoLite2-ASN DB를 지정한 경우에만 활성화됩니다.

보안 고려 사항

- HTTP API는 신뢰할 수 있는 네트워크 내에서만 노출하십시오.
- API 앞단에 리버스 프록시를 두고 인증/인가, 속도 제한, 감사 로깅을 적용하십시오.
- 관리자 토큰은 주기적으로 교체하고, 최소 권한 원칙을 준수하십시오.
- 별도 SvelteKit 3+·TailwindCSS 패널을 브라우저에서 직접 연결할 경우 `MCPROXY_ADMIN_ALLOWED_ORIGINS`에 정확한 HTTPS Origin만 등록하십시오. 권장 방식은 패널 서버 라우트가 토큰을 보관하고 mcproxy API를 프록시하는 것입니다.
- `MCPROXY_TRUST_PROXY_HEADERS=true`를 사용할 때는 반드시 `MCPROXY_TRUSTED_PROXY_CIDRS`에 실제 L7 프록시의 네트워크를 지정하십시오. 목록에 없는 peer의 `X-Forwarded-For`/`X-Real-IP`는 무시되며, CIDR 없이 활성화해도 헤더를 신뢰하지 않습니다.
- 외부 PostgreSQL 운영에서는 배포 preflight에 `MCPROXY_REQUIRE_POSTGRES_TLS=true`를 지정해 DSN의 `sslmode=require`, `verify-ca`, `verify-full`을 강제하십시오.
- 외부 Redis 운영에서도 `MCPROXY_REQUIRE_REDIS_TLS=true`를 지정해 `MCPROXY_REDIS_TLS=true`를 강제하십시오.
- `/health`는 liveness, `/ready`는 DB와 Gate 데이터 플레인 readiness를 확인하며, `MCPROXY_REDIS_FAIL_CLOSED=true`인 경우 Redis를 최대 2초 bounded `PING`으로 능동 확인해 coordination 장애를 readiness 실패로 보고합니다. `/metrics`는 Prometheus 호환 기본 지표를 제공합니다.
- `/metrics`에는 런타임 차단 사유별 `mcproxy_blocks_by_reason_total` 지표도 포함됩니다.
- `/metrics`의 `mcproxy_build_info`에서 실행 중인 release version, commit, build time, Go runtime 버전을 확인할 수 있습니다. 릴리스 빌드는 Make의 `VERSION`·`COMMIT`·`BUILD_TIME` 또는 동일한 Docker build arg를 주입하십시오.
- 메인 프로세스에서 Gate를 실행할 때 `/metrics`의 `mcproxy_gate_ready`로 데이터 플레인 상태를 확인할 수 있습니다.
- `/metrics`에는 현재 Gate 접속자 수(`mcproxy_gate_players`), 등록 backend 수(`mcproxy_gate_backends`), 유효 접속 시도·허용 시도·프로세스 수명 기준 정책 차단 counter도 포함됩니다.
- Redis 분산 카운터/캐시 무효화 오류는 `mcproxy_distributed_errors_total`, 로컬 fallback 횟수는 `mcproxy_distributed_fallbacks_total`, 무효화 publish 오류는 `mcproxy_distributed_publish_errors_total`로 노출되며, Redis 장애 시 다중 인스턴스 보호가 약화될 수 있어 경보를 확인해야 합니다.
- 감사 이벤트 저장 실패는 `mcproxy_audit_write_errors_total`로 노출됩니다. 값이 증가하면 관리자·보안 이벤트가 누락될 수 있으므로 데이터베이스, pool, 저장공간을 즉시 확인해야 합니다.
- 현재 Redis coordination 상태는 `mcproxy_distributed_healthy`로 확인할 수 있습니다. 오류가 누적되어도 연결이 복구되면 이 gauge는 다시 1이 됩니다.
- Compose 운영 스택은 보안 경계를 위해 `MCPROXY_REDIS_FAIL_CLOSED=true`가 기본값이며 Redis 카운터 장애 시 새 접속 평가를 거부합니다. Redis 장애 중 가용성을 우선할 때만 `false`로 명시하고 local fallback 지표를 감시하십시오.
- 관리패널 대시보드는 위 Gate 지표와 snapshot cache age를 서버 측에서 읽어 데이터 플레인 상태 카드로 표시합니다.
- SIGTERM 수신 시 HTTP 서버와 Gate 런타임을 함께 종료하고 제한 시간 동안 drain을 기다립니다.
- 활성 백엔드가 0개여도 Gate 리스너는 유지되며, 관리패널에서 서버를 다시 활성화하면 재시작 없이 동기화됩니다.
- distroless 컨테이너는 셸 없이 `/app/mcproxy healthcheck` 명령으로 `/ready`를 검사합니다.
- root `.dockerignore`는 자격증명, 로컬 DB, Git 메타데이터, 관리패널 의존성/빌드 산출물을 mcproxy 이미지 컨텍스트에서 제외합니다.
- `/api/v1/audits`는 관리패널용 최근 감사 이벤트 목록을 제공합니다.
- 패널 회원 이름을 감사 actor로 신뢰하려면 mcproxy와 별도 패널에 동일한 임의의 `MCPROXY_ADMIN_ACTOR_SECRET`(또는 `*_FILE`)를 설정하십시오. 패널이 actor 헤더를 HMAC 서명하며, 서명되지 않았거나 잘못된 헤더는 `admin-api`로 기록됩니다.
- 기본 관리패널은 [admin-panel](admin-panel)에서 별도 서버로 실행하며, 통계·백엔드 라우팅 조회와 서버 등록/활성화/비활성화/삭제를 제공합니다. 토큰은 패널 서버 환경변수에만 둡니다.
- 패널은 `ADMIN_PANEL_PASSWORD`와 32자 이상 `ADMIN_PANEL_SESSION_SECRET`으로 자체 로그인 세션을 사용합니다. 운영에서는 반드시 TLS와 reverse proxy/VPN을 함께 사용하십시오.
- 관리패널은 한·영·일 locale 전환을 지원합니다. 회원제 도입 범위와 권장 RBAC 모델은 [docs/admin-panel-membership.md](docs/admin-panel-membership.md)를 참고하십시오.
- 배포 전 mcproxy 설정은 `./deploy/preflight.sh`, 별도 관리패널 설정은 `./deploy/panel-preflight.sh`로 점검하십시오. 패널은 `PANEL_PREFLIGHT_REQUIRE_HTTPS=true`를 사용해 외부 `ORIGIN`의 HTTPS를 강제하고, Secret 파일 주입은 `*_FILE` 형식을 사용합니다.

성능 및 운영 팁

- 카운터 저장은 메모리 우선이며, 1초 슬롯 기반 슬라이딩 윈도우를 사용합니다. Redis 사용 시에도 sorted-set 기반 원자 연산으로 최근 N초 기준을 유지합니다: [internal/store/counter_engine.go](internal/store/counter_engine.go), [internal/store/distributed.go](internal/store/distributed.go)
- 정책/룰 평가는 snapshot 캐시를 우선 사용합니다: [internal/store/cache.go](internal/store/cache.go)
- 다중 인스턴스 환경에서는 Redis를 통한 분산 카운터/캐시 무효화를 사용할 수 있습니다: [internal/store/distributed.go](internal/store/distributed.go)
- 애그레시브한 임계값은 정상 사용자에게 영향이 갈 수 있으므로 점진적으로 조정하십시오.
- 백엔드 서버별로 트래픽 특성이 다를 수 있으니 서버별 임계값 튜닝을 활용하십시오.

컨테이너 실행

- 이미지 빌드: `docker build -t mcproxy .`
- 런타임 이미지는 `gcr.io/distroless/base-debian13` 기반이며 이미지 자체가 `USER nonroot:nonroot`(UID/GID 65532)로 실행됩니다. init wrapper는 명시적으로 root로 덮어쓰는 배포의 호환 경로도 유지합니다.
- 이미지 기본값은 HTTP `0.0.0.0:8080`, SQLite `/data/mcproxy.db`이며 필요하면 환경변수로 덮어쓸 수 있습니다.
- `make gate-e2e`는 이미지를 기동한 뒤 실제 Minecraft 26.3 status smoke test를 실행합니다. 로그인 패킷 전달은 백엔드 fixture를 연결하는 Gate 통합 테스트에서 검증하며, 백엔드가 활성화된 Gate에는 `mcproxy-statuscheck -login`을 별도로 사용할 수 있습니다.
- `make compose-smoke`는 PostgreSQL/Redis Compose 스택을 기동하고 mcproxy readiness healthcheck를 검사합니다.
- `make image-hardening-smoke`는 이미지 배포 전 관리패널 non-root 실행, mcproxy 권한 drop init, 양쪽 healthcheck가 유지되는지 검사합니다.
- `make release-check`는 Go 테스트·race·vet, 관리패널 빌드, 보안 audit, 공급망 pin, 환경설정 문서, 배포 매니페스트, mcproxy/패널 preflight를 한 번에 검사합니다. Go 테스트와 race는 기본 5분 후 실패하도록 제한되어(`GO_TEST_TIMEOUT`으로 변경 가능) Gate fixture 교착이 CI를 무기한 점유하지 않게 합니다. 보안 audit만 다시 실행하려면 `make security-audit`를, 공급망 pin만 확인하려면 `make supply-chain-check`를 사용하십시오. `make config-doc-check`는 한·영 README와 `inc.env`의 환경변수 목록 동기화를 확인하며, `make config-doc-check-smoke`는 누락 항목 거부도 검증합니다. 컨테이너 E2E와 백업 복구는 `make gate-e2e`, `make compose-smoke`, `make postgres-restore-smoke`, `make panel-backup-smoke`를 별도로 실행하십시오. 배포 직전 전체 로컬 운영 rehearsal은 `make ops-rehearsal`로 release gate, Gate protocol 777, PostgreSQL/Redis fail-closed, PostgreSQL 복구, 관리패널 rehearsal을 한 번에 실행할 수 있습니다.
- 로컬 Make 이미지 빌드는 가능하면 Git tag/description과 commit에서 mcproxy 버전을 자동으로 만들며, 재현 가능한 릴리스 빌드는 `VERSION`·`COMMIT`·`BUILD_TIME`으로 명시적으로 덮어쓸 수 있습니다.
- `make panel-smoke`는 별도 관리패널 이미지를 기동하고 로그인 endpoint와 보안 헤더를 검사합니다.
- `make migration-smoke`는 distroless 배포 이미지 안에서 DB 마이그레이션 명령을 검사합니다.

마이그레이션 및 스키마(개요)

- entgo 스키마는 애플리케이션 시작 시 자동으로 생성/보정됩니다. 기본 동작은 기존 데이터를 삭제하지 않는 additive migration입니다.
- 운영 배포에서는 먼저 백업한 뒤 `mcproxy migrate`를 실행할 수 있습니다. 로컬에서는 `make migrate`, Compose에서는 `docker compose -f deploy/docker-compose.yml run --rm mcproxy migrate`를 사용합니다.
- 이번 서버별 BungeeGuard 토큰과 `ping_motd` 컬럼도 기존 SQLite/PostgreSQL에 같은 경로로 추가되며, 빈 기존 행은 서버 목록 조회 시 32자 토큰으로 보완됩니다.
- 운영 순서: 백업 → 마이그레이션 → 애플리케이션 재기동 → `/ready` 확인. 마이그레이션 실패 시 애플리케이션을 계속 배포하지 마십시오.

로드맵

- 초기 MVP: 전역/서버별 임계값, IP/닉네임 Allow/Deny, Geo 정책, 기본 감사 로그, SQLite 지원
- 현재 반영: Gate 런타임 초안, PostgreSQL/Redis 운영 예시, 비동기 감사 파이프라인, 메모리 캐시/카운터
- 운영 환경별 후속 작업: provider별 SecretStore와 egress CIDR을 실제 클러스터 값으로 치환하고, Prometheus 경보 threshold를 정상 peak에 맞춰 튜닝합니다. Store 책임 분리와 관리패널 1차 UI/UX·다국어·고대비 기능은 반영되어 있습니다.
- Kubernetes 적용 순서는 `deploy/kubernetes/README.md`의 Secret 생성 → preflight → server-side dry-run → apply → rollout 확인 절차를 따르십시오. provider별 SecretStore와 egress CIDR은 클러스터 값 없이 안전하게 자동 결정할 수 없으므로 예시 값을 그대로 운영에 사용하지 마십시오.
- Prometheus 적용 전에는 [deploy/observability/README.md](deploy/observability/README.md)의 `promtool check rules/config`와 target `UP` 확인 절차를 따르십시오. 저장소의 YAML 검증만으로 PromQL 의미 검증을 대신할 수 없습니다.

라이선스

- AGPL-3.0. 자세한 내용은 [LICENSE.md](LICENSE.md) 참고.

관련 문서

- 영어 요약: [README.md](README.md)
- HTTP API 상세 레퍼런스(영문): [docs/api-reference.md](docs/api-reference.md)
- 진행 현황: [progress.md](progress.md)
- 현재 구현과 이후 목표: [docs/roadmap.md](docs/roadmap.md)
- Minekube Gate: https://gate.minekube.com/
